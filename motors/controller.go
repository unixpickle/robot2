package motors

import (
	"context"
	"encoding/json"
	"log"
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

	RelativePos       float64    `json:"relativePos"`
	PositionLimit     MotorLimit `json:"positionLimit"`
	TargetPos         uint16     `json:"targetPos"`
	TargetRelativePos float64    `json:"targetRelativePos"`
}

type MotorController struct {
	motorIDs map[string]uint8

	conn *Connection
	mux  *http.ServeMux

	changeLimitsLock sync.Mutex
	limits           atomic.Value // contains a map[string]MotorLimit

	changeTargetLock sync.Mutex
	targets          *sync.Map // maps uint8 to uint16

	listenersLock sync.RWMutex
	listeners     map[*statusListener]struct{}

	relaxTicker *time.Ticker
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
	}

	w := &MotorController{
		motorIDs:    ids,
		conn:        conn,
		mux:         mux,
		targets:     targets,
		listeners:   map[*statusListener]struct{}{},
		relaxTicker: time.NewTicker(relaxTimeout),
	}
	w.limits.Store(limits)

	mux.HandleFunc("/names", w.handleNames)
	mux.HandleFunc("/status", w.handleStatus)
	mux.HandleFunc("/limits", w.handleLimits)
	mux.HandleFunc("/setlimits", w.handleSetLimits)
	mux.HandleFunc("/torque", w.handleTorque)
	mux.HandleFunc("/settorque", w.handleSetTorque)
	mux.HandleFunc("/move", w.handleMove)
	mux.HandleFunc("/stream", w.handleStream)
	mux.HandleFunc("/calibratecenter", w.handleCalibrateCenter)

	go w.stateLoop()
	go w.relaxLoop()

	return w, nil
}

func (m *MotorController) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(wr, r)
}

func (m *MotorController) handleNames(w http.ResponseWriter, r *http.Request) {
	var names []string
	var ids []uint8

	for name, id := range m.motorIDs {
		names = append(names, name)
		ids = append(ids, id)
	}
	essentials.VoodooSort(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	}, names)

	apiutil.ServeData(w, names)
}

func (m *MotorController) handleStatus(w http.ResponseWriter, r *http.Request) {
	statuses, err := m.motorStatuses()
	if err != nil {
		apiutil.ServeError(w, err)
	} else {
		apiutil.ServeData(w, statuses)
	}
}

func (m *MotorController) handleLimits(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeData(w, m.limits.Load().(map[string]MotorLimit))
}

func (m *MotorController) handleSetLimits(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeAPI(w, r, func(newLimits map[string]MotorLimit) (bool, error) {
		if err := m.changeLimits(newLimits); err != nil {
			return false, err
		} else {
			m.delayRelax()
			return true, nil
		}
	})
}

func (m *MotorController) changeLimits(limits map[string]MotorLimit) error {
	m.changeLimitsLock.Lock()
	defer m.changeLimitsLock.Unlock()
	for name, limit := range limits {
		if id, ok := m.motorIDs[name]; !ok {
			return errUnknownMotor
		} else if err := m.conn.SetPositionLimit(id, limit.Min, limit.Max); err != nil {
			return err
		}
	}
	m.limits.Store(limits)
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

func (m *MotorController) handleMove(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeAPI(w, r, func(body MoveRequest) (bool, error) {
		motorID, ok := m.motorIDs[body.Motor]
		if !ok {
			return false, errUnknownMotor
		}
		m.delayRelax()
		if err := m.changeTarget(motorID, body.Pos); err != nil {
			return false, err
		} else {
			return true, nil
		}
	})
}

func (m *MotorController) changeTarget(motorID uint8, target uint16) error {
	m.changeTargetLock.Lock()
	defer m.changeTargetLock.Unlock()
	if err := m.conn.SetPosition(motorID, target, 300, 10); err != nil {
		return err
	}
	m.targets.Store(motorID, target)
	return nil
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
		statuses, err := m.motorStatuses()
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

func (m *MotorController) motorStatuses() (map[string]*AnnotatedStatus, error) {
	statuses, err := m.conn.MotorStatuses(len(m.motorIDs))
	if err != nil {
		return nil, err
	}
	statusMap := map[string]*AnnotatedStatus{}
	for k, v := range m.motorIDs {
		statusMap[k] = m.annotatedStatus(statuses[v-1])
	}
	return statusMap, nil
}
