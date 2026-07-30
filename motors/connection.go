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
	request := encodeInstruction(id, instructionReadData, []any{uint8(56), uint8(11)})
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
