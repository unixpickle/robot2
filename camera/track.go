package camera

import (
	"context"
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
			// * I'm not quite sure what e0 is, but it's likely some bit flags
			// * Hex 1f is decimal 31, meaning H.264 level 3.1.
			SDPFmtpLine: "packetization-mode=1;profile-level-id=42e01f;level-asymmetry-allowed=1",
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
		for {
			frame, err := encoder.ReadFrame()
			if err != nil {
				return
			}
			ts := <-frameTimes
			if err := track.WriteSample(media.Sample{Data: frame, Timestamp: ts}); err != nil {
				return
			}
		}
	}()

	return &CameraTrack{Name: name, Track: track}, nil
}
