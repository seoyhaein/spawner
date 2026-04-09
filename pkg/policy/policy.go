package policy

import (
	"errors"
	"strings"
	"time"
)

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

type AttemptPhase uint8

const (
	AttemptPhaseInitialSubmit AttemptPhase = iota
	AttemptPhaseRecoveryReplay
	AttemptPhaseManualRequeue
	AttemptPhaseAutoRetry
)

type AttemptPolicy struct {
	RecoveryReplayNewAttempt bool
	ManualRequeueNewAttempt  bool
	AutoRetryNewAttempt      bool
}

var ErrInvalidPolicy = errors.New("invalid policy")

func DefaultAttemptPolicy() AttemptPolicy {
	return AttemptPolicy{
		RecoveryReplayNewAttempt: false,
		ManualRequeueNewAttempt:  true,
		AutoRetryNewAttempt:      true,
	}
}

func (p AttemptPolicy) UseNewAttempt(phase AttemptPhase) bool {
	switch phase {
	case AttemptPhaseInitialSubmit:
		return false
	case AttemptPhaseRecoveryReplay:
		return p.RecoveryReplayNewAttempt
	case AttemptPhaseManualRequeue:
		return p.ManualRequeueNewAttempt
	case AttemptPhaseAutoRetry:
		return p.AutoRetryNewAttempt
	default:
		return false
	}
}

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

type AdmitPolicyPatch struct {
	Priority           *int
	SlotClass          *string
	Labels             map[string]string
	Timeout            *time.Duration
	KillAfter          *time.Duration
	RetryMax           *int
	RetryBackoff       *time.Duration
	RetryKind          *BackoffKind
	RetryJitterPct     *int
	CancelModeDefault  *CancelMode
	CancelOnDisconnect *DisconnectPolicy
	AutoCancelAfter    *time.Duration
}

const (
	defaultTimeout        = 30 * time.Minute
	defaultKillAfter      = 30 * time.Second
	defaultAutoCancel     = 30 * time.Second
	defaultRetryBackoff   = 5 * time.Second
	defaultRetryJitterPct = 20
	defaultSlotClass      = "actor"
)

func (p AdmitPolicy) WithDefaults() AdmitPolicy {
	if p.SlotClass == "" {
		p.SlotClass = defaultSlotClass
	}
	if p.Timeout == 0 {
		p.Timeout = defaultTimeout
	}
	if p.KillAfter == 0 {
		p.KillAfter = defaultKillAfter
	}
	if p.AutoCancelAfter == 0 {
		p.AutoCancelAfter = defaultAutoCancel
	}
	if p.Retry.Backoff == 0 && p.Retry.Max > 0 {
		p.Retry.Backoff = defaultRetryBackoff
	}
	if p.Retry.Backoff == 0 && p.Retry.Max == 0 {
		p.Retry.Backoff = defaultRetryBackoff
	}
	if p.Retry.JitterPct == 0 {
		p.Retry.JitterPct = defaultRetryJitterPct
	}
	if p.Labels == nil {
		p.Labels = map[string]string{}
	}
	return p
}

func (p AdmitPolicy) Validate() error {
	if strings.TrimSpace(p.SlotClass) == "" {
		return ErrInvalidPolicy
	}
	if p.Timeout < 0 || p.KillAfter < 0 || p.AutoCancelAfter < 0 {
		return ErrInvalidPolicy
	}
	if p.Retry.Max < 0 || p.Retry.Backoff < 0 {
		return ErrInvalidPolicy
	}
	if p.Retry.JitterPct < 0 || p.Retry.JitterPct > 100 {
		return ErrInvalidPolicy
	}
	if p.CancelOnDisconnect == DiscGrace && p.AutoCancelAfter <= 0 {
		return ErrInvalidPolicy
	}
	return nil
}

func (p AdmitPolicy) MergePatch(patch AdmitPolicyPatch) AdmitPolicy {
	out := p
	if patch.Priority != nil {
		out.Priority = *patch.Priority
	}
	if patch.SlotClass != nil {
		out.SlotClass = *patch.SlotClass
	}
	if patch.Labels != nil {
		if out.Labels == nil {
			out.Labels = map[string]string{}
		}
		for k, v := range patch.Labels {
			out.Labels[k] = v
		}
	}
	if patch.Timeout != nil {
		out.Timeout = *patch.Timeout
	}
	if patch.KillAfter != nil {
		out.KillAfter = *patch.KillAfter
	}
	if patch.RetryMax != nil {
		out.Retry.Max = *patch.RetryMax
	}
	if patch.RetryBackoff != nil {
		out.Retry.Backoff = *patch.RetryBackoff
	}
	if patch.RetryKind != nil {
		out.Retry.Kind = *patch.RetryKind
	}
	if patch.RetryJitterPct != nil {
		out.Retry.JitterPct = *patch.RetryJitterPct
	}
	if patch.CancelModeDefault != nil {
		out.CancelModeDefault = *patch.CancelModeDefault
	}
	if patch.CancelOnDisconnect != nil {
		out.CancelOnDisconnect = *patch.CancelOnDisconnect
	}
	if patch.AutoCancelAfter != nil {
		out.AutoCancelAfter = *patch.AutoCancelAfter
	}
	return out
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
	return p.WithDefaults()
}

func DefaultPolicyB(timeout time.Duration) AdmitPolicy {
	var rp AdmitPolicy
	rp.Timeout = timeout
	rp.Retry.Max = 0
	rp.Retry.Backoff = 0
	return rp.WithDefaults()
}
