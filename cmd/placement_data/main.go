// Command placement_data places the object in the gripper in various hover positions,
// homes the robot, captures images, and records all of the images with the corresponding
// placement state.
package main

import (
	"flag"
	"log"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/model3d/model2d"
	"github.com/unixpickle/model3d/model3d"
	"github.com/unixpickle/robot2/api"
	"github.com/unixpickle/robot2/kinematics"
)

func main() {
	var minBaseDist float64
	var minX, maxX float64
	var minY, maxY float64
	var insetDistance float64
	var tableBufferClose float64
	var tableBufferFar float64
	var gripperGrasp float64
	var regraspThreshold float64
	var outputDir string
	var lowerer Lowerer
	var raiser Raiser
	parseClient := api.AddClientFlags()
	lowerer.AddFlags()
	raiser.AddFlags()
	flag.Float64Var(
		&minBaseDist,
		"min-base-dist",
		100,
		"minimum distance from base to place the object",
	)
	flag.Float64Var(&minX, "min-x", -200.0, "min X for random points")
	flag.Float64Var(&maxX, "max-x", 200.0, "max X for random points")
	flag.Float64Var(&minY, "min-y", 0.0, "min Y for random points")
	flag.Float64Var(&maxY, "max-y", 300.0, "max Y for random points")
	flag.Float64Var(
		&insetDistance,
		"inset-distance",
		10,
		"move the locked gripper finger inward by this many mm while regrasping to avoid hitting the edge of the object",
	)
	flag.Float64Var(&tableBufferClose, "table-buffer-close", 12, "z-axis to lift the claw off of the table")
	flag.Float64Var(&tableBufferFar, "table-buffer-far", 30, "z-axis to lift the claw off of the table")
	flag.Float64Var(&gripperGrasp, "gripper-grasp", -10.0*math.Pi/180, "angle to grab the gripped object")
	flag.Float64Var(
		&regraspThreshold,
		"regrasp-threshold",
		4*math.Pi/180,
		"minimum angle for closed gripper to indicate a successful grasp (default to 4 degrees)",
	)
	flag.StringVar(&outputDir, "output-dir", "", "path where samples are saved")
	flag.Parse()

	raiser.ZDelta = lowerer.ZDelta

	if outputDir == "" {
		essentials.Die("must specify -output-dir")
	}

	client, err := parseClient()
	essentials.Must(err)

	cameraTracks, err := client.CameraTrackNames()
	essentials.Must(err)

	min, max, err := client.LimitsAngles()
	essentials.Must(err)

	essentials.Must(client.HomeSafely())

	for {
		xy := model2d.NewCoordRandBounds(model2d.XY(minX, minY), model2d.XY(maxX, maxY))
		if xy.Norm() < minBaseDist {
			// Do not drop too close to the base.
			continue
		}
		gripperAngle := (rand.Float64() - 0.5) * math.Pi / 2
		centerPos := model3d.XYZ(xy.X, xy.Y, lowerer.MaxZ)
		angles := kinematics.HoverPositionToCoordAngles(min, max, centerPos, gripperAngle)
		endCoords := kinematics.AnglesToCoords(angles)
		dist := endCoords.Mid().Dist(centerPos)
		if dist > 2 {
			continue
		}

		log.Printf("Trying point (%f, %f), gripper angle %f", xy.X, xy.Y, gripperAngle)

		log.Println(" - starting camera recordings...")
		recording, err := NewRecording(outputDir)
		essentials.Must(err)
		var recorders []*api.CameraRecorder
		for _, trackName := range cameraTracks {
			r, err := client.RecordCamera(recording.VideoPath(trackName), trackName, 30)
			essentials.Must(err)
			recorders = append(recorders, r)
		}
		// Allow the first keyframe to come in.
		time.Sleep(time.Second)

		log.Println(" - readjusting hand around object...")
		adjustCenter(&lowerer, &raiser, client)

		log.Println(" - moving to initial position...")
		essentials.Must(client.MoveAngles(angles))
		essentials.Must(client.WaitUntilStill())

		log.Println(" - lowering...")
		foundPoint, err := lowerer.Lower(client, centerPos, gripperAngle)
		essentials.Must(err)
		log.Printf(" - found table point %f,%f,%f", foundPoint.X, foundPoint.Y, foundPoint.Z)

		// Relax the motor to avoid pressing the table.
		distFrac := xy.Norm() / model2d.XY(maxX, maxY).Norm()
		tableBuffer := distFrac*tableBufferFar + (1-distFrac)*tableBufferClose
		relaxPoint := relax(client, tableBuffer)
		relaxState, err := client.MotorStatuses()
		essentials.Must(err)

		translation := model2d.XY(-math.Sin(gripperAngle), math.Cos(gripperAngle)).Scale(-insetDistance)
		raiseAngles, err := raiser.OpenAndRaise(client, translation)
		essentials.Must(err)

		log.Println(" - homing...")
		essentials.Must(client.HomeSafely())

		log.Println(" - recording snapshots and data...")
		for _, name := range cameraTracks {
			img, err := client.Snapshot(name)
			essentials.Must(essentials.AddCtx("capture snapshot", err))
			essentials.Must(recording.WriteImage(name, img))
		}
		essentials.Must(recording.WriteJSON("found_point", foundPoint))
		essentials.Must(recording.WriteJSON("relax_point", relaxPoint))
		essentials.Must(recording.WriteJSON("relax_state", relaxState))
		essentials.Must(recording.WriteJSON("target", map[string]any{"Coord": xy, "Gripper": gripperAngle}))

		log.Println(" - attempting re-grip of cube...")
		essentials.Must(raiser.UndoRaise(client, raiseAngles))
		essentials.Must(client.Move("gripper", kinematics.AngleToPosition(gripperGrasp)))
		essentials.Must(client.WaitUntilStill())

		log.Println(" - homing after trajectory...")
		essentials.Must(client.HomeSafely())

		// Hopefully allow video to catch up.
		time.Sleep(time.Second)
		for _, recorder := range recorders {
			essentials.Must(recorder.Stop())
		}

		closedAngles, err := client.CurrentAngles()
		essentials.Must(err)
		log.Printf(" - regrasp angle: %f", closedAngles.Gripper)
		if closedAngles.Gripper < regraspThreshold {
			essentials.Must(recording.MarkSuccessful(false))
			log.Println("failed to re-grasp")
			break
		} else {
			essentials.Must(recording.MarkSuccessful(true))
		}
	}
}

