package motors

type TorqueRequest struct {
	Motor string `json:"motor"`
}

type SetTorqueRequest struct {
	Motor   *string `json:"motor,omitempty"`
	Enabled bool    `json:"enabled"`
}

type MoveRequest struct {
	Motor string `json:"motor"`
	Pos   uint16 `json:"pos"`
}
