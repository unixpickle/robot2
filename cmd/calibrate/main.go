// Command calibrate guides the user through zeroing out the robot's motor
// positions and finding suitable bounds for each motor's movement.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/robot2/api"
	"github.com/unixpickle/robot2/motors"
)

func main() {
	parseClient := api.AddClientFlags()
	flag.Parse()

	client, err := parseClient()
	essentials.Must(err)

	essentials.Must(client.SetTorqueEnabled(false))
	defer client.SetTorqueEnabled(true)

	fmt.Println("Move the joints such that:")
	fmt.Println(" * The shoulder pan is pointed towards the front center.")
	fmt.Println(" * The shoulder is pointing straight up, elbow bent at 90 degrees, and wrist straight forward.")
	fmt.Println(" * The fingers of the gripper are parallel.")
	fmt.Println(" * The wrist roll is fully centered (motor will be facing upward).")
	fmt.Println("Hit enter once complete.")
	essentials.Must(waitNewline())
	essentials.Must(client.CalibrateCenter())

	fmt.Println("Now move the joints to reach their corresponding extremes.")
	fmt.Println("At each extreme, leave for a few seconds to make sure it is recorded.")
	fmt.Println("Hit enter once complete.")

	endChan := make(chan error, 1)
	go func() {
		endChan <- waitNewline()
	}()

	motorNames, err := client.MotorNames()
	essentials.Must(err)

	limits := map[string]motors.MotorLimit{}
	for _, name := range motorNames {
		limits[name] = motors.MotorLimit{Min: 2048, Max: 2048}
	}
	printLimits := func() {
		fmt.Printf("limits:")
		for _, name := range motorNames {
			limit := limits[name]
			fmt.Printf(" [%04d, %04d]", limit.Min, limit.Max)
		}
	}
	printLimits()
	for {
		status, err := client.MotorStatuses()
		essentials.Must(err)
		for name, info := range status {
			pos := uint16(info.Position)
			limit := limits[name]
			if pos < limit.Min {
				limit.Min = pos
			}
			if pos > limit.Max {
				limit.Max = pos
			}
			limits[name] = limit
		}
		fmt.Printf("\r")
		printLimits()
		time.Sleep(time.Millisecond * 15)
		select {
		case err := <-endChan:
			essentials.Must(err)
			essentials.Must(client.SetLimits(limits))
			return
		default:
		}
	}
}

func waitNewline() error {
	for {
		var data [1]byte
		if n, err := os.Stdin.Read(data[:]); err != nil {
			return err
		} else if n == 0 {
			continue
		}
		if data[0] == '\n' {
			return nil
		}
	}
}
