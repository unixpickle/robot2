package motors

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/unixpickle/robot2/apiutil"
)

var motorIDs = map[string]uint8{
	"shoulder_pan":  1,
	"shoulder_lift": 2,
	"elbow_flex":    3,
	"wrist_flex":    4,
	"wrist_roll":    5,
	"gripper":       6,
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
	conn *Connection
	mux  *http.ServeMux

	changeLimitsLock sync.Mutex
	limits           atomic.Value // contains a map[string]MotorLimit

	changeTargetLock sync.Mutex
	targets          *sync.Map // maps uint8 to uint16

	listenersLock sync.RWMutex
	listeners     map[*statusListener]struct{}
}

func NewMotorController(conn *Connection) (*MotorController, error) {
	mux := http.NewServeMux()

	limits := map[string]MotorLimit{}
	targets := new(sync.Map)
	for name, id := range motorIDs {
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
		conn:      conn,
		mux:       mux,
		targets:   targets,
		listeners: map[*statusListener]struct{}{},
	}
	w.limits.Store(limits)

	mux.HandleFunc("/status", w.handleStatus)
	mux.HandleFunc("/limits", w.handleLimits)
	mux.HandleFunc("/setlimits", w.handleSetLimits)
	mux.HandleFunc("/torque", w.handleTorque)
	mux.HandleFunc("/move", w.handleMove)
	mux.HandleFunc("/moverel", w.handleMove)
	mux.HandleFunc("/stream", w.handleStream)
	mux.HandleFunc("/calibratecenter", w.handleCalibrateCenter)

	go w.stateLoop()

	return w, nil
}

func (m *MotorController) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(wr, r)
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
	var results map[string]MotorLimit
	if err := json.NewDecoder(r.Body).Decode(&results); err != nil {
		apiutil.ServeError(w, err)
		return
	}
	if err := m.changeLimits(results); err != nil {
		apiutil.ServeError(w, err)
	} else {
		apiutil.ServeData(w, true)
	}
}

func (m *MotorController) changeLimits(results map[string]MotorLimit) error {
	m.changeLimitsLock.Lock()
	defer m.changeLimitsLock.Unlock()
	for name, limit := range results {
		if id, ok := motorIDs[name]; !ok {
			return &apiutil.WebError{Message: "unknown motor ID", Code: http.StatusBadRequest}
		} else if err := m.conn.SetPositionLimit(id, limit.Min, limit.Max); err != nil {
			return err
		}
	}
	m.limits.Store(results)
	return nil
}

func (m *MotorController) handleTorque(w http.ResponseWriter, r *http.Request) {
	var ids []uint8
	if r.FormValue("motor") == "" {
		for _, id := range motorIDs {
			ids = append(ids, id)
		}
	} else {
		motorID, _, err := m.motorIDFromRequest(r)
		if err != nil {
			apiutil.ServeError(w, err)
			return
		}
		ids = []uint8{motorID}
	}
	enabled := r.FormValue("enabled")
	if enabled == "" {
		var results []bool
		for _, id := range ids {
			flag, err := m.conn.TorqueEnabled(id)
			if err != nil {
				apiutil.ServeError(w, err)
				return
			}
			results = append(results, flag)
		}
		apiutil.ServeData(w, results)
	} else {
		for _, id := range ids {
			if err := m.conn.SetTorqueEnabled(id, enabled == "1"); err != nil {
				apiutil.ServeError(w, err)
				return
			}
		}
		apiutil.ServeData(w, true)
	}
}

func (m *MotorController) handleMove(w http.ResponseWriter, r *http.Request) {
	motorID, motorName, err := m.motorIDFromRequest(r)
	if err != nil {
		apiutil.ServeError(w, err)
		return
	}

	// Allow both relative and absolute positioning for a motor, which currently
	// leads to some unpleasant looking code.
	rawPos := r.FormValue("pos")
	parsedPos, err := strconv.Atoi(rawPos)
	pos := uint16(parsedPos)
	if err != nil {
		relPos := r.FormValue("rel")
		relPosValue, err := strconv.ParseFloat(relPos, 64)
		if err != nil {
			apiutil.ServeError(
				w,
				&apiutil.WebError{Message: "invalid position", Code: http.StatusBadRequest},
			)
			return
		}
		limit := m.limits.Load().(map[string]MotorLimit)[motorName]
		pos = limit.Min + uint16(math.Round(relPosValue*float64(limit.Max-limit.Min)))
	}

	if err := m.changeTarget(motorID, pos); err != nil {
		apiutil.ServeError(w, err)
	} else {
		apiutil.ServeData(w, true)
	}
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
	for _, id := range motorIDs {
		if err := m.conn.CenterPosition(id); err != nil {
			apiutil.ServeError(w, err)
			return
		}
	}
	apiutil.ServeData(w, true)
}

func (m *MotorController) motorIDFromRequest(r *http.Request) (uint8, string, error) {
	name := r.FormValue("motor")
	if name == "" {
		return 0, name, &apiutil.WebError{Message: "must specify a motor", Code: http.StatusBadRequest}
	}
	if id, ok := motorIDs[name]; ok {
		return id, name, nil
	}
	return 0, name, &apiutil.WebError{Message: "unknown motor ID was specified", Code: http.StatusBadRequest}
}

func (m *MotorController) annotatedStatus(status *MotorStatus) *AnnotatedStatus {
	limits := m.limits.Load().(map[string]MotorLimit)
	var limit MotorLimit
	for name, id := range motorIDs {
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

func (m *MotorController) motorStatuses() (map[string]*AnnotatedStatus, error) {
	statuses, err := m.conn.MotorStatuses(len(motorIDs))
	if err != nil {
		return nil, err
	}
	statusMap := map[string]*AnnotatedStatus{}
	for k, v := range motorIDs {
		statusMap[k] = m.annotatedStatus(statuses[v-1])
	}
	return statusMap, nil
}
