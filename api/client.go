package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/unixpickle/robot2/motors"
)

type RemoteError struct {
	Message string
}

func (r *RemoteError) Error() string {
	return r.Message
}

func AddClientFlags() func() (*Client, error) {
	var baseURL string
	flag.StringVar(&baseURL, "base-url", "http://localhost:1337", "base URL of the robot server")
	return func() (*Client, error) {
		if u, err := url.Parse(baseURL); err != nil {
			return nil, fmt.Errorf("invalid -base-url flag: %w", err)
		} else {
			return NewClient(u), nil
		}
	}
}

type Client struct {
	baseURL url.URL
}

func NewClient(baseURL *url.URL) *Client {
	return &Client{baseURL: *baseURL}
}

func (c *Client) MotorNames() []string {
	return []string{"shoulder_pan", "shoulder_lift", "elbow_flex", "wrist_flex", "wrist_roll", "gripper"}
}

func (c *Client) MotorStatuses() (map[string]*motors.AnnotatedStatus, error) {
	var result map[string]*motors.AnnotatedStatus
	if err := c.doRequest("/motor/status", "", nil, &result); err != nil {
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
	if err := c.doRequest("/motor/calibratecenter", "", nil, nil); err != nil {
		return fmt.Errorf("calibrate center: %w", err)
	}
	return nil
}

func (c *Client) Limits() (map[string]motors.MotorLimit, error) {
	var result map[string]motors.MotorLimit
	if err := c.doRequest("/motor/limits", "", nil, &result); err != nil {
		return nil, fmt.Errorf("get limits: %w", err)
	}
	return result, nil
}

func (c *Client) SetLimits(limits map[string]motors.MotorLimit) error {
	if err := c.doRequest("/motor/setlimits", "", limits, nil); err != nil {
		return fmt.Errorf("set limits: %w", err)
	}
	return nil
}

func (c *Client) SetTorqueEnabled(enabled bool) error {
	flag := "0"
	if enabled {
		flag = "1"
	}
	if err := c.doRequest("/motor/torque", "enabled="+flag, nil, nil); err != nil {
		return fmt.Errorf("set torque enabled: %w", err)
	}
	return nil
}

func (c *Client) Move(motor string, pos int16) error {
	q := fmt.Sprintf("motor=%s&pos=%d", motor, pos)
	if err := c.doRequest("/motor/move", q, nil, nil); err != nil {
		return fmt.Errorf("set torque enabled: %w", err)
	}
	return nil
}

func (c *Client) MoveAngles(angles *motors.MotorAngles) error {
	for i, name := range c.MotorNames() {
		angle := angles.Vec()[i]
		pos := motors.AngleToPosition(angle)
		if err := c.Move(name, pos); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) LimitsAngles() (min *motors.MotorAngles, max *motors.MotorAngles, err error) {
	if rawLimits, err := c.Limits(); err != nil {
		return nil, nil, err
	} else {
		var minVec, maxVec [6]float64
		for i, name := range c.MotorNames() {
			lim := rawLimits[name]
			minVec[i] = motors.PositionToAngle(int16(lim.Min))
			maxVec[i] = motors.PositionToAngle(int16(lim.Max))
		}
		min, max = motors.NewMotorAngles(minVec), motors.NewMotorAngles(maxVec)
		return min, max, nil
	}
}

func (c *Client) doRequest(path, query string, objIn, objOut any) error {
	u := c.baseURL
	u.Path = path
	u.RawQuery = query

	var body io.Reader
	var method string
	if objIn == nil {
		body = nil
		method = "GET"
	} else {
		data, err := json.Marshal(objIn)
		if err != nil {
			return err
		}
		method = "POST"
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	var fullOut struct {
		Error *string `json:"error"`
		Data  any     `json:"data"`
	}
	if objOut != nil {
		fullOut.Data = objOut
	}
	if err := json.NewDecoder(resp.Body).Decode(&fullOut); err != nil {
		return err
	}
	if fullOut.Error != nil {
		return &RemoteError{Message: *fullOut.Error}
	}
	return nil
}
