package kinematics

import "math"

// PlanSafeHome creates a trajectory that raises the arm into the
// home position without scratching the table.
func PlanSafeHome(min, max *MotorAngles, start *MotorAngles) []*MotorAngles {
	isValid := func(c *MotorAngles) bool {
		minVec := min.Vec()
		maxVec := max.Vec()
		for i, x := range c.Vec() {
			if x < minVec[i] || x > maxVec[i] {
				return false
			}
		}
		return true
	}

	var result []*MotorAngles
	curPos := *start

	// Clamp initial state to bounds, which it should already be, but
	// this way we ensure we won't find no solutions.
	v := curPos.Vec()
	for i, x := range v {
		v[i] = math.Max(min.Vec()[i], math.Min(max.Vec()[i], x))
	}
	curPos.SetVec(v)

	// Initially relax the joint by moving targets to the current state.
	result = append(result, curPos.Copy())

	// Initial raise that lowers the fingers as little as possible while
	// pointing the shoulder and elbow into the air.
	for AnglesToCoords(&curPos).Min().Z < 50 {
		var candidates []MotorAngles

		if curPos.ShoulderLift > 0 {
			raiseShoulder := curPos
			raiseShoulder.ShoulderLift -= math.Pi / 20.0
			candidates = append(candidates, raiseShoulder)
		}

		if curPos.ElbowFlex > -math.Pi/2 {
			raiseElbow := curPos
			raiseElbow.ElbowFlex -= math.Pi / 20.0
			candidates = append(candidates, raiseElbow)
		}

		highestZ := math.Inf(-1)
		best := MotorAngles{}
		found := false
		for _, c := range candidates {
			if !isValid(&c) {
				continue
			}
			z := AnglesToCoords(&curPos).Min().Z
			if z > highestZ {
				highestZ = z
				best = c
				found = true
			}
		}
		if !found {
			break
		}
		curPos = best
		result = append(result, curPos.Copy())
	}

	// Now that the arm is raised, we should home the remaining positions
	curPos.WristFlex = 0
	curPos.WristRoll = 0
	curPos.Gripper = 0
	curPos.ShoulderPan = 0
	result = append(result, curPos.Copy())

	curPos.ShoulderLift = 0
	curPos.ElbowFlex = 0
	result = append(result, curPos.Copy())

	return result
}
