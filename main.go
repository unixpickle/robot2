package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/unixpickle/robot2/camera"
	"github.com/unixpickle/robot2/motors"
)

type Server struct {
	Camera *camera.CameraController
}

func main() {
	var cameras string
	var rtcTimeout time.Duration
	var webDir string
	var addr string
	var motorPort string
	var motorTimeout time.Duration

	flag.StringVar(
		&cameras,
		"cameras",
		"top:/dev/v4l/by-id/usb-BC-231220-A_XWF-1080P-video-index0,bottom:/dev/v4l/by-id/usb-webcam_1080P_webcam_1080P_202601081445001-video-index0",
		"comma-separated list of cameras and names",
	)
	flag.DurationVar(&rtcTimeout, "rtc-timeout", time.Minute, "RTC session timeout")
	flag.StringVar(&webDir, "web-dir", "web/dist", "static asset directory")
	flag.StringVar(&addr, "addr", ":1337", "address to listen on")
	flag.StringVar(&motorPort, "motor-port", "/dev/ttyACM0", "path to motorbus serial port")
	flag.DurationVar(&motorTimeout, "motor-timeout", time.Second*2, "motor serial read timeout")
	flag.Parse()

	log.Println("connecting to motors...")
	motorConn, err := motors.NewConnection(motorPort, motorTimeout)
	if err != nil {
		log.Fatalln("failed to connect to motors:", err)
	}
	log.Println("getting motor statuses...")
	statuses, err := motorConn.MotorStatuses(6)
	if err != nil {
		log.Fatalln("failed to get motor statuses:", err)
	}
	for i, status := range statuses {
		log.Printf("motor %d initial status: %s", i, status)
	}

	log.Println("opening cameras...")
	var tracks []*camera.CameraTrack
	for _, cameraStr := range strings.Split(cameras, ",") {
		parts := strings.Split(cameraStr, ":")
		if len(parts) != 2 {
			log.Fatal("invaild camera string: " + cameraStr)
		}
		name, path := parts[0], parts[1]
		log.Printf("loading camera %s at %s...", name, path)
		cam, err := camera.NewV4L2Camera(path)
		if err != nil {
			log.Fatalln("failed to open camera:", err)
		}
		track, err := camera.NewCameraTrack(cam, name, context.Background())
		if err != nil {
			log.Fatal("failed to create WebRTC track for camera:", err)
		}
		tracks = append(tracks, track)
	}
	camController := camera.NewCameraController(tracks, rtcTimeout)
	motorController, err := motors.NewMotorController(motorConn)
	if err != nil {
		log.Fatalf("failed to create motor controller: %s", err)
	}
	http.Handle("/camera/", http.StripPrefix("/camera", camController))
	http.Handle("/motor/", http.StripPrefix("/motor", motorController))
	http.Handle("/", http.FileServer(http.Dir(webDir)))

	log.Printf("attempting to listen at %s...", addr)
	http.ListenAndServe(addr, nil)
}
