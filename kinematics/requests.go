package kinematics

import "github.com/unixpickle/model3d/model3d"

type ReverseHoverRequest struct {
	CenterPos    model3d.Coord3D `json:"center_pos"`
	GripperAngle float64         `json:"gripper_angle"`
}
