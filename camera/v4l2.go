package camera

import (
	"bytes"
	"errors"
	"image/jpeg"
	"math"
	"sync"
	"time"

	"github.com/blackjack/webcam"
)

const pixelFormatMJPG webcam.PixelFormat = 1196444237

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
	var largest webcam.FrameSize
	for _, fs := range sizes {
		if fs.StepWidth == 0 && fs.MinWidth == fs.MaxWidth && fs.MaxWidth > largest.MaxWidth {
			largest = fs
		}
	}
	if largest.MaxWidth == 0 {
		return nil, errors.New("no supported frame size was found for camera: " + path)
	}

	// Find framerate closest to 30fps
	framerates := wc.GetSupportedFramerates(format, largest.MinWidth, largest.MinHeight)
	framerate := framerates[0]
	for _, fr := range framerates {
		rate := float64(fr.MaxNumerator) / float64(fr.MaxDenominator)
		prevRate := float64(framerate.MaxNumerator) / float64(framerate.MaxDenominator)
		if math.Abs(rate-1.0/30) < math.Abs(prevRate-1.0/30) {
			framerate = fr
		}
	}

	_, _, _, err = wc.SetImageFormat(format, largest.MaxWidth, largest.MaxHeight)
	if err != nil {
		return nil, err
	}
	err = wc.SetFramerate(float32(framerate.MaxDenominator) / float32(framerate.MaxNumerator))
	if err != nil {
		return nil, err
	}

	ch := make(chan *Frame, 1)
	cancel := make(chan struct{}, 1)
	v := &V4L2Camera{
		width:  int(largest.MaxWidth),
		height: int(largest.MaxHeight),
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
			wc.ReleaseFrame(idx)
			if err != nil {
				v.setError(err)
				return
			}
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
