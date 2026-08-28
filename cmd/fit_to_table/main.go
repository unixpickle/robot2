// Command fit_to_table attempts to push the arm against the table repeatedly
// at different positions to show how well the kinematics model is tuned.
//
// It records a height map which can be used for the placement_data script.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/model3d/model2d"
	"github.com/unixpickle/model3d/model3d"
	"github.com/unixpickle/robot2/api"
	"github.com/unixpickle/robot2/kinematics"
)

func main() {
	var maxZ, minZ, gridDelta, zDelta, minElbowLoad, minShoulderLoad float64
	var outputPath string
	parseClient := api.AddClientFlags()
	flag.Float64Var(&minZ, "min-z", -110.0, "lowest possible Z to search")
	flag.Float64Var(&maxZ, "max-z", 10.0, "initial Z for descent")
	flag.Float64Var(&gridDelta, "grid-delta", 25.0, "delta for coordinate grid search")
	flag.Float64Var(&zDelta, "z-delta", 10, "delta for lowering incrementally")
	flag.Float64Var(
		&minElbowLoad,
		"min-elbow-load",
		-0.04,
		"once load is below thus we have hit table",
	)
	flag.Float64Var(
		&minShoulderLoad,
		"min-shoulder-load",
		-0.1,
		"once load is below thus we have hit table",
	)
	flag.StringVar(&outputPath, "output-path", "", "path to calibration data output")
	flag.Parse()

	if outputPath == "" {
		essentials.Die("Must specify -output-path. See -help.")
	}

	client, err := parseClient()
	essentials.Must(err)

	min, max, err := client.LimitsAngles()
	essentials.Must(err)

	log.Println("homing...")
	essentials.Must(client.HomeSafely())

	var inPoints []model2d.Coord
	for x := -200.0; x < 200; x += gridDelta {
		for y := 0.0; y < 300; y += gridDelta {
			if model2d.XY(x, y).Norm() < 100 {
				// Do not drop too close to the base.
				continue
			}
			centerPos := model3d.XYZ(x, y, maxZ)
			angles := kinematics.HoverPositionToCoordAngles(min, max, centerPos, 0)
			if angles == nil {
				continue
			}
			endCoords := kinematics.AnglesToCoords(angles)
			dist := endCoords.Mid().Dist(centerPos)
			if dist > 2 {
				continue
			}
			inPoints = append(inPoints, model2d.XY(x, y))
		}
	}

	var results []model3d.Coord3D
	for i, xy := range inPoints {
		centerPos := model3d.XYZ(xy.X, xy.Y, maxZ)
		angles := kinematics.HoverPositionToCoordAngles(min, max, centerPos, 0)
		log.Printf("trying coordinate %d/%d (%f, %f)", i+1, len(inPoints), xy.X, xy.Y)
		essentials.Must(client.MoveAngles(angles))
		essentials.Must(client.WaitUntilStill())

		var foundPoint model3d.Coord3D
		for z := maxZ; z > minZ; z -= zDelta {
			centerPos.Z = z
			angles = kinematics.HoverPositionToCoordAngles(min, max, centerPos, 0)
			if angles == nil {
				essentials.Die("IK failed during lower")
			}
			essentials.Must(client.MoveAngles(angles))
			essentials.Must(client.WaitUntilStill())
			state, err := client.CurrentAngles()
			essentials.Must(err)
			ps := kinematics.AnglesToCoords(state)
			p := ps.LockedFinger
			if ps.MovingFinger.Z < ps.LockedFinger.Z {
				p = ps.MovingFinger
			}
			foundPoint = p
			stat, err := client.MotorStatuses()
			essentials.Must(err)
			elbowLoad := stat["elbow_flex"].Load
			shoulderLoad := stat["shoulder_lift"].Load
			log.Printf(" - at z %f, elbow load %f, shoulder load %f", z, elbowLoad, shoulderLoad)
			if elbowLoad < minElbowLoad || shoulderLoad < minShoulderLoad {
				break
			}
		}
		log.Printf(" - found point %f,%f,%f", foundPoint.X, foundPoint.Y, foundPoint.Z)
		results = append(results, foundPoint)
		essentials.Must(client.HomeSafely())
	}

	data, err := json.Marshal(results)
	essentials.Must(err)
	essentials.Must(os.WriteFile(outputPath, data, 0644))
}
