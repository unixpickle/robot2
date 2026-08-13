package api

import (
	"github.com/unixpickle/essentials"
	"github.com/unixpickle/robot2/kinematics"
)

func (c *Client) MoveAngles(angles *kinematics.MotorAngles) error {
	names, err := c.MotorNames()
	if err != nil {
		return err
	}
	for i, name := range names {
		angle := angles.Vec()[i]
		pos := kinematics.AngleToPosition(angle)
		if err := c.Move(name, pos); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) LimitsAngles() (min *kinematics.MotorAngles, max *kinematics.MotorAngles, err error) {
	names, err := c.MotorNames()
	if err != nil {
		return nil, nil, err
	}
	rawLimits, err := c.Limits()
	if err != nil {
		return nil, nil, err
	}
	var minVec, maxVec [6]float64
	for i, name := range names {
		lim := rawLimits[name]
		minVec[i] = kinematics.PositionToAngle(int16(lim.Min))
		maxVec[i] = kinematics.PositionToAngle(int16(lim.Max))
	}
	min, max = kinematics.NewMotorAngles(minVec), kinematics.NewMotorAngles(maxVec)
	return min, max, nil
}

func (c *Client) CurrentAngles() (*kinematics.MotorAngles, error) {
	statuses, err := c.MotorStatuses()
	if err != nil {
		return nil, err
	}
	names, err := c.MotorNames()
	if err != nil {
		return nil, err
	}
	var angles [6]float64
	for i, name := range names {
		angles[i] = kinematics.PositionToAngle(statuses[name].Position)
	}
	return kinematics.NewMotorAngles(angles), nil
}

// HomeSafely zeros all the motor angles of the robot, attempting to ensure
// that the arm never scrapes against the ground.
func (c *Client) HomeSafely() (err error) {
	defer essentials.AddCtxTo("home safely", &err)
	min, max, err := c.LimitsAngles()
	if err != nil {
		return err
	}
	startPos, err := c.CurrentAngles()
	if err != nil {
		return err
	}
	trajectory := kinematics.PlanSafeHome(min, max, startPos)
	for _, angles := range trajectory {
		if err := c.MoveAngles(angles); err != nil {
			return err
		}
		if err := c.WaitUntilStill(); err != nil {
			return err
		}
	}
	return nil
}
