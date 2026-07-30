package motors

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// Structures are based on the FeeTech Serial Bus Smart Control servoCommunication Protocol Manual
// https://www.mantech.co.za/datasheets/products/Feetech%20Smart%20Servo%20Communication%20Protocol.pdf
// and also the scservo headers here: https://github.com/adityakamath/SCServo_Linux/blob/main/include/scservo/SMS_STS.h

var byteOrder = binary.LittleEndian

type instructionType uint8

const (
	instructionPing          instructionType = 1
	instructionReadData      instructionType = 2
	instructionWriteData     instructionType = 3
	instructionRegWriteData  instructionType = 4
	instructionAction        instructionType = 5
	instructionSyncWriteData instructionType = 6
	instructionReset         instructionType = 7
)

type ErrorFlags uint8

const (
	ErrorFlagInputVoltage ErrorFlags = 1 << 0
	ErrorFlagAngleLimit   ErrorFlags = 1 << 1
	ErrorFlagOverheat     ErrorFlags = 1 << 2
	ErrorFlagRange        ErrorFlags = 1 << 3
	ErrorFlagChecksum     ErrorFlags = 1 << 4
	ErrorFlagOverload     ErrorFlags = 1 << 5
	ErrorFlagInstruction  ErrorFlags = 1 << 6
)

var (
	// ErrBadChecksum is returned when a decoded packet has an incorrect checksum.
	ErrBadChecksum = errors.New("bad checksum")

	// ErrReadTimeout is returned when the reply on the serial bus is not returned
	// in a timely manner.
	ErrReadTimeout = errors.New("serial read timed out")
)

// encodeInstruction encodes a request packet.
// The params are a list of uint8, uint16, or int16, and will be encoded based
// on their type.
func encodeInstruction(id uint8, inst instructionType, params []any) []byte {
	var payload []byte
	for i, param := range params {
		switch param := param.(type) {
		case uint8:
			payload = append(payload, param)
		case uint16:
			payload = byteOrder.AppendUint16(payload, param)
		case int16:
			// Sign-bit encoding
			magnitude := uint16(param)
			sign := uint16(0)
			if param < 0 {
				magnitude = uint16(-param)
				sign = 1
			}
			value := magnitude | (sign << 15)
			payload = byteOrder.AppendUint16(payload, value)
		default:
			panic(fmt.Sprintf("parameter %d has invalid type: %T", i, param))
		}
	}
	if len(payload)+2 > 255 {
		panic("packet is too large")
	}
	result := append([]byte{0xff, 0xff, id, uint8(len(payload) + 2), uint8(inst)}, payload...)
	result = append(result, checksum(result[2:]))
	return result
}

// decodeResponse decodes the data in a response packet.
// The params are of type *uint8, *uint16, and *int16.
//
// The id and errFlags may be nil if they are not needed by the caller.
func decodeResponse(data []byte, id *uint8, errFlags *ErrorFlags, params []any) error {
	if data[0] != 0xff || data[1] != 0xff {
		return errors.New("packet is missing 0xffff header")
	}

	if checksum(data[2:len(data)-1]) != data[len(data)-1] {
		return ErrBadChecksum
	}

	if id != nil {
		*id = data[2]
	}
	if errFlags != nil {
		*errFlags = ErrorFlags(data[4])
	}

	buffer := data[5 : len(data)-1]
	for i, param := range params {
		switch param := param.(type) {
		case *uint8:
			if len(buffer) == 0 {
				return fmt.Errorf("underflow at parameter %d of type %T", i, param)
			}
			*param = buffer[0]
			buffer = buffer[1:]
		case *uint16:
			if len(buffer) < 2 {
				return fmt.Errorf("underflow at parameter %d of type %T", i, param)
			}
			*param = byteOrder.Uint16(buffer)
			buffer = buffer[2:]
		case *int16:
			if len(buffer) < 2 {
				return fmt.Errorf("underflow at parameter %d of type %T", i, param)
			}
			raw := byteOrder.Uint16(buffer)
			buffer = buffer[2:]
			magnitude := int16(raw & 0x7fff)
			if raw&(1<<15) != 0 {
				*param = -magnitude
			} else {
				*param = magnitude
			}
		default:
			panic(fmt.Sprintf("parameter %d has invalid type: %T", i, param))
		}
	}
	return nil
}

// readResponse reads a raw response packet but does not attempt to decode it.
func readResponse(r io.Reader, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	// Wait for start of packet.
	numFFs := 0
	for numFFs < 2 {
		data := make([]byte, 1)
		if _, err := readWithDeadline(r, data, deadline); err != nil {
			return nil, err
		}
		if data[0] == 0xff {
			numFFs++
		} else {
			numFFs = 0
		}
	}

	header := make([]byte, 2)
	if _, err := readWithDeadline(r, header, deadline); err != nil {
		return nil, err
	}
	remaining := make([]byte, header[1])
	if _, err := readWithDeadline(r, remaining, deadline); err != nil {
		return nil, err
	}
	return append([]byte{0xff, 0xff}, append(header, remaining...)...), nil
}

func readWithDeadline(r io.Reader, data []byte, deadline time.Time) (n int, err error) {
	for n < len(data) {
		if x, err := r.Read(data[n:]); err != nil {
			return n, err
		} else {
			n += x
		}
		if time.Since(deadline) > 0 {
			return n, ErrReadTimeout
		}
	}
	return
}

func checksum(packet []byte) uint8 {
	result := uint8(0)
	for _, x := range packet {
		result += x
	}
	return ^result
}