func adjustCenter(lowerer *Lowerer, raiser *Raiser, client *api.Client) {
	min, max, err := client.LimitsAngles()
	essentials.Must(err)
	centerPos := model3d.XYZ(0, 200, lowerer.MaxZ)

	angles := kinematics.HoverPositionToCoordAngles(min, max, centerPos, math.Pi/2)
	essentials.Must(client.MoveAngles(angles))
	lowerer.Lower(client, centerPos, math.Pi/2)
	relax(client, 20)

	// This was manually calibrated around the red cube pickup task.
	trajectory, err := raiser.OpenAndRaise(client, model2d.XY(0, -10))
	essentials.Must(err)
	for i := range trajectory {
		trajectory[i].ShoulderPan = -3 * math.Pi / 180
		trajectory[i].ShoulderLift += 4 * math.Pi / 180
		trajectory[i].WristRoll = 0
	}
	essentials.Must(raiser.UndoRaise(client, trajectory))
}

func relax(client *api.Client, tableBuffer float64) model3d.Coord3D {
	stat, err := client.MotorStatuses()
	essentials.Must(err)
	for name, status := range stat {
		essentials.Must(client.Move(name, uint16(status.Position)))
	}
	essentials.Must(client.WaitUntilStill())

	// Try to lift a tiny bit off the table to avoid pressure.
	min, max, err := client.LimitsAngles()
	essentials.Must(err)
	angles, err := client.CurrentAngles()
	essentials.Must(err)
	pos := kinematics.AnglesToCoords(angles).Mid()
	pos.Z += tableBuffer
	targetAngles := kinematics.HoverPositionToCoordAngles(min, max, pos, 0)
	angles.ShoulderPan = targetAngles.ShoulderPan
	angles.ShoulderLift = targetAngles.ShoulderLift
	angles.ElbowFlex = targetAngles.ElbowFlex
	angles.WristFlex = targetAngles.WristFlex
	essentials.Must(client.MoveAngles(angles))
	essentials.Must(client.WaitUntilStill())
	return pos
}

func openAndRaise(
	client *api.Client,
	gripperRelease,
	delta,
	totalRaise float64,
	translation model2d.Coord,
) func() {
	min, max, err := client.LimitsAngles()
	essentials.Must(err)

	// Record initial position before opening gripper so as not to mess
	// with the midpoint reading.
	angles, err := client.CurrentAngles()
	essentials.Must(err)
	startPos := kinematics.AnglesToCoords(angles).Mid()
	startRelativeWristRoll := angles.WristRoll - angles.ShoulderPan

	intermediateAngles := []*kinematics.MotorAngles{angles}

	// First, open gripper
	essentials.Must(client.Move("gripper", kinematics.AngleToPosition(gripperRelease)))
	essentials.Must(client.WaitUntilStill())

	angles, err = client.CurrentAngles()
	essentials.Must(err)
	intermediateAngles = append(intermediateAngles, angles)

	// Gradually lower.
	for z := 0.0; z < totalRaise; z += delta {
		pos := startPos
		pos.Z += z + delta

		frac := math.Min(1, z/totalRaise)
		pos.X += frac * translation.X
		pos.Y += frac * translation.Y

		angles = kinematics.HoverPositionToCoordAngles(min, max, pos, startRelativeWristRoll)
		angles.Gripper = gripperRelease
		essentials.Must(client.MoveAngles(angles))
		essentials.Must(client.WaitUntilStill())
		intermediateAngles = append(intermediateAngles, angles)
	}

	return func() {
		for _, angles := range slices.Backward(intermediateAngles) {
			essentials.Must(client.MoveAngles(angles))
			essentials.Must(client.WaitUntilStill())
		}
	}
}
