package api

import (
	"github.com/unixpickle/robot2/kinematics"
)

func (c *Client) MoveAngles(angles *kinematics.MotorAngles) error {
	return c.doRequest("/kinematics/move", angles, nil)
}

func (c *Client) LimitsAngles() (min *kinematics.MotorAngles, max *kinematics.MotorAngles, err error) {
	var resp struct {
		Min *kinematics.MotorAngles `json:"min"`
		Max *kinematics.MotorAngles `json:"max"`
	}
	if err := c.doRequest("/kinematics/limitsangles", nil, &resp); err != nil {
		return nil, nil, err
	}
	return resp.Min, resp.Max, nil
}

func (c *Client) CurrentAngles() (*kinematics.MotorAngles, error) {
	var resp *kinematics.MotorAngles
	if err := c.doRequest("/kinematics/currentangles", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// HomeSafely zeros all the motor angles of the robot, attempting to ensure
// that the arm never scrapes against the ground.
func (c *Client) HomeSafely() error {
	return c.doRequest("/kinematics/safehome", nil, nil)
}
