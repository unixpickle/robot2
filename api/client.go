package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"

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

func (c *Client) CalibrateCenter() error {
	if err := c.doRequest("/motor/calibratecenter", "", nil, nil); err != nil {
		return fmt.Errorf("calibrate center: %w", err)
	}
	return nil
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
