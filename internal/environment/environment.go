package environment

import "fmt"

// Status represents the lifecycle state of a stream environment.
type Status int

const (
	StatusProvisioning Status = iota
	StatusReady
	StatusFailed
	StatusDown
)

var statusNames = [...]string{
	"Provisioning",
	"Ready",
	"Failed",
	"Down",
}

func (s Status) String() string {
	if int(s) < len(statusNames) {
		return statusNames[s]
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// Environment tracks the state of a containerized app server for a stream.
type Environment struct {
	ProjectName string
	HostPort    int
	Status      Status
	Error       string // non-empty when Status == StatusFailed
}
