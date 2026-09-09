package motors

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/robot2/apiutil"
)

// relaxTimeout is the amount of time of no movement after which
// we will reset positions to what they previously were.
const relaxTimeout = time.Second * 15

// DefaultMotorIDs is the ID mapping for the SO-101 arm.
var DefaultMotorIDs = map[string]uint8{
	"shoulder_pan":  1,
	"shoulder_lift": 2,
	"elbow_flex":    3,
	"wrist_flex":    4,
	"wrist_roll":    5,
	"gripper":       6,
}

var errUnknownMotor = &apiutil.WebError{
	Message: "unknown motor ID was specified",
	Code:    http.StatusBadRequest,
}

type statusListener struct {
	Ch chan map[string]*AnnotatedStatus
}

func (s *statusListener) Send(x map[string]*AnnotatedStatus) {
	select {
	case s.Ch <- x:
		return
	default:
		select {
		case <-s.Ch:
		default:
		}
		s.Ch <- x
	}
}

type MotorLimit struct {
	Min uint16 `json:"min"`
	Max uint16 `json:"max"`
}

type AnnotatedStatus struct {
	MotorStatus

	RelativePos       float64    `json:"relative_pos"`
	PositionLimit     MotorLimit `json:"position_limit"`
	TargetPos         uint16     `json:"target_pos"`
	TargetRelativePos float64    `json:"target_relative_pos"`
}

type MotorController struct {
	motorIDs map[string]uint8

	conn *Connection
	mux  *http.ServeMux

	changeLimitsLock sync.Mutex
	limits           atomic.Value // contains a map[string]MotorLimit

	changeTargetLock sync.Mutex
	targets          *sync.Map // maps uint8 to uint16

	changeTorqueLimitLock sync.Mutex
	torqueLimits          *sync.Map // maps uint8 to float64

	listenersLock sync.RWMutex
	listeners     map[*statusListener]struct{}

	relaxTicker  *time.Ticker
	relaxTimeout time.Duration
}

// NewMotorController creates a controller with the given motor ID mapping.
// If no mapping is provided, DefaultMotorIDs is used.
func NewMotorController(conn *Connection, ids map[string]uint8) (*MotorController, error) {
	if ids == nil {
		ids = DefaultMotorIDs
	}
	mux := http.NewServeMux()

	limits := map[string]MotorLimit{}
	targets := new(sync.Map)
	torqueLimits := new(sync.Map)
	for name, id := range ids {
		if err := conn.SetOverloadProtection(id, 80, time.Second*2, 20); err != nil {
			return nil, err
		}
		if min, max, err := conn.PositionLimit(id); err != nil {
			return nil, err
		} else {
			limits[name] = MotorLimit{Min: min, Max: max}
		}
		if target, err := conn.PositionTarget(id); err != nil {
			return nil, err
		} else {
			targets.Store(id, target)
		}
		if limit, err := conn.TorqueLimitFrac(id); err != nil {
			return nil, err
		} else {
			torqueLimits.Store(id, limit)
		}
	}

	w := &MotorController{
		motorIDs:     ids,
		conn:         conn,
		mux:          mux,
		targets:      targets,
		torqueLimits: torqueLimits,
		listeners:    map[*statusListener]struct{}{},
		relaxTicker:  time.NewTicker(relaxTimeout),
		relaxTimeout: relaxTimeout,
	}
	w.limits.Store(limits)

	mux.HandleFunc("/names", w.handleNames)
	mux.HandleFunc("/status", w.handleStatus)
	mux.HandleFunc("/limits", w.handleLimits)
	mux.HandleFunc("/setlimits", w.handleSetLimits)
	mux.HandleFunc("/torque", w.handleTorque)
	mux.HandleFunc("/settorque", w.handleSetTorque)
	mux.HandleFunc("/torquelimit", w.handleTorqueLimit)
	mux.HandleFunc("/settorquelimit", w.handleSetTorqueLimit)
	mux.HandleFunc("/move", w.handleMove)
	mux.HandleFunc("/waituntilstill", w.handleWaitUntilStill)
	mux.HandleFunc("/stream", w.handleStream)
	mux.HandleFunc("/calibratecenter", w.handleCalibrateCenter)
	mux.HandleFunc("/stoprelax", w.handleStopRelax)

	go w.stateLoop()
	go w.relaxLoop()

	return w, nil
}

func (m *MotorController) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(wr, r)
}

func (m *MotorController) handleNames(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeData(w, m.MotorNames())
}

