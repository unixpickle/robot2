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
	"log"
	"math"
	"sync"
	"time"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/robot2/api"
	"github.com/unixpickle/robot2/motors"
)

func main() {
	var maxChange, delta float64
	parseClient := api.AddClientFlags()
	flag.Float64Var(&maxChange, "max-change", 5.0, "maximum absolute change from the baseline calibration to make")
	flag.Float64Var(&delta, "delta", 0.2, "delta for grid search")
	flag.Parse()

	client, err := parseClient()
	essentials.Must(err)

	// For any set of angles, we want elbow+shoulder+wrist > T,
	// where T can be some angle threshold like 130 degrees, so that the tip of
	// the claw is actually likely to be touching the table and not some other
	// part of the arm.
	// Each position is a shoulder_lift, elbow_flex, and wrist_flex expressed
	// as an angle in degrees, encoded first as a start and then as a target
	// that should surely be on the table.
	tryPositions := [][2][3]float64{
		{{10, 90, 0}, {45, 90, 0}},
		{{45, 45, 90}, {80, 45, 90}},
		{{41, 73, 0}, {75, 73, 0}},
		{{41, 73, 0}, {41, 80, 50}},
		{{41, 73, 0}, {41, 100, 10}},
		{{41, 73, 0}, {35, 100, 20}},
	}

	thetaToPos := func(theta float64) int16 {
		return motors.AngleToPosition(theta * math.Pi / 180)
	}
	goToTargets := func(p [3]float64) *motors.MotorAngles {
		// Center wrist to ensure the locked finger hits the table first.
		client.Move("wrist_roll", 2048)

		client.Move("shoulder_lift", thetaToPos(p[0]))
		client.Move("elbow_flex", thetaToPos(p[1]))
		client.Move("wrist_flex", thetaToPos(p[2]))
		return motors.MotorAnglesFromStatuses(waitForStop(client))
	}

	var onTableAngles []*motors.MotorAngles
	for _, targets := range tryPositions {
		log.Printf("entering initial target %#v", targets[0])
		goToTargets(targets[0])
		log.Printf("entering table target %#v", targets[1])
		final := goToTargets(targets[1])
		onTableAngles = append(onTableAngles, final)

		log.Printf("z value %f; homing after pose", motors.AnglesToCoords(final).LockedFinger.Z)
		goToTargets([3]float64{})
	}

	log.Printf("initial Z variance is %f", varianceForStep(onTableAngles, &motors.MotorAngles{}))

	deltas := []float64{}
	for x := -maxChange; x < maxChange; x += delta {
		deltas = append(deltas, x)
	}

	var lock sync.Mutex
	var bestDelta motors.MotorAngles
	bestVariance := math.Inf(1)

	essentials.ConcurrentMap(0, len(deltas)*len(deltas)*len(deltas), func(i int) {
		shoulderDelta := deltas[i%len(deltas)]
		i /= len(deltas)
		elbowDelta := deltas[i%len(deltas)]
		i /= len(deltas)
		wristDelta := deltas[i]

		ds := motors.MotorAngles{
			ShoulderLift: shoulderDelta,
			ElbowFlex:    elbowDelta,
			WristFlex:    wristDelta,
		}
		variance := varianceForStep(onTableAngles, &ds)
		lock.Lock()
		if variance < bestVariance {
			bestVariance = variance
			bestDelta = ds
		}
		lock.Unlock()
	})

	log.Printf("final Z variance is %f", bestVariance)
	log.Printf("best delta is %f", bestDelta)

	log.Println("going to new homed target...")
	essentials.Must(client.Move("wrist_roll", 2048))
	essentials.Must(client.Move("gripper", 2048))
	essentials.Must(client.Move("shoulder_pan", 2048))
	goToTargets([3]float64{bestDelta.ShoulderLift, bestDelta.ElbowFlex, bestDelta.WristFlex})

	log.Println("creating new home center...")
	time.Sleep(time.Second * 2)
	client.CalibrateCenter()
}

func varianceForStep(angles []*motors.MotorAngles, deltas *motors.MotorAngles) float64 {
	var sum, sqSum float64
	for _, ang := range angles {
		added := *ang
		added.Add(deltas)
		z := motors.AnglesToCoords(&added).LockedFinger.Z
		sum += z
		sqSum += z * z
	}
	mean := sum / float64(len(angles))
	sqMean := sqSum / float64(len(angles))
	return sqMean - mean*mean
}

func waitForStop(c *api.Client) map[string]*motors.AnnotatedStatus {
	// Allow initial motion to start.
	time.Sleep(time.Second)

	for {
		statuses, err := c.MotorStatuses()
		essentials.Must(err)
		allDone := true
		for _, s := range statuses {
			if s.IsMoving() {
				allDone = false
			}
		}
		if allDone {
			return statuses
		}
		time.Sleep(time.Second)
	}
}
