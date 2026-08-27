package main

import (
	"flag"
	"math"
	"slices"

	"github.com/unixpickle/model3d/model2d"
	"github.com/unixpickle/robot2/api"
	"github.com/unixpickle/robot2/kinematics"
)

type Raiser struct {
	ZDelta         float64
	GripperRelease float64
	TotalRaise     float64
}

func (r *Raiser) AddFlags() {
	flag.Float64Var(&r.ZDelta, "z-delta", 5, "z delta during raise")
	flag.Float64Var(&r.GripperRelease, "gripper-release", math.Pi/2, "gripper release angle")
	flag.Float64Var(&r.TotalRaise, "lift-amount", 40, "lift the hand this much after releasing")
}

func (r *Raiser) OpenAndRaise(
	c *api.Client,
	translation model2d.Coord,
) ([]*kinematics.MotorAngles, error) {
	min, max, err := c.LimitsAngles()
	if err != nil {
		return nil, err
	}

	// Record initial position before opening gripper so as not to mess
	// with the midpoint reading.
	angles, err := c.CurrentAngles()
	if err != nil {
		return nil, err
	}
	startPos := kinematics.AnglesToCoords(angles).Mid()
	startRelativeWristRoll := angles.WristRoll - angles.ShoulderPan

	intermediateAngles := []*kinematics.MotorAngles{angles}

	// First, open gripper
	angles = kinematics.HoverPositionToCoordAngles(min, max, startPos, startRelativeWristRoll)
	angles.Gripper = r.GripperRelease
	if err := c.MoveAngles(angles); err != nil {
		return nil, err
	}
	if err := c.WaitUntilStill(); err != nil {
		return nil, err
	}

	angles, err = c.CurrentAngles()
	if err != nil {
		return nil, err
	}
	intermediateAngles = append(intermediateAngles, angles)

	for z := 0.0; z < r.TotalRaise; z += r.ZDelta {
		pos := startPos
		pos.Z += z + r.ZDelta

		frac := math.Min(1, z/r.TotalRaise)
		pos.X += frac * translation.X
		pos.Y += frac * translation.Y

		angles = kinematics.HoverPositionToCoordAngles(min, max, pos, startRelativeWristRoll)
		angles.Gripper = r.GripperRelease
		if err := c.MoveAngles(angles); err != nil {
			return nil, err
		}
		if err := c.WaitUntilStill(); err != nil {
			return nil, err
		}
		intermediateAngles = append(intermediateAngles, angles)
	}

	return intermediateAngles, nil
}

func (r *Raiser) UndoRaise(c *api.Client, allAngles []*kinematics.MotorAngles) error {
	for _, angles := range slices.Backward(allAngles) {
		if err := c.MoveAngles(angles); err != nil {
			return err
		}
		if err := c.WaitUntilStill(); err != nil {
			return err
		}
	}
	return nil
}
