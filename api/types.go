package api

import "time"

type CmdKind int

const (
	CmdRun CmdKind = iota
	CmdCancel
	CmdSignal
	CmdQuery
)

type AdmitPolicy struct {
	Priority int
	Timeout  time.Duration
	Retry    struct {
		Max     int
		Backoff time.Duration
	}
	AutoCancelAfter time.Duration // for advisory cancel upgrade
}

type RunSpec struct {
	RunID     string
	ImageRef  string // digest-locked preferred
	Env       map[string]string
	Mounts    []Mount
	Resources Resources
}

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

type Resources struct {
	CPU    string
	Memory string
}

type Signal struct{ Name string }

// === Events (server → client) ===

type State string

const (
	StateQueued     State = "queued"
	StateRunning    State = "running"
	StateCancelling State = "cancelling"
	StateCancelled  State = "cancelled"
	StateSucceeded  State = "succeeded"
	StateFailed     State = "failed"
)

type Event struct {
	RunID    string
	SpawnKey string
	When     time.Time
	State    State
	Message  string
	Details  map[string]string
}

// EventSink is used by actor to push events (streaming RPC, bus, etc.)
// Implemented in dispatcher layer for your gRPC server.
// Keep it simple here to decouple from transport.
type EventSink interface {
	Send(Event)
}

type Command struct {
	Kind   CmdKind
	Run    *RunSpec
	Signal *Signal
	Policy AdmitPolicy
	Sink   EventSink // filled by dispatcher
}
