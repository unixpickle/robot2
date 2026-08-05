package motors

import (
	"math"

	"github.com/unixpickle/model3d/model3d"
)

// EndCoords represents the location of both fingers of the robot in
// 3D space. The z-axis points up, the y-axis faces in front of the robot,
// and the x-axis faces to the left of the robot when you are looking at
// the robot head-on from the front. This is the right hand rule.
type EndCoords struct {
	MovingFinger model3d.Coord3D
	LockedFinger model3d.Coord3D
}

// MotorAngles stores the angle of each motor in radians, assuming that
// the 0 angle is determined by the calibration script, i.e. where the
// arm is held straight upward.
type MotorAngles struct {
	ShoulderPan  float64
	ShoulderLift float64
	ElbowFlex    float64
	WristFlex    float64
	WristRoll    float64
	Gripper      float64
}

// MotorAnglesFromStatuses turns a motor state response into angles.
func MotorAnglesFromStatuses(state map[string]*AnnotatedStatus) *MotorAngles {
	transform := func(key string) float64 {
		return ((float64(state[key].Position) - 2048) / 4096) * math.Pi * 2
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

func (m *MotorAngles) Add(m1 *MotorAngles) {
	m.ShoulderPan += m1.ShoulderPan
	m.ShoulderLift += m1.ShoulderLift
	m.ElbowFlex += m1.ElbowFlex
	m.WristFlex += m1.WristFlex
	m.WristRoll += m1.WristRoll
	m.Gripper += m1.Gripper
}

// AnglesToCoords computes coordinates given the motor positions.
func AnglesToCoords(angles *MotorAngles) *EndCoords {
	// 9mm vertically between wrist roll and finger bases
	// Fingers are 82mm long
	c := EndCoords{
		LockedFinger: model3d.XYZ(0, 9, 82),
	}
	rotateMotor := func(angle float64, axis model3d.Coord3D) {
		xf := model3d.Rotation(axis, angle)
		c.MovingFinger = xf.Apply(c.MovingFinger)
		c.LockedFinger = xf.Apply(c.LockedFinger)
	}
	translate := func(offset model3d.Coord3D) {
		c.MovingFinger = c.MovingFinger.Add(offset)
		c.LockedFinger = c.LockedFinger.Add(offset)
	}

	c.MovingFinger = model3d.Rotation(
		model3d.X(1),
		angles.Gripper,
	).Apply(model3d.XYZ(0, 0, 82)).Add(model3d.Y(-9))
	rotateMotor(angles.WristRoll, model3d.Z(-1))

	// 82mm from wrist flex motor to center of wrist
	translate(model3d.Z(82))
	rotateMotor(angles.WristFlex, model3d.X(-1))

	// 134mm from wrist flex to elbow flex, and a bit
	// backwards in y (about 6mm)
	translate(model3d.XYZ(0, -6, 134))
	rotateMotor(angles.ElbowFlex, model3d.X(-1))

	// 111.5mm vertically and 28mm horizontally from
	// elbow flex to shoulder lift.
	translate(model3d.XYZ(0, 28, 111.5))
	rotateMotor(angles.ShoulderLift, model3d.X(-1))

	// 31mm horizontally and 70mm vertically to shoulder pan.
	translate(model3d.XYZ(0, 31, 70))
	rotateMotor(angles.ShoulderPan, model3d.Z(-1))

	return &c
}
