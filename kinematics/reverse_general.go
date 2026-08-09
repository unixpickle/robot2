package kinematics

import "math"

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
