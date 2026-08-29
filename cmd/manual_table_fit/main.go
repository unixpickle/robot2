// Command manual_table_fit uses user feedback to identify a function of table
// z axis versus distance from arm base.
//
// Produces a nearest neighbors file similar to fit_to_table, but with extrapolated
// values for various rotations.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"log"
	"math"
	"os"
	"strings"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/model3d/model2d"
	"github.com/unixpickle/model3d/model3d"
	"github.com/unixpickle/robot2/api"
	"github.com/unixpickle/robot2/kinematics"
)

func main() {
	var minRadius, maxRadius float64
	var radiusDelta float64
	var startZ float64
	var zDelta float64
	var outputPath string
	parseClient := api.AddClientFlags()
	flag.Float64Var(&minRadius, "min-radius", 90, "lowest radius to check")
	flag.Float64Var(&maxRadius, "max-radius", 400, "highest radius to check")
	flag.Float64Var(&radiusDelta, "radius-delta", 40.0, "radius increase step")
	flag.Float64Var(&startZ, "start-z", -30, "z to start lowering from")
	flag.Float64Var(&zDelta, "z-delta", 5, "delta for lowering incrementally")
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

	reader := bufio.NewReader(os.Stdin)

	var radiusToZ [][2]float64
	for radius := minRadius; radius < maxRadius; radius += radiusDelta {
		centerPos := model3d.XYZ(0, radius, startZ)
		angles := kinematics.HoverPositionToCoordAngles(min, max, centerPos, 0)
		if angles == nil {
			continue
		}
		endCoords := kinematics.AnglesToCoords(angles)
		dist := endCoords.Mid().Dist(centerPos)
		if dist > 2 {
			continue
		}

		log.Printf("trying radius %f", radius)
		for z := startZ; true; z -= zDelta {
			centerPos.Z = z
			angles = kinematics.HoverPositionToCoordAngles(min, max, centerPos, 0)
			if angles == nil {
				essentials.Die("IK failed during lower")
			}
			essentials.Must(client.MoveAngles(angles))
			essentials.Must(client.WaitUntilStill())
			log.Printf("moved to z %f, hit enter to continue, or type x and hit enter if at table...", z)

			line, err := reader.ReadString('\n')
			essentials.Must(err)
			if strings.HasPrefix(line, "x") {
				log.Printf("found z %f", z)
				radiusToZ = append(radiusToZ, [2]float64{radius, z})
				break
			}
		}
	}

	var points []model3d.Coord3D
	for _, pair := range radiusToZ {
		radius, z := pair[0], pair[1]
		for theta := -math.Pi; theta < math.Pi; theta += 0.1 {
			point := model2d.Rotation(theta).Apply(model2d.X(radius))
			points = append(points, model3d.XYZ(point.X, point.Y, z))
		}
	}

	data, err := json.Marshal(points)
	essentials.Must(err)
	essentials.Must(os.WriteFile(outputPath, data, 0644))
}
