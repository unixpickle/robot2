package camera

import (
	"context"
	"log"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/unixpickle/h264framer"
)

type CameraTrack struct {
	Name  string
	Track *webrtc.TrackLocalStaticSample
}

func NewCameraTrack(cam Camera, name string, ctx context.Context) (*CameraTrack, error) {
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeH264,
			ClockRate: 90000,
			// TODO: perhaps the profile-level-id should come from the h264 encoder, but for now
			// we hardcode to the following:
			// * Hex 42 is decimal 66, which identifies the H.264 Baseline profile family.
			// * I'm not quite sure what c0 is, but it's likely some bit flags for constrained profiles.
			// * Hex 28 is level 4.0.
			SDPFmtpLine: "packetization-mode=1;profile-level-id=42c028;level-asymmetry-allowed=1",
		},
		name,
		"cameras",
	)
	if err != nil {
		return nil, err
	}

	width, height := cam.Resolution()
	encoder, err := h264framer.NewH264FrameEncoder(width, height, ctx)
	if err != nil {
		return nil, err
	}

	// FIFO for frame times to match encoded output samples
	frameTimes := make(chan time.Time, 10)

	go func() {
		defer close(frameTimes)
		for packet := range cam.Frames() {
			select {
			case frameTimes <- packet.Time:
				if err := encoder.WriteFrame(packet.Image); err != nil {
					log.Printf("error writing camera frame: %s", err)
					return
				}
			default:
				// Backpressure on encoder/consumer. We would probably hit backpressure on the
				// encoder's WriteFrame itself first, but we do this just in case.
			}
		}
	}()
	go func() {
		defer encoder.Cancel()
		var prevFrame []byte
		var prevTime time.Time
		for {
			frame, err := encoder.ReadFrame()
			if err != nil {
				return
			}
			ts := <-frameTimes
			if prevFrame != nil {
				duration := ts.Sub(prevTime)
				if err := track.WriteSample(media.Sample{Data: prevFrame, Duration: duration}); err != nil {
					return
				}
			}
			prevTime = ts
			prevFrame = frame
		}
	}()

	return &CameraTrack{Name: name, Track: track}, nil
}
