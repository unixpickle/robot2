package motors

import (
	"fmt"
	"sync"
	"time"

	"go.bug.st/serial"
)

const baudRate = 1000000

type Connection struct {
	readTimeout time.Duration
	port        serial.Port
	txLock      sync.Mutex
}

func NewConnection(path string, readTimeout time.Duration) (*Connection, error) {
	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(path, mode)
	if err != nil {
		return nil, err
	}

	// Make sure read doesn't block for too long.
	if err := port.SetReadTimeout(250 * time.Millisecond); err != nil {
		port.Close()
		return nil, err
	}

	return &Connection{readTimeout: readTimeout, port: port}, nil
}

func (c *Connection) sendAndReceive(packet []byte) ([]byte, error) {
	c.txLock.Lock()
	defer c.txLock.Unlock()
	if _, err := c.port.Write(packet); err != nil {
		return nil, err
	}
	data, err := readResponse(c.port, c.readTimeout)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Connection) MotorStatus(id uint8) (*MotorStatus, error) {
	request := encodeInstruction(id, instructionReadData, []any{uint8(56), uint8(15)})
	response, err := c.sendAndReceive(request)
	if err != nil {
		return nil, err
	}
	return decodeMotorStatus(response)
}

func (c *Connection) MotorStatuses(count int) ([]*MotorStatus, error) {
	var results []*MotorStatus
	for i := 1; i <= count; i++ {
		if status, err := c.MotorStatus(uint8(i)); err != nil {
			return nil, fmt.Errorf("get status for motor %d failed: %w", i, err)
		} else {
			results = append(results, status)
		}
	}
	return results, nil
}

// SetOverloadProtection configures the overload protection system.
// The threshold is a torque percentage from 0 to 100 (80 is standard).
// The delay is the time before protection kicks in during overload.
// The reduction is the torque percentage to apply after protection kicks in,
// where 20 is a reasonable default.
func (c *Connection) SetOverloadProtection(id, threshold uint8, delay time.Duration, reduction uint8) error {
	delayUnit := uint8(delay / (time.Millisecond * 10))
	if err := c.write(id, 34, reduction, delayUnit, threshold); err != nil {
		return fmt.Errorf("set overload protection: %w", err)
	}

	// Set the overload protection flag
	var flags uint8
	if err := c.read(id, 19, &flags); err != nil {
		return fmt.Errorf("get overload flags: %w", err)
	}
	if (flags & (1 << 5)) == 0 {
		flags |= 1 << 5
		if err := c.write(id, 19, flags); err != nil {
			return fmt.Errorf("set overload flags: %w", err)
		}
	}
	return nil
}

// TorqueEnabled enables or disables torque for a motor.
func (c *Connection) TorqueEnabled(id uint8) (bool, error) {
	var flag uint8
	if err := c.read(id, 40, &flag); err != nil {
		return false, fmt.Errorf("get torque enabled: %w", err)
	}
	return flag != 0, nil
}

// SetTorqueEnabled enables or disables torque for a motor.
func (c *Connection) SetTorqueEnabled(id uint8, enabled bool) error {
	val := uint8(0)
	if enabled {
		val = 1
	}
	if err := c.write(id, 40, val); err != nil {
		return fmt.Errorf("set torque enabled: %w", err)
	}
	return nil
}

// PositionTarget gets the current target position, e.g. one set with SetPosition().
func (c *Connection) PositionTarget(id uint8) (uint16, error) {
	var pos uint16
	if err := c.read(id, 42, &pos); err != nil {
		return 0, fmt.Errorf("get position target: %w", err)
	}
	return pos, nil
}

// SetPosition moves the motor to a position with a possible speed and
// acceleration limit.
//
// The speed is a limit on positions-per-second, with 0 defaulting to max speed.
// The acceleration is a limit on acceleration, roughly in 100 steps-per-second^2,
// with 0 defaulting to max.
func (c *Connection) SetPosition(id uint8, position, speed uint16, acceleration uint8) error {
	// Note that we always pass 0 for time to use speed-based limits instead.
	if err := c.write(id, 41, acceleration, position, uint16(0), speed); err != nil {
		return fmt.Errorf("set position: %w", err)
	}
	return nil
}

// PositionLimit gets the min/max position configured on the motor.
func (c *Connection) PositionLimit(id uint8) (min, max uint16, err error) {
	if err := c.read(id, 9, &min, &max); err != nil {
		return 0, 0, fmt.Errorf("get position limit: %w", err)
	}
	return
}

// SetPositionLimit adjusts the position limit for the motor.
func (c *Connection) SetPositionLimit(id uint8, min, max uint16) error {
	if err := c.write(id, 9, min, max); err != nil {
		return fmt.Errorf("set position limit: %w", err)
	}
	return nil
}

// CenterPosition calibrates the motor's current position as 2048.
func (c *Connection) CenterPosition(id uint8) error {
	// This is a special value of the torque enabled register that
	// does the centering calibration behavior.
	if err := c.write(id, 40, 128); err != nil {
		return fmt.Errorf("center position: %w", err)
	}
	return nil
}

func (c *Connection) read(id uint8, addr uint8, data ...any) error {
	var totalSize uint8
	for _, x := range data {
		switch x := x.(type) {
		case *uint8:
			totalSize += 1
		case *uint16, *int16:
			totalSize += 2
		default:
			panic(fmt.Sprintf("unknown type for read output: %T", x))
		}
	}
	request := encodeInstruction(id, instructionReadData, []any{addr, totalSize})
	response, err := c.sendAndReceive(request)
	if err != nil {
		return err
	}
	return decodeResponse(response, nil, nil, data)
}

func (c *Connection) write(id uint8, addr uint8, data ...any) error {
	var allData = append([]any{addr}, data...)
	request := encodeInstruction(id, instructionWriteData, allData)
	_, err := c.sendAndReceive(request)
	return err
}
