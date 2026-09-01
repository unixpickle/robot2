// Command calibrate_single helps the user calibrate a single motor that is
// connected directly through the motor bus, without relying on the server.
//
// This can be useful for replacing a single motor without recalibrating the
// entire system.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/robot2/motors"
)

func main() {
	var motorPort string
	var motorTimeout time.Duration
	var motorID int

	flag.StringVar(&motorPort, "motor-port", "", "path to motorbus serial port")
	flag.DurationVar(&motorTimeout, "motor-timeout", time.Second*2, "motor serial read timeout")
	flag.IntVar(&motorID, "motor-id", -1, "ID of the motor to calibrate")
	flag.Parse()

	if motorID == -1 {
		essentials.Die("must pass -motor-id flag")
	}

	log.Println("connecting to motors...")
	motorConn, err := motors.NewConnection(motorPort, motorTimeout)
	if err != nil {
		log.Fatalln("failed to connect to motors:", err)
	}

	essentials.Must(motorConn.SetTorqueEnabled(uint8(motorID), false))
	defer motorConn.SetTorqueEnabled(uint8(motorID), true)

	fmt.Println("Center the motor and then hit enter.")
	essentials.Must(waitNewline())
	essentials.Must(motorConn.CenterPosition(uint8(motorID)))

	fmt.Println("Now move the motor between its two physical extremes.")
	fmt.Println("Hit enter when done.")

	endChan := make(chan error, 1)
	go func() {
		endChan <- waitNewline()
	}()

	lastPrint := time.Time{}
	min := int16(2048)
	max := int16(2048)
LimitLoop:
	for {
		select {
		case <-endChan:
			break LimitLoop
		default:
		}
		status, err := motorConn.MotorStatus(uint8(motorID))
		essentials.Must(err)
		pos := status.Position
		if pos < min {
			min = pos
		}
		if pos > max {
			max = pos
		}
		if time.Since(lastPrint) > time.Second {
			lastPrint = time.Now()
			fmt.Printf("min=%04d max=%04d   \r", min, max)
		}
	}
	fmt.Println()
	essentials.Must(motorConn.SetPositionLimit(uint8(motorID), uint16(min), uint16(max)))
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
