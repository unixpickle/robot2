package main

import (
	"flag"
	"log"

	"github.com/unixpickle/model3d/model3d"
	"github.com/unixpickle/robot2/api"
	"github.com/unixpickle/robot2/kinematics"
)

type Lowerer struct {
	MaxZ            float64
	MinZ            float64
	ZDelta          float64
	MinElbowLoad    float64
	MinShoulderLoad float64
	RelShoulderLoad float64
}

func (l *Lowerer) AddFlags() {
	flag.Float64Var(&l.MaxZ, "max-z", 10.0, "initial Z for descent")
	flag.Float64Var(&l.MinZ, "min-z", -110.0, "lowest possible Z to search")
	flag.Float64Var(&l.ZDelta, "z-delta", 5, "delta for lowering incrementally")
	flag.Float64Var(
		&l.MinElbowLoad,
		"min-elbow-load",
		-0.04,
		"once load is below thus we have hit table",
	)
	flag.Float64Var(
		&l.MinShoulderLoad,
		"min-shoulder-load",
		-0.05,
		"once load is below thus we have hit table",
	)
	flag.Float64Var(
		&l.RelShoulderLoad,
		"rel-shoulder-load",
		-0.15,
		"once load changes by this amount, we have hit the table",
	)
}

func (l *Lowerer) Lower(
	c *api.Client,
	centerPos model3d.Coord3D,
	gripperAngle float64,
) (model3d.Coord3D, error) {
	min, max, err := c.LimitsAngles()
	if err != nil {
		return model3d.Coord3D{}, err
	}

	stat, err := c.MotorStatuses()
	if err != nil {
		return model3d.Coord3D{}, err
	}
	startShoulderLoad := stat["shoulder_lift"].Load

	var foundPoint model3d.Coord3D
	for z := l.MaxZ; z > l.MinZ; z -= l.ZDelta {
		centerPos.Z = z
		angles := kinematics.HoverPositionToCoordAngles(min, max, centerPos, gripperAngle)
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

		stat, err := c.MotorStatuses()
		if err != nil {
			return model3d.Coord3D{}, err
		}
		elbowLoad := stat["elbow_flex"].Load
		shoulderLoad := stat["shoulder_lift"].Load
		log.Printf("   * at z %.02f, elbow load %.01f, shoulder load %.02f", z, elbowLoad, shoulderLoad)
		if (shoulderLoad < l.MinShoulderLoad && shoulderLoad < startShoulderLoad+l.RelShoulderLoad) ||
			elbowLoad < l.MinElbowLoad {
			break
		}
	}
	return foundPoint, nil
}