// MotorNames gets the id-sorted list of motor names.
func (m *MotorController) MotorNames() []string {
	var names []string
	var ids []uint8

	for name, id := range m.motorIDs {
		names = append(names, name)
		ids = append(ids, id)
	}
	essentials.VoodooSort(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	}, names)

	return names
}

func (m *MotorController) handleStatus(w http.ResponseWriter, r *http.Request) {
	statuses, err := m.Status()
	if err != nil {
		apiutil.ServeError(w, err)
	} else {
		apiutil.ServeData(w, statuses)
	}
}

func (m *MotorController) Status() (map[string]*AnnotatedStatus, error) {
	statusMap := map[string]*AnnotatedStatus{}
	for name, id := range m.motorIDs {
		if status, err := m.conn.MotorStatus(uint8(id)); err != nil {
			return nil, fmt.Errorf("get status for motor %d failed: %w", id, err)
		} else {
			statusMap[name] = m.annotatedStatus(status)
		}
	}
	return statusMap, nil
}

func (m *MotorController) handleLimits(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeData(w, m.Limits())
}

func (m *MotorController) Limits() map[string]MotorLimit {
	return m.limits.Load().(map[string]MotorLimit)
}

func (m *MotorController) handleSetLimits(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeAPI(w, r, func(newLimits map[string]MotorLimit) (bool, error) {
		if err := m.SetLimits(newLimits); err != nil {
			return false, err
		} else {
			return true, nil
		}
	})
}

func (m *MotorController) SetLimits(limits map[string]MotorLimit) error {
	m.changeLimitsLock.Lock()
	defer m.changeLimitsLock.Unlock()
	m.delayRelax()
	newLimits := map[string]MotorLimit{}
	maps.Copy(newLimits, m.limits.Load().(map[string]MotorLimit))
	maps.Copy(newLimits, limits)
	for name, limit := range limits {
		if id, ok := m.motorIDs[name]; !ok {
			return errUnknownMotor
		} else if err := m.conn.SetPositionLimit(id, limit.Min, limit.Max); err != nil {
			return err
		}
		m.delayRelax()
	}
	m.limits.Store(newLimits)
	return nil
}

func (m *MotorController) handleTorque(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeAPI(w, r, func(body TorqueRequest) (bool, error) {
		motorID, ok := m.motorIDs[body.Motor]
		if !ok {
			return false, errUnknownMotor
		}
		return m.conn.TorqueEnabled(motorID)
	})
}

func (m *MotorController) handleSetTorque(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeAPI(w, r, func(body SetTorqueRequest) (bool, error) {
		if body.Motor != nil {
			motorID, ok := m.motorIDs[*body.Motor]
			if !ok {
				return false, errUnknownMotor
			}
			m.delayRelax()
			if err := m.conn.SetTorqueEnabled(motorID, body.Enabled); err != nil {
				return false, err
			}
			return true, nil
		} else {
			for _, id := range m.motorIDs {
				m.delayRelax()
				if err := m.conn.SetTorqueEnabled(id, body.Enabled); err != nil {
					return false, err
				}
			}
			return true, nil
		}
	})
}

func (m *MotorController) handleTorqueLimit(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeAPI(w, r, func(body TorqueLimitRequest) (float64, error) {
		motorID, ok := m.motorIDs[body.Motor]
		if !ok {
			return 0, errUnknownMotor
		}
		value, _ := m.torqueLimits.Load(motorID)
		return value.(float64), nil
	})
}

func (m *MotorController) handleSetTorqueLimit(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeAPI(w, r, func(body SetTorqueLimitRequest) (bool, error) {
		motorID, ok := m.motorIDs[body.Motor]
		if !ok {
			return false, errUnknownMotor
		}
		if body.Limit < 0 || body.Limit > 1 {
			return false, &apiutil.WebError{Code: http.StatusBadRequest, Message: "invalid limit argument"}
		}
		m.changeLimitsLock.Lock()
		err := m.conn.SetTorqueLimitFrac(motorID, body.Limit)
		if err == nil {
			m.torqueLimits.Store(motorID, body.Limit)
		}
		m.changeLimitsLock.Unlock()
		return true, err
	})
}

func (m *MotorController) handleMove(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeAPI(w, r, func(body MoveRequest) (bool, error) {
		if err := m.Move(body.Motor, body.Pos); err != nil {
			return false, err
		} else {
			return true, nil
		}
	})
}

func (m *MotorController) Move(motor string, target uint16) error {
	motorID, ok := m.motorIDs[motor]
	if !ok {
		return errUnknownMotor
	}

	m.changeTargetLock.Lock()
	defer m.changeTargetLock.Unlock()
	m.delayRelax()
	if err := m.conn.SetPosition(motorID, target, 300, 10); err != nil {
		return err
	}
	m.targets.Store(motorID, target)
	m.delayRelax()
	return nil
}

