package camera

import (
	"bytes"
	"errors"
	"image/jpeg"
	"sync"
	"time"

	"github.com/blackjack/webcam"
)

const pixelFormatMJPG webcam.PixelFormat = 1196444237

const (
	targetWidth             = 640
	targetFPS               = 15
	maxSuccessiveJPEGErrors = 10
)

type V4L2Camera struct {
	width  int
	height int
	ch     <-chan *Frame
	cancel chan struct{}

	errLock sync.RWMutex
	err     error
}

func NewV4L2Camera(path string) (*V4L2Camera, error) {
	wc, err := webcam.Open(path)
	if err != nil {
		return nil, err
	}
	formats := wc.GetSupportedFormats()
	if _, ok := formats[pixelFormatMJPG]; !ok {
		return nil, errors.New("only MJPG is supported, but it is not available for camera: " + path)
	}
	format := pixelFormatMJPG
	sizes := wc.GetSupportedFrameSizes(format)

	var chosenWidth, chosenHeight uint32
	for _, fs := range sizes {
		// For now, only support constant frame sizes
		if fs.StepWidth == 0 && fs.MinWidth == fs.MaxWidth && fs.MinHeight == fs.MaxHeight {
			if fs.MaxWidth <= targetWidth {
				if fs.MaxWidth > chosenWidth || (fs.MaxWidth == chosenWidth && fs.MaxHeight > chosenHeight) {
					chosenWidth = fs.MaxWidth
					chosenHeight = fs.MaxHeight
				}
			}
		}
	}
	if chosenWidth == 0 {
		return nil, errors.New("no supported frame size was found for camera: " + path)
	}

	// Find framerate closest to 30fps
	framerates := wc.GetSupportedFramerates(format, chosenWidth, chosenHeight)
	var chosenFPS float32
	for _, fr := range framerates {
		if fr.MinNumerator != fr.MaxNumerator || fr.MinDenominator != fr.MaxDenominator {
			// Only support constant framerates for now
			continue
		}
		fps := float32(fr.MaxDenominator) / float32(fr.MaxNumerator)
		if chosenFPS == 0 || abs(fps-targetFPS) < abs(chosenFPS-targetFPS) {
			chosenFPS = fps
		}
	}
	if chosenFPS == 0 {
		return nil, errors.New("no supported frame size was found for camera: " + path)
	}

	_, _, _, err = wc.SetImageFormat(format, chosenWidth, chosenHeight)
	if err != nil {
		return nil, err
	}
	err = wc.SetFramerate(chosenFPS)
	if err != nil {
		return nil, err
	}

	// Reduce buffer memory consumption and allow quicker backpressure
	// from the encoder.
	wc.SetBufferCount(4)

	ch := make(chan *Frame, 1)
	cancel := make(chan struct{}, 1)
	v := &V4L2Camera{
		width:  int(chosenWidth),
		height: int(chosenHeight),
		ch:     ch,
		cancel: cancel,
	}
	go func() {
		defer close(ch)
		err = wc.StartStreaming()
		if err != nil {
			v.setError(err)
			return
		}
		defer wc.StopStreaming()
		successiveErrors := 0
		for {
			select {
			case <-cancel:
				return
			default:
			}
			err = wc.WaitForFrame(1)
			if err != nil {
				if _, ok := err.(*webcam.Timeout); !ok {
					v.setError(err)
					return
				}
				continue
			}
			frame, idx, err := wc.GetFrame()
			t := time.Now()
			if err != nil {
				v.setError(err)
				return
			}
			if len(frame) == 0 {
				continue
			}
			img, err := jpeg.Decode(bytes.NewReader(frame))
			if err != nil {
				successiveErrors += 1
				if successiveErrors == maxSuccessiveJPEGErrors {
					v.setError(err)
					return
				}
				continue
			}
			successiveErrors = 0
			wc.ReleaseFrame(idx)
			select {
			case ch <- &Frame{Image: img, Time: t}:
			case <-cancel:
				return
			}
		}
	}()

	return v, nil
}

func (v *V4L2Camera) Resolution() (int, int) {
	return v.width, v.height
}

func (v *V4L2Camera) Close() error {
	select {
	case v.cancel <- struct{}{}:
	default:
	}
	return nil
}

func (v *V4L2Camera) Frames() <-chan *Frame {
	return v.ch
}

func (v *V4L2Camera) Error() error {
	v.errLock.RLock()
	defer v.errLock.RUnlock()
	return v.err
}

func (v *V4L2Camera) setError(err error) {
	v.errLock.Lock()
	v.err = err
	v.errLock.Unlock()
}

func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
