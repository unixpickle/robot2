package camera

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/unixpickle/h264framer"
)

type CameraTrackMetrics struct {
	LastFPS float64 `json:"lastFPS"`

	// Millisecond epoch time
	LastUpdateTime int64 `json:"lastUpdateTime"`

	TotalFrames uint64 `json:"totalFrames"`
}

type cameraTrackWaiter struct {
	FrameCh chan<- *Frame
	ErrCh   chan<- error
}

type CameraTrack struct {
	Name      string
	Track     *webrtc.TrackLocalStaticSample
	metrics   atomic.Value // contains a *CameraTrackMetrics
	lastError atomic.Value // contains error or nil

	waitersLock sync.Mutex
	waiters     []*cameraTrackWaiter
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

	// FIFO for raw frames to match encoded output samples
	frames := make(chan *Frame, 10)

	result := &CameraTrack{Name: name, Track: track}

	go func() {
		defer close(frames)
		for frame := range cam.Frames() {
			select {
			case frames <- frame:
				if err := encoder.WriteFrame(frame.Image); err != nil {
					result.recordError(fmt.Errorf("write camera frame: %w", err))
					log.Printf("error writing camera frame: %s", err)
					return
				}
			default:
				// Backpressure on encoder/consumer. We would probably hit backpressure on the
				// encoder's WriteFrame itself first, but we do this just in case.
			}
		}
		if err := cam.Error(); err != nil {
			result.recordError(fmt.Errorf("error streaming images from camera: %w", err))
			log.Printf("error streaming images from camera %s: %s", name, err)
		}
	}()
	go func() {
		defer encoder.Cancel()
		var prevSample []byte
		var prevFrame *Frame
		var totalFrames uint64
		for {
			sample, err := encoder.ReadFrame()
			if err != nil {
				return
			}
			frame := <-frames
			if prevFrame != nil {
				duration := frame.Time.Sub(prevFrame.Time)
				if err := track.WriteSample(media.Sample{Data: prevSample, Duration: duration}); err != nil {
					return
				}

				result.signalFrame(frame)

				totalFrames += 1
				result.metrics.Store(&CameraTrackMetrics{
					LastFPS:        1e9 / float64(duration.Nanoseconds()),
					LastUpdateTime: prevFrame.Time.UnixMilli(),
					TotalFrames:    totalFrames,
				})
			}
			prevFrame = frame
			prevSample = sample
		}
	}()

	return result, nil
}

func (c *CameraTrack) recordError(err error) {
	// Store the error before notifying the waiters to avoid a race
	// where a waiter doesn't see the error at first but also doesn't
	// get notified when the error comes in.
	c.lastError.Store(err)

	c.waitersLock.Lock()
	defer c.waitersLock.Unlock()
	for _, w := range c.waiters {
		w.ErrCh <- err
	}
	c.waiters = nil
}

func (c *CameraTrack) signalFrame(frame *Frame) {
	c.waitersLock.Lock()
	defer c.waitersLock.Unlock()
	for _, w := range c.waiters {
		w.FrameCh <- frame
	}
	c.waiters = nil
}

// Wait polls for the next frame and returns it, or an error if the
// context completes or the camera fails.
func (c *CameraTrack) Wait(ctx context.Context) (*Frame, error) {
	errCh := make(chan error, 1)
	frameCh := make(chan *Frame, 1)
	w := &cameraTrackWaiter{FrameCh: frameCh, ErrCh: errCh}
	c.waitersLock.Lock()
	// Check for error after acquiring lock to match the order
	// in recordError.
	if err := c.LastError(); err != nil {
		c.waitersLock.Unlock()
		return nil, err
	}
	c.waiters = append(c.waiters, w)
	c.waitersLock.Unlock()

	select {
	case <-ctx.Done():
		c.waitersLock.Lock()
		defer c.waitersLock.Unlock()
		for i, x := range c.waiters {
			if x == w {
				c.waiters[i] = c.waiters[len(c.waiters)-1]
				c.waiters = c.waiters[:len(c.waiters)-1]
				break
			}
		}
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case frame := <-frameCh:
		return frame, nil
	}
}

func (c *CameraTrack) Metrics() *CameraTrackMetrics {
	return c.metrics.Load().(*CameraTrackMetrics)
}

func (c *CameraTrack) LastError() error {
	obj, _ := c.lastError.Load().(error)
	return obj
}
