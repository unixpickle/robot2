package motors

import "fmt"

type StatusFlags uint8

const (
	StatusVoltage     StatusFlags = 1 << 0
	StatusSensor      StatusFlags = 1 << 1
	StatusTemperature StatusFlags = 1 << 2
	StatusCurrent     StatusFlags = 1 << 3
	StatusOverload    StatusFlags = 1 << 5
)

type MotorStatus struct {
	ID         uint8
	ErrorFlags ErrorFlags

	Position    int16
	Speed       int16
	Load        int16
	RawVoltage  uint8
	Temperature uint8
	AsyncFlag   uint8
	Status      uint8
	Moving      uint8
	RawCurrent  uint16

	// Voltage is measured in volts.
	Voltage float64

	// Current is measured in mA.
	Current float64
}

func decodeMotorStatus(packet []byte) (*MotorStatus, error) {
	resp := &MotorStatus{}
	var ignore uint8
	if err := decodeResponse(packet, &resp.ID, &resp.ErrorFlags, []any{
		&resp.Position,
		&resp.Speed,
		&resp.Load,
		&resp.RawVoltage,
		&resp.Temperature,
		&resp.AsyncFlag,
		&resp.Status,
		&resp.Moving,
		&ignore,
		&resp.RawCurrent,
	}); err != nil {
		return nil, err
	}

	// The load is signed but is only 9 bits.
	if resp.Load&(1<<9) != 0 {
		resp.Load ^= (1 << 9)
		resp.Load = -resp.Load
	}

	resp.Voltage = float64(resp.RawVoltage) / 10.0
	resp.Current = float64(resp.RawCurrent) * 6.5

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

// IsOverloaded checks if the status includes the overload protection flag.
func (m *MotorStatus) IsOverloaded() bool {
	return m.Status&uint8(StatusOverload) != 0
}
