package api

import (
	"time"

	ply "github.com/seoyhaein/spawner/pkg/policy"
)

type CmdKind int

const (
	CmdRun CmdKind = iota
	CmdCancel
	CmdSignal
	CmdQuery
)

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
	StateIdle       State = "idle" // 아무 것도 안 도는 상태
	StateQueued     State = "queued"
	StateStarting   State = "starting" // prepare/start 호출 사이 -> driver 참고.
	StateRunning    State = "running"
	StateCancelling State = "cancelling"
	StateCancelled  State = "cancelled"
	StateSucceeded  State = "succeeded"
	StateFailed     State = "failed"
)

// === Cancel ===

type CancelMode uint8

const (
	CancelSoft CancelMode = iota // 협조적: drv.Cancel + runCtx cancel
	CancelHard                   // 강제: TERM→(grace)→KILL (grace는 Policy로)
)

type CancelReq struct {
	SpawnID     string        // 라우터/디스패처에서 채워줄 수도 있음
	RunID       string        // (옵션) 현재 실행 중인 run과 매칭 확인용
	IdemKey     string        // (옵션) idemKey→spawnID 매핑용
	Mode        CancelMode    // Soft|Hard
	Grace       time.Duration // (옵션) Hard일 때 TERM→KILL까지 대기; 0이면 Policy.KillAfter 사용
	Reason      string        // 예: "user-cancel" | "transport"
	RequestedBy string        // principal(감사로그용)
}

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
	Cancel *CancelReq // 추가: 취소 명령 페이로드
	Signal *Signal
	Policy ply.AdmitPolicy // 라우터/디스패처가 확정한 정책 스냅샷
	Sink   EventSink
}
