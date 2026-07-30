package motors

import (
	"encoding/json"
	"net/http"
	"strconv"
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

type MotorController struct {
	conn *Connection
	mux  *http.ServeMux
}

func NewMotorController(conn *Connection) (*MotorController, error) {
	mux := http.NewServeMux()

	// Configure the standard overload setup.
	for _, id := range motorIDs {
		if err := conn.SetOverloadProtection(id, 80, time.Second*2, 20); err != nil {
			return nil, err
		}
	}

	w := &MotorController{
		conn: conn,
		mux:  mux,
	}

	mux.HandleFunc("/status", w.handleStatus)
	mux.HandleFunc("/limits", w.handleLimits)
	mux.HandleFunc("/setlimits", w.handleSetLimits)
	mux.HandleFunc("/torque", w.handleTorque)
	mux.HandleFunc("/move", w.handleMove)

	return w, nil
}

func (m *MotorController) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(wr, r)
}

func (m *MotorController) handleStatus(w http.ResponseWriter, r *http.Request) {
	statuses, err := m.conn.MotorStatuses(6)
	if err != nil {
		apiutil.ServeError(w, err)
	} else {
		statusMap := map[string]*MotorStatus{}
		for k, v := range motorIDs {
			statusMap[k] = statuses[v-1]
		}
		apiutil.ServeData(w, statusMap)
	}
}

func (m *MotorController) handleLimits(w http.ResponseWriter, r *http.Request) {
	type limit struct {
		Min uint16 `json:"min"`
		Max uint16 `json:"max"`
	}
	results := map[string]limit{}
	for name, id := range motorIDs {
		min, max, err := m.conn.PositionLimit(id)
		if err != nil {
			apiutil.ServeError(w, err)
			return
		}
		results[name] = limit{Min: min, Max: max}
	}
	apiutil.ServeData(w, results)
}

func (m *MotorController) handleSetLimits(w http.ResponseWriter, r *http.Request) {
	type limit struct {
		Min uint16 `json:"min"`
		Max uint16 `json:"max"`
	}
	var results map[string]limit
	if err := json.NewDecoder(r.Body).Decode(&results); err != nil {
		apiutil.ServeError(w, err)
		return
	}
	for name, limit := range results {
		if id, ok := motorIDs[name]; !ok {
			apiutil.ServeError(w, &apiutil.WebError{Message: "unknown motor ID", Code: http.StatusBadRequest})
			return
		} else if err := m.conn.SetPositionLimit(id, limit.Min, limit.Max); err != nil {
			apiutil.ServeError(w, err)
			return
		}
	}
	apiutil.ServeData(w, true)
}

func (m *MotorController) handleTorque(w http.ResponseWriter, r *http.Request) {
	var ids []uint8
	if r.FormValue("motor") == "" {
		for _, id := range motorIDs {
			ids = append(ids, id)
		}
	} else {
		motorID, err := m.motorIDFromRequest(r)
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
	motorID, err := m.motorIDFromRequest(r)
	if err != nil {
		apiutil.ServeError(w, err)
		return
	}
	rawPos := r.FormValue("pos")
	pos, err := strconv.Atoi(rawPos)
	if err != nil {
		apiutil.ServeError(w, &apiutil.WebError{Message: "invalid pos", Code: http.StatusBadRequest})
		return
	}
	m.conn.SetPosition(motorID, uint16(pos), 100, 10)
	apiutil.ServeData(w, true)
}

func (m *MotorController) motorIDFromRequest(r *http.Request) (uint8, error) {
	name := r.FormValue("motor")
	if name == "" {
		return 0, &apiutil.WebError{Message: "must specify a motor", Code: http.StatusBadRequest}
	}
	if id, ok := motorIDs[name]; ok {
		return id, nil
	}
	return 0, &apiutil.WebError{Message: "unknown motor ID was specified", Code: http.StatusBadRequest}
}
