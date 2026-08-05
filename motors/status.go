package motors

import (
	"fmt"
	"reflect"
)

type StatusFlags uint8

const (
	StatusVoltage     StatusFlags = 1 << 0
	StatusSensor      StatusFlags = 1 << 1
	StatusTemperature StatusFlags = 1 << 2
	StatusCurrent     StatusFlags = 1 << 3
	StatusOverload    StatusFlags = 1 << 5
)

type MotorStatus struct {
	ID         uint8      `json:"id"`
	ErrorFlags ErrorFlags `json:"errorFlags"`

	Position    int16  `json:"position"`
	Speed       int16  `json:"speed"`
	Load        int16  `json:"load"`
	rawVoltage  uint8  `json:"-"`
	Temperature uint8  `json:"temperature"`
	AsyncFlag   uint8  `json:"asyncFlag"`
	Status      uint8  `json:"status"`
	Moving      uint8  `json:"moving"`
	rawCurrent  uint16 `json:"-"`

	// Voltage is measured in volts.
	Voltage float64 `json:"voltage"`

	// Current is measured in mA.
	Current float64 `json:"current"`
}

func decodeMotorStatus(packet []byte) (*MotorStatus, error) {
	resp := &MotorStatus{}
	var ignore uint16
	if err := decodeResponse(packet, &resp.ID, &resp.ErrorFlags, []any{
		&resp.Position,
		&resp.Speed,
		&resp.Load,
		&resp.rawVoltage,
		&resp.Temperature,
		&resp.AsyncFlag,
		&resp.Status,
		&resp.Moving,
		&ignore,
		&resp.rawCurrent,
	}); err != nil {
		return nil, err
	}

	// The load is signed but is only 9 bits.
	if resp.Load&(1<<9) != 0 {
		resp.Load ^= (1 << 9)
		resp.Load = -resp.Load
	}

	resp.Voltage = float64(resp.rawVoltage) / 10.0
	resp.Current = float64(resp.rawCurrent) * 6.5

	return resp, nil
}

func (m *MotorStatus) String() string {
	return fmt.Sprintf(
		"MotorStatus(id=%d, errFlags=%d, position=%d, speed=%d, load=%d, voltage=%.1f, current=%f)",
		m.ID,
		m.ErrorFlags,
		m.Position,
		m.Speed,
		m.Load,
		m.Voltage,
		m.Current,
	)
}

// Equal checks if all of the fields match between m and m1.
func (m *MotorStatus) Equal(m1 *MotorStatus) bool {
	return reflect.DeepEqual(m, m1)
}

// IsOverloaded checks if the status includes the overload protection flag.
func (m *MotorStatus) IsOverloaded() bool {
	return m.Status&uint8(StatusOverload) != 0
}

func (m *MotorStatus) IsMoving() bool {
	return m.Moving != 0
}
