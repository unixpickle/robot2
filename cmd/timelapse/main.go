// Command timelapse records a timelapse of all of the cameras
// on the laptop, encoding the results to one video file per
// camera.
package main

import (
	"bytes"
	"context"
	"flag"
	"image"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/ffmpego"
	"github.com/unixpickle/robot2/api"
)

func main() {
	var outputDir string
	var secondsPerFrame float64
	var framesPerSecond float64
	parseClient := api.AddClientFlags()
	flag.StringVar(&outputDir, "output-dir", "", "output directory for recordings")
	flag.Float64Var(&secondsPerFrame, "seconds-per-frame", 1.0, "seconds between frame captures")
	flag.Float64Var(&framesPerSecond, "frames-per-second", 24, "FPS in metadata of exported video")
	flag.Parse()

	if outputDir == "" {
		essentials.Die("must specify -output-dir; see -help")
	}

	essentials.Must(os.MkdirAll(outputDir, 0755))

	client, err := parseClient()
	essentials.Must(err)

	trackNames, err := client.CameraTrackNames()
	essentials.Must(err)

	exitCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() {
		// Prevent catching more than one Ctrl+C to allow force exit.
		<-exitCtx.Done()
		stop()
	}()

	var wg sync.WaitGroup
	for _, trackName := range trackNames {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outputPath := filepath.Join(outputDir, trackName+".mp4")
			RecordTrackTimelapse(
				exitCtx,
				client,
				trackName,
				time.Duration(float64(time.Second)*secondsPerFrame),
				framesPerSecond,
				outputPath,
			)
		}()
	}
	wg.Wait()
}

func RecordTrackTimelapse(
	ctx context.Context,
	client *api.Client,
	trackName string,
	interval time.Duration,
	fps float64,
	outputPath string,
) {
	ticker := time.NewTicker(interval)
	var encoder *ffmpego.VideoWriter
	defer func() {
		if encoder != nil {
			log.Printf("closing video file at %s ...", outputPath)
			encoder.Close()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		imgData, err := client.Snapshot(trackName)
		essentials.Must(err)
		img, _, err := image.Decode(bytes.NewReader(imgData))
		essentials.Must(err)
		if encoder == nil {
			log.Printf("starting encoding of video file: %s", outputPath)
			size := img.Bounds()
			encoder, err = ffmpego.NewVideoWriter(outputPath, size.Dx(), size.Dy(), fps)
			essentials.Must(err)
		}
		essentials.Must(encoder.WriteFrame(img))
	}
}
