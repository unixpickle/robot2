// Command ik_move attempts to move the fingers to specified coordinates.
// If only one coordinate is given, then a hover position is found.
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
	"github.com/unixpickle/robot2/kinematics"
)

func main() {
	parseClient := api.AddClientFlags()
	flag.Usage = func() {
		fmt.Fprintf(
			flag.CommandLine.Output(),
			"Usage: %s [flags] [<locked_x1,y1,z1> <moving_x2,y2,z2> | <hover_x,y,z>",
			os.Args[0],
		)
		flag.PrintDefaults()
		essentials.Die()
	}
	flag.Parse()

	if len(flag.Args()) > 2 || len(flag.Args()) == 0 {
		flag.Usage()
	}

	client, err := parseClient()
	essentials.Must(err)

	var angles *kinematics.MotorAngles
	if len(flag.Args()) == 2 {
		lockedPos := parseCoord(flag.Args()[0])
		movingPos := parseCoord(flag.Args()[1])
		angles = kinematics.CoordsToAngles(&kinematics.EndCoords{
			LockedFinger: lockedPos,
			MovingFinger: movingPos,
		})
		endCoords := kinematics.AnglesToCoords(angles)
		lockedDist := endCoords.LockedFinger.Dist(lockedPos)
		movingDist := endCoords.MovingFinger.Dist(movingPos)
		log.Printf("solved: locked distance %f, moving distance %f", lockedDist, movingDist)
	} else {
		centerPos := parseCoord(flag.Args()[0])
		min, max, err := client.LimitsAngles()
		essentials.Must(err)
		angles = kinematics.HoverPositionToCoordAngles(min, max, centerPos, 0)
		if angles == nil {
			log.Fatal("IK failed for point")
		}
		endCoords := kinematics.AnglesToCoords(angles)
		log.Printf("solved: distance %f", endCoords.Mid().Dist(centerPos))
	}

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
