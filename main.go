package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/unixpickle/robot2/camera"
)

type Server struct {
	RTC *camera.WebRTCSessions
}

func main() {
	var cameras string
	var rtcTimeout time.Duration
	var webDir string
	var addr string

	flag.StringVar(
		&cameras,
		"cameras",
		"top:/dev/v4l/by-id/usb-BC-231220-A_XWF-1080P-video-index0,bottom:/dev/v4l/by-id/usb-webcam_1080P_webcam_1080P_202601081445001-video-index0",
		"comma-separated list of cameras and names",
	)
	flag.DurationVar(&rtcTimeout, "rtc-timeout", time.Minute, "RTC session timeout")
	flag.StringVar(&webDir, "web-dir", "web/dist", "static asset directory")
	flag.StringVar(&addr, "addr", ":1337", "address to listen on")
	flag.Parse()

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
	sessions := camera.NewWebRTCSessions(tracks, rtcTimeout)
	http.Handle("/camera/", http.StripPrefix("/camera", sessions))
	http.Handle("/", http.FileServer(http.Dir(webDir)))

	log.Printf("attempting to listen at %s...", addr)
	http.ListenAndServe(addr, nil)
}