func (m *MotorController) handleWaitUntilStill(w http.ResponseWriter, r *http.Request) {
	if err := m.WaitUntilStill(r.Context()); err != nil {
		apiutil.ServeError(w, err)
	} else {
		apiutil.ServeData(w, true)
	}
}

func (m *MotorController) WaitUntilStill(ctx context.Context) error {
	// Make sure any enqueued movements are started in the first place.
	time.Sleep(time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		statuses, err := m.Status()
		if err != nil {
			return err
		}
		allDone := true
		for _, s := range statuses {
			if s.IsMoving() {
				allDone = false
			}
		}
		if allDone {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (m *MotorController) handleStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	rc := http.NewResponseController(w)
	for event := range m.listenToStatuses(r.Context()) {
		data, err := json.Marshal(event)
		if err != nil {
			panic(err)
		}
		data = append(append([]byte("data: "), data...), []byte("\n\n")...)
		if _, err := w.Write(data); err != nil {
			return
		}
		if rc.Flush() != nil {
			return
		}
	}
}

func (m *MotorController) handleCalibrateCenter(w http.ResponseWriter, r *http.Request) {
	m.delayRelax()
	for _, id := range m.motorIDs {
		if err := m.conn.CenterPosition(id); err != nil {
			apiutil.ServeError(w, err)
			return
		}
	}
	apiutil.ServeData(w, true)
}

func (m *MotorController) handleStopRelax(w http.ResponseWriter, r *http.Request) {
	for {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		m.delayRelax()
		time.Sleep(m.relaxTimeout / 2)
	}
}

func (m *MotorController) annotatedStatus(status *MotorStatus) *AnnotatedStatus {
	limits := m.limits.Load().(map[string]MotorLimit)
	var limit MotorLimit
	for name, id := range m.motorIDs {
		if id == status.ID {
			limit = limits[name]
		}
	}
	targetRaw, _ := m.targets.Load(status.ID)
	target := targetRaw.(uint16)
	normedPos := (float64(status.Position) - float64(limit.Min)) / float64(limit.Max-limit.Min)
	normedTarget := (float64(target) - float64(limit.Min)) / float64(limit.Max-limit.Min)
	return &AnnotatedStatus{
		MotorStatus:       *status,
		RelativePos:       normedPos,
		PositionLimit:     limit,
		TargetPos:         target,
		TargetRelativePos: normedTarget,
	}
}

func (m *MotorController) listenToStatuses(ctx context.Context) <-chan map[string]*AnnotatedStatus {
	listener := &statusListener{Ch: make(chan map[string]*AnnotatedStatus, 1)}
	m.listenersLock.Lock()
	m.listeners[listener] = struct{}{}
	m.listenersLock.Unlock()

	go func() {
		<-ctx.Done()
		m.listenersLock.Lock()
		delete(m.listeners, listener)
		m.listenersLock.Unlock()
		close(listener.Ch)
	}()

	return listener.Ch
}

func (m *MotorController) stateLoop() {
	for {
		statuses, err := m.Status()
		if err != nil {
			log.Println("error in motor status loop:", err)
			time.Sleep(time.Second * 30)
			continue
		}
		m.listenersLock.RLock()
		for listener := range m.listeners {
			listener.Send(statuses)
		}
		m.listenersLock.RUnlock()
		time.Sleep(time.Second)
	}
}

func (m *MotorController) delayRelax() {
	m.relaxTicker.Reset(relaxTimeout)
}

func (m *MotorController) relaxLoop() {
	for range m.relaxTicker.C {
		if err := m.relax(); err != nil {
			log.Printf("error relaxing motors: %s", err)
		}
	}
}

func (m *MotorController) relax() error {
	m.changeTargetLock.Lock()
	defer m.changeTargetLock.Unlock()
	for _, id := range m.motorIDs {
		torque, err := m.conn.TorqueEnabled(id)
		if err != nil {
			return err
		}
		if !torque {
			// We might be mid calibration, but generally there's
			// no reason to relax a torqueless motor.
			continue
		}

		status, err := m.conn.MotorStatus(id)
		if err != nil {
			return err
		}

		target, err := m.conn.PositionTarget(id)
		if err != nil {
			return err
		}
		if status.Position != int16(target) {
			newTarget := uint16(status.Position)
			if err := m.conn.SetPosition(id, newTarget, 300, 10); err != nil {
				return err
			}
			m.targets.Store(id, newTarget)
		}
	}
	return nil
}
