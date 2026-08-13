package kinematics

import (
	"context"
	"net/http"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/robot2/apiutil"
	"github.com/unixpickle/robot2/motors"
)

type KinematicsController struct {
	mc  *motors.MotorController
	mux *http.ServeMux
}

// NewKinematicsController creates a controller that wraps a motor controller.
func NewKinematicsController(mc *motors.MotorController) *KinematicsController {
	mux := http.NewServeMux()

	k := &KinematicsController{
		mc:  mc,
		mux: mux,
	}

	mux.HandleFunc("/move", k.handleMove)
	mux.HandleFunc("/limitsangles", k.handleLimitsAngles)
	mux.HandleFunc("/currentangles", k.handleCurrentAngles)
	mux.HandleFunc("/safehome", k.handleSafeHome)

	return k
}

func (k *KinematicsController) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	k.mux.ServeHTTP(wr, r)
}

func (k *KinematicsController) handleMove(w http.ResponseWriter, r *http.Request) {
	apiutil.ServeAPI(w, r, func(angles *MotorAngles) (bool, error) {
		if err := k.MoveAngles(angles); err != nil {
			return false, err
		} else {
			return true, nil
		}
	})
}

func (k *KinematicsController) MoveAngles(angles *MotorAngles) error {
	names := k.mc.MotorNames()
	for i, name := range names {
		angle := angles.Vec()[i]
		pos := AngleToPosition(angle)
		if err := k.mc.Move(name, pos); err != nil {
			return err
		}
	}
	return nil
}

func (k *KinematicsController) handleLimitsAngles(w http.ResponseWriter, r *http.Request) {
	var resp struct {
		Min *MotorAngles `json:"min"`
		Max *MotorAngles `json:"max"`
	}
	resp.Min, resp.Max = k.LimitsAngles()
	apiutil.ServeData(w, resp)
}

func (k *KinematicsController) LimitsAngles() (min *MotorAngles, max *MotorAngles) {
	names := k.mc.MotorNames()
	rawLimits := k.mc.Limits()
	var minVec, maxVec [6]float64
	for i, name := range names {
		lim := rawLimits[name]
		minVec[i] = PositionToAngle(int16(lim.Min))
		maxVec[i] = PositionToAngle(int16(lim.Max))
	}
	min, max = NewMotorAngles(minVec), NewMotorAngles(maxVec)
	return min, max
}

func (k *KinematicsController) handleCurrentAngles(w http.ResponseWriter, r *http.Request) {
	if angles, err := k.CurrentAngles(); err != nil {
		apiutil.ServeError(w, err)
	} else {
		apiutil.ServeData(w, angles)
	}
}

func (k *KinematicsController) CurrentAngles() (*MotorAngles, error) {
	statuses, err := k.mc.Status()
	if err != nil {
		return nil, err
	}
	names := k.mc.MotorNames()
	var angles [6]float64
	for i, name := range names {
		angles[i] = PositionToAngle(statuses[name].Position)
	}
	return NewMotorAngles(angles), nil
}

func (k *KinematicsController) handleSafeHome(w http.ResponseWriter, r *http.Request) {
	if err := k.HomeSafely(r.Context()); err != nil {
		apiutil.ServeError(w, err)
	} else {
		apiutil.ServeData(w, true)
	}
}

func (k *KinematicsController) HomeSafely(ctx context.Context) (err error) {
	defer essentials.AddCtxTo("home safely", &err)
	min, max := k.LimitsAngles()
	startPos, err := k.CurrentAngles()
	if err != nil {
		return err
	}
	trajectory := PlanSafeHome(min, max, startPos)
	for _, angles := range trajectory {
		if err := k.MoveAngles(angles); err != nil {
			return err
		}
		if err := k.mc.WaitUntilStill(ctx); err != nil {
			return err
		}
	}
	return nil
}
