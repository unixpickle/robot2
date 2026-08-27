package main

import (
	"encoding/json"
	"errors"
	"math"
	"os"

	"github.com/unixpickle/model3d/model2d"
	"github.com/unixpickle/model3d/model3d"
)

type TableFit struct {
	FitPoints []model3d.Coord3D
}

func LoadTableFit(path string) (*TableFit, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result TableFit
	if err := json.Unmarshal(data, &result.FitPoints); err != nil {
		return nil, err
	}
	if len(result.FitPoints) < 1 {
		return nil, errors.New("no table fit points loaded")
	}
	return &result, nil
}

func (t *TableFit) ZForPoint(xy model2d.Coord) float64 {
	var nearest model3d.Coord3D
	distance := math.Inf(1)
	for _, p := range t.FitPoints {
		d := p.XY().Dist(xy)
		if d < distance {
			nearest = p
			distance = d
		}
	}
	return nearest.Z
}
