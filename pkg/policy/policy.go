package policy

import "time"

// 추후 생각해봐야 할 문제들이 있음.
// 연결 끊김 취소 신호를 판단하기 위한 정책 및 기타 정잭들.
// Principal 예약어 사용??
// TODO actor = slot 생각해야함.

type DisconnectPolicy uint8

const (
	DiscNever     DisconnectPolicy = iota
	DiscAdvisory                   // 그냥 권고 플래그만
	DiscGrace                      // AutoCancelAfter 후 Soft cancel
	DiscImmediate                  // 즉시 Hard cancel
)

type CancelMode uint8

const (
	CancelSoft CancelMode = iota // 협조적 중단
	CancelHard                   // TERM→(KillAfter)→KILL
)

type BackoffKind uint8

const (
	BackoffFixed BackoffKind = iota
	BackoffExponential
)

type AdmitPolicy struct {
	// Queueing / Scheduling
	Priority  int    // 높을수록 먼저
	SlotClass string // "actor"|"run" 등 (선택) — slot 회계 정책
	Labels    map[string]string

	// Runtime limits
	Timeout   time.Duration // 전체 런 타임아웃
	KillAfter time.Duration // Hard cancel 시 SIGTERM→SIGKILL 대기 (기본 30s)

	// Retry
	Retry struct {
		Max       int
		Backoff   time.Duration // base
		Kind      BackoffKind   // Fixed|Exponential
		JitterPct int           // 0~100
	}

	// Cancellation policy
	CancelModeDefault  CancelMode       // 명시적 cancel 기본 모드(soft/hard)
	CancelOnDisconnect DisconnectPolicy // 전송 레벨 취소 처리
	AutoCancelAfter    time.Duration    // DiscGrace에 사용(=grace)
}

func DefaultPolicyA(d time.Duration) AdmitPolicy {
	p := AdmitPolicy{
		Priority:           0,
		SlotClass:          "actor",
		Timeout:            d,
		KillAfter:          30 * time.Second,
		CancelModeDefault:  CancelSoft,
		CancelOnDisconnect: DiscAdvisory,
		AutoCancelAfter:    30 * time.Second,
	}
	p.Retry.Max = 0
	p.Retry.Backoff = 5 * time.Second
	p.Retry.Kind = BackoffFixed
	p.Retry.JitterPct = 20
	return p
}

func DefaultPolicyB(timeout time.Duration) AdmitPolicy {
	var rp AdmitPolicy
	rp.Timeout = timeout
	rp.Retry.Max = 0
	rp.Retry.Backoff = 0
	return rp
}
