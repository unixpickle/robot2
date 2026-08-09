package kinematics

import (
	"math"

	"github.com/unixpickle/model3d/model3d"
)

// HoverPositionToCoordAngles solves an IK problem where the
// gripper is held pointing down towards the table and optionally
// rotated by an absolute angle (where 0 always faces the Y+ axis,
// rather than in the direction the arm happens to be facing.)
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
				WristRoll:    shoulderPan + gripperAngle,
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
