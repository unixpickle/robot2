package main

import (
	"flag"

	"github.com/unixpickle/model3d/model2d"
	"github.com/unixpickle/model3d/model3d"
	"github.com/unixpickle/robot2/api"
	"github.com/unixpickle/robot2/kinematics"
)

type Lowerer struct {
	Fit         *TableFit
	StartHeight float64
	EndHeight   float64
	Steps       int
}

func (l *Lowerer) AddFlags() {
	flag.Float64Var(&l.StartHeight, "lower-start-height", 100, "start the lower at this height")
	flag.Float64Var(&l.EndHeight, "lower-end-height", 5, "end the lower at this height")
	flag.IntVar(&l.Steps, "lower-steps", 5, "split the lower into this many steps")
}

func (l *Lowerer) Lower(
	c *api.Client,
	centerPos model2d.Coord,
	gripperAngle float64,
) (model3d.Coord3D, error) {
	min, max, err := c.LimitsAngles()
	if err != nil {
		return model3d.Coord3D{}, err
	}

	z := l.Fit.ZForPoint(centerPos)
	delta := (l.StartHeight - l.EndHeight) / float64(l.Steps)

	var foundPoint model3d.Coord3D
	for i := 0; i <= l.Steps; i++ {
		p := model3d.XYZ(centerPos.X, centerPos.Y, z+delta*float64(l.Steps-i))
		angles := kinematics.HoverPositionToCoordAngles(min, max, p, gripperAngle)
		if angles == nil {
			panic("IK failed")
		}
		if err := c.MoveAngles(angles); err != nil {
			return model3d.Coord3D{}, err
		}
		if err := c.WaitUntilStill(); err != nil {
			return model3d.Coord3D{}, err
		}
		state, err := c.CurrentAngles()
		if err != nil {
			return model3d.Coord3D{}, err
		}
		foundPoint = kinematics.AnglesToCoords(state).Mid()
	}
	return foundPoint, nil
}
