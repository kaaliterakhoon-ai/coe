package app

type runtimeCommandType string

const (
	runtimeCommandToggle runtimeCommandType = "toggle"
	runtimeCommandStart  runtimeCommandType = "start"
	runtimeCommandStop   runtimeCommandType = "stop"
)

type runtimeCommand struct {
	Type   runtimeCommandType
	Source string
	Reply  chan runtimeCommandResponse
}

type runtimeCommandResponse struct {
	Active  bool
	Changed bool
	Err     error
}
