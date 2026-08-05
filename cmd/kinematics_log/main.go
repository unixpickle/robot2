package main

import (
	"context"
	"flag"
	"log"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/robot2/api"
	"github.com/unixpickle/robot2/motors"
)

func main() {
	parseClient := api.AddClientFlags()
	flag.Parse()

	client, err := parseClient()
	essentials.Must(err)

	stream, errCh := client.StreamStatuses(context.Background())
	for status := range stream {
		pos := motors.MotorAnglesFromStatuses(status)
		coords := motors.AnglesToCoords(pos)
		log.Printf(
			"moving: (%.02f, %.02f, %.02f)  locked: (%.02f, %.02f, %.02f)",
			coords.MovingFinger.X, coords.MovingFinger.Y, coords.MovingFinger.Z,
			coords.LockedFinger.X, coords.LockedFinger.Y, coords.LockedFinger.Z,
		)
	}
	essentials.Must(<-errCh)
}
