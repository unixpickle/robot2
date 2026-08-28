package kinematics

import (
	"math"

	"github.com/unixpickle/robot2/motors"
)

// PositionToAngle turns a motor position into an angle where
// 2048 is 0. The angle is in radians.
func PositionToAngle(pos int16) float64 {
	return math.Pi * (float64(pos) - 2048) / 2048
}

// AngleToPosition is the inverse of PositionToAngle, with extra
// coverage for angles outside of [-pi, pi].
func AngleToPosition(theta float64) uint16 {
	for theta < -math.Pi {
		theta += math.Pi * 2
	}
	for theta > math.Pi {
		theta -= math.Pi * 2
	}
	return uint16(math.Max(0, math.Min(4095, 2048+theta*2048/math.Pi)))
}

// MotorAngles stores the angle of each motor in radians, assuming that
// the 0 angle is determined by the calibration script, i.e. where the
// arm is held straight upward.
type MotorAngles struct {
	ShoulderPan  float64 `json:"shoulder_pan"`
	ShoulderLift float64 `json:"shoulder_lift"`
	ElbowFlex    float64 `json:"elbow_flex"`
	WristFlex    float64 `json:"wrist_flex"`
	WristRoll    float64 `json:"wrist_roll"`
	Gripper      float64 `json:"gripper"`
}

// MotorAnglesFromStatuses turns a motor state response into angles.
func MotorAnglesFromStatuses(state map[string]*motors.AnnotatedStatus) *MotorAngles {
	transform := func(key string) float64 {
		return PositionToAngle(state[key].Position)
	}
	return &MotorAngles{
		ShoulderPan:  transform("shoulder_pan"),
		ShoulderLift: transform("shoulder_lift"),
		ElbowFlex:    transform("elbow_flex"),
		WristFlex:    transform("wrist_flex"),
		WristRoll:    transform("wrist_roll"),
		Gripper:      transform("gripper"),
	}
}

// NewMotorAngles creates MotorAngles from a vector.
func NewMotorAngles(v [6]float64) *MotorAngles {
	res := MotorAngles{}
	res.SetVec(v)
	return &res
}

func (m *MotorAngles) Vec() [6]float64 {
	return [6]float64{
		m.ShoulderPan,
		m.ShoulderLift,
		m.ElbowFlex,
		m.WristFlex,
		m.WristRoll,
		m.Gripper,
	}
}

func (m *MotorAngles) SetVec(v [6]float64) {
	*m = MotorAngles{
		ShoulderPan:  v[0],
		ShoulderLift: v[1],
		ElbowFlex:    v[2],
		WristFlex:    v[3],
		WristRoll:    v[4],
		Gripper:      v[5],
	}
}

func (m *MotorAngles) Copy() *MotorAngles {
	res := *m
	return &res
}
