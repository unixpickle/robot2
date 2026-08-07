package motors

import (
	"math"

	"github.com/unixpickle/model3d/model3d"
)

func PositionToAngle(pos int16) float64 {
	return math.Pi * (float64(pos) - 2048) / 2048
}

func AngleToPosition(theta float64) int16 {
	for theta < -math.Pi {
		theta += math.Pi * 2
	}
	for theta > math.Pi {
		theta -= math.Pi * 2
	}
	return int16(math.Max(0, math.Min(4095, 2048+theta*2048/math.Pi)))
}

// EndCoords represents the location of both fingers of the robot in
// 3D space. The z-axis points up, the y-axis faces in front of the robot,
// and the x-axis faces to the left of the robot when you are looking at
// the robot head-on from the front. This is the right hand rule.
type EndCoords struct {
	MovingFinger model3d.Coord3D
	LockedFinger model3d.Coord3D
}

func (e *EndCoords) Mid() model3d.Coord3D {
	return e.MovingFinger.Mid(e.LockedFinger)
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

func (m *MotorAngles) Add(m1 *MotorAngles) {
	v := m.Vec()
	for i, x := range m1.Vec() {
		v[i] += x
	}
	m.SetVec(v)
}

func (m *MotorAngles) Sub(m1 *MotorAngles) {
	v := m.Vec()
	for i, x := range m1.Vec() {
		v[i] -= x
	}
	m.SetVec(v)
}

func (m *MotorAngles) Scale(s float64) {
	v := m.Vec()
	for i, x := range m.Vec() {
		v[i] *= x * s
	}
	m.SetVec(v)
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
	rotateMotor(angles.ElbowFlex+math.Pi/2, model3d.X(-1))

	// 111.5mm vertically and 28mm horizontally from
	// elbow flex to shoulder lift.
	translate(model3d.XYZ(0, 28, 111.5))
	rotateMotor(angles.ShoulderLift, model3d.X(-1))

	// 31mm horizontally and 70mm vertically to shoulder pan.
	translate(model3d.XYZ(0, 31, 70))
	rotateMotor(angles.ShoulderPan, model3d.Z(-1))

	return &c
}

// CoordsToAngles attempts to solve the inverse kinematics problem to target
// the given coordinates.
func CoordsToAngles(targets *EndCoords) *MotorAngles {
	var minVec [6]float64
	var maxVec [6]float64
	var delta [6]float64
	for i := range minVec {
		minVec[i] = -math.Pi
		maxVec[i] = math.Pi
		if i == 0 {
			// Constrain pan to front of robot
			minVec[i] = -math.Pi / 2
			maxVec[i] = math.Pi / 2
		}
		if i == 1 {
			// Constrain shoulder lift to in front or above robot
			minVec[i] = 0
		}
		delta[i] = maxVec[i] - minVec[i]
	}
	const segments = 10
	for i := 0; i < 3; i++ {
		best := searchBest(NewMotorAngles(minVec), NewMotorAngles(maxVec), segments, targets)
		for i, x := range best.Vec() {
			delta[i] = 2 * delta[i] / (segments - 1)
			minVec[i] = x - delta[i]
			maxVec[i] = x + delta[i]
		}
	}
	return NewMotorAngles(minVec)
}

func HoverPositionToCoordAngles(
	limitMin, limitMax *MotorAngles,
	centerPos model3d.Coord3D,
	gripperAngle float64,
) *MotorAngles {
	const delta = 0.01

	shoulderPan := math.Atan2(centerPos.X, centerPos.Y)

	var best *MotorAngles
	for shoulder := limitMin.ShoulderLift; shoulder < limitMax.ShoulderLift; shoulder += delta {
		var bestInner *MotorAngles
		var innerDist float64
		for elbow := limitMin.ElbowFlex; elbow < limitMax.ElbowFlex; elbow += delta {
			wrist := math.Pi/2 - (shoulder + elbow)
			if wrist < limitMin.WristFlex || wrist > limitMax.WristFlex {
				continue
			}
			angles := &MotorAngles{
				ShoulderPan:  shoulderPan,
				ShoulderLift: shoulder,
				ElbowFlex:    elbow,
				WristFlex:    wrist,
				WristRoll:    shoulderPan,
				Gripper:      0,
			}
			newPos := AnglesToCoords(angles)
			dist := math.Abs(newPos.LockedFinger.Z - centerPos.Z)
			if bestInner == nil || dist < innerDist {
				innerDist = dist
				bestInner = angles
			}
		}
		if best == nil {
			best = bestInner
			continue
		}

		bestCoord := AnglesToCoords(best).Mid()
		newCoord := AnglesToCoords(bestInner).Mid()
		if bestCoord.Dist(centerPos) > newCoord.Dist(centerPos) {
			best = bestInner
		}
	}
	return best
}

func searchBest(min, max *MotorAngles, segments int, target *EndCoords) *MotorAngles {
	var bestSolution *MotorAngles
	bestDist := math.Inf(1)
	for angles := range enumerateBox(min, max, segments) {
		end := AnglesToCoords(angles)
		dist := math.Pow(end.LockedFinger.Dist(target.LockedFinger), 2) + math.Pow(end.MovingFinger.Dist(target.MovingFinger), 2)
		if dist < bestDist {
			bestDist = dist
			bestSolution = angles
		}
	}
	return bestSolution
}

func enumerateBox(min *MotorAngles, max *MotorAngles, segments int) func(func(*MotorAngles) bool) {
	v1 := min.Vec()
	v2 := max.Vec()
	return func(cb func(*MotorAngles) bool) {
		var recurse func(v [6]float64, idx int) bool
		recurse = func(v [6]float64, idx int) bool {
			if idx == 6 {
				return cb(NewMotorAngles(v))
			}
			delta := (v2[idx] - v1[idx]) / float64(segments-1)
			for j := 0; j < segments; j++ {
				v[idx] = v1[idx] + delta*float64(j)
				if !recurse(v, idx+1) {
					return false
				}
			}
			return true
		}
		recurse([6]float64{}, 0)
	}
}
