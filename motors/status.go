package motors

import "fmt"

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

	Voltage float64
}

func decodeMotorStatus(packet []byte) (*MotorStatus, error) {
	resp := &MotorStatus{}
	if err := decodeResponse(packet, &resp.ID, &resp.ErrorFlags, []any{
		&resp.Position,
		&resp.Speed,
		&resp.Load,
		&resp.RawVoltage,
		&resp.Temperature,
		&resp.AsyncFlag,
		&resp.Status,
		&resp.Moving,
	}); err != nil {
		return nil, err
	}

	// The load is signed but is only 9 bits.
	if resp.Load&(1<<9) != 0 {
		resp.Load ^= (1 << 9)
		resp.Load = -resp.Load
	}

	resp.Voltage = float64(resp.RawVoltage) / 10.0

	return resp, nil
}

func (m *MotorStatus) String() string {
	return fmt.Sprintf(
		"MotorStatus(id=%d, errFlags=%d, position=%d, speed=%d, load=%d, voltage=%.1f)",
		m.ID,
		m.ErrorFlags,
		m.Position,
		m.Speed,
		m.Load,
		m.Voltage,
	)
}
