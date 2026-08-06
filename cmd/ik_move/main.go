// Command fit_to_table attempts to push the arm against the table repeatedly
// at different positions to fine-tune the motor calibration by forcing all of
// the found points to line up at the same z value.
//
// After the command is complete, the command will move the arm to the upright
// position under the new angles, such that a further calibration command could
// be run.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/model3d/model3d"
	"github.com/unixpickle/robot2/api"
	"github.com/unixpickle/robot2/motors"
)

func main() {
	parseClient := api.AddClientFlags()
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: %s [flags] <locked_x1,y1,z1> <moving_x2,y2,z2>", os.Args[0])
		flag.PrintDefaults()
		essentials.Die()
	}
	flag.Parse()

	if len(flag.Args()) != 2 {
		flag.Usage()
	}
	lockedPos := parseCoord(flag.Args()[0])
	movingPos := parseCoord(flag.Args()[1])

	angles := motors.CoordsToAngles(&motors.EndCoords{LockedFinger: lockedPos, MovingFinger: movingPos})
	endCoords := motors.AnglesToCoords(angles)
	lockedDist := endCoords.LockedFinger.Dist(lockedPos)
	movingDist := endCoords.MovingFinger.Dist(movingPos)
	log.Printf("solved: locked distance %f, moving distance %f", lockedDist, movingDist)

	client, err := parseClient()
	essentials.Must(err)
	client.MoveAngles(angles)
}

func parseCoord(coord string) model3d.Coord3D {
	parts := strings.Split(coord, ",")
	if len(parts) != 3 {
		essentials.Die("invalid coord:", coord)
	}
	var result [3]float64
	for i, x := range parts {
		data, err := strconv.ParseFloat(x, 64)
		if err != nil {
			essentials.Die("invalid coord:", coord)
		}
		result[i] = data
	}
	return model3d.NewCoord3DArray(result)
}
