package camera

import (
	"image"
	"time"
)

type Frame struct {
	Image image.Image
	Time  time.Time
}

type Camera interface {
	Close() error
	Resolution() (width int, height int)
	Frames() <-chan *Frame
	Error() error
}
