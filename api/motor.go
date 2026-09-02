package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/unixpickle/robot2/motors"
)

func (c *Client) MotorNames() ([]string, error) {
	var result []string
	if err := c.doRequest("/motor/names", nil, &result); err != nil {
		return nil, fmt.Errorf("get motor names: %w", err)
	}
	return result, nil
}

func (c *Client) MotorStatuses() (map[string]*motors.AnnotatedStatus, error) {
	var result map[string]*motors.AnnotatedStatus
	if err := c.doRequest("/motor/status", nil, &result); err != nil {
		return nil, fmt.Errorf("get motor statuses: %w", err)
	}
	return result, nil
}

// StreamStatuses creates a channel of continuous motor readings.
//
// If the consumer does not read fast enough, intermediate readings are
// dropped in favor of the latest reading.
func (c *Client) StreamStatuses(ctx context.Context) (
	<-chan map[string]*motors.AnnotatedStatus,
	<-chan error,
) {
	resultCh := make(chan map[string]*motors.AnnotatedStatus, 1)
	errCh := make(chan error, 1)

	go func() {
		defer close(resultCh)
		defer close(errCh)

		u := c.baseURL
		u.Path = "/motor/stream"
		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			errCh <- err
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)
		for {
			// This is out of an abundance of caution, but hopefully the context close
			// would end the response body stream anyway.
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
			}

			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- fmt.Errorf("failed to read data from event stream: %w", err)
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				errCh <- errors.New("ill-formatted event stream line")
				return
			}
			line = line[len("data: "):]
			var response map[string]*motors.AnnotatedStatus
			if err := json.Unmarshal([]byte(line), &response); err != nil {
				errCh <- fmt.Errorf("failed to parse JSON from event stream: %w", err)
				return
			}
			select {
			case resultCh <- response:
			default:
				// Drop intermediate records on backpressure
				select {
				case <-resultCh:
				default:
				}
				resultCh <- response
			}
		}
	}()

	return resultCh, errCh
}

func (c *Client) CalibrateCenter() error {
	if err := c.doRequest("/motor/calibratecenter", nil, nil); err != nil {
		return fmt.Errorf("calibrate center: %w", err)
	}
	return nil
}

func (c *Client) Limits() (map[string]motors.MotorLimit, error) {
	var result map[string]motors.MotorLimit
	if err := c.doRequest("/motor/limits", nil, &result); err != nil {
		return nil, fmt.Errorf("get limits: %w", err)
	}
	return result, nil
}

func (c *Client) SetLimits(limits map[string]motors.MotorLimit) error {
	if err := c.doRequest("/motor/setlimits", limits, nil); err != nil {
		return fmt.Errorf("set limits: %w", err)
	}
	return nil
}

func (c *Client) SetTorqueEnabledAll(enabled bool) error {
	body := motors.SetTorqueRequest{Enabled: enabled}
	if err := c.doRequest("/motor/settorque", body, nil); err != nil {
		return fmt.Errorf("set torque enabled: %w", err)
	}
	return nil
}

func (c *Client) SetTorqueEnabled(motor string, enabled bool) error {
	body := motors.SetTorqueRequest{Motor: &motor, Enabled: enabled}
	if err := c.doRequest("/motor/settorque", body, nil); err != nil {
		return fmt.Errorf("set torque enabled: %w", err)
	}
	return nil
}

func (c *Client) TorqueLimit(motor string) (float64, error) {
	var result float64
	body := motors.TorqueLimitRequest{Motor: motor}
	if err := c.doRequest("/motor/limits", body, &result); err != nil {
		return 0, fmt.Errorf("get torque limit: %w", err)
	}
	return result, nil
}

func (c *Client) SetTorqueLimit(motor string, limit float64) error {
	body := motors.SetTorqueLimitRequest{Motor: motor, Limit: limit}
	if err := c.doRequest("/motor/settorquelimit", body, nil); err != nil {
		return fmt.Errorf("set torque limit: %w", err)
	}
	return nil
}

func (c *Client) Move(motor string, pos uint16) error {
	body := motors.MoveRequest{Motor: motor, Pos: pos}
	if err := c.doRequest("/motor/move", body, nil); err != nil {
		return fmt.Errorf("set torque enabled: %w", err)
	}
	return nil
}

func (c *Client) WaitUntilStill() error {
	time.Sleep(time.Second)
	for {
		statuses, err := c.MotorStatuses()
		if err != nil {
			return err
		}
		allDone := true
		for _, s := range statuses {
			if s.IsMoving() {
				allDone = false
			}
		}
		if allDone {
			return nil
		}
		time.Sleep(time.Second)
	}
}
