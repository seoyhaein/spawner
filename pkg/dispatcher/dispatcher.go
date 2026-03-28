package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/seoyhaein/spawner/pkg/api"
	sErr "github.com/seoyhaein/spawner/pkg/error"
	fac "github.com/seoyhaein/spawner/pkg/factory"
	"github.com/seoyhaein/spawner/pkg/frontdoor"
	"github.com/seoyhaein/spawner/pkg/store"
)

type Dispatcher struct {
	FD          frontdoor.FrontDoor
	AF          fac.Factory
	Sem         chan struct{} // slot=actor, 바인딩 기간 동안만 점유
	defaultSink api.EventSink // 지정 안 하면 NoopSink 사용
	// 추가
	loopBaseCtx    context.Context // (옵션) 액터 루프 베이스 컨텍스트
	enqueueTimeout time.Duration   // (옵션) EnqueueCtx 타임아웃
	// ingress boundary
	runStore     store.RunStore // nil = skip RunStore (backward-compat)
	k8sAvailable bool           // false = K8s unreachable; runs held, not dispatched
}

// New is deprecated. Use NewDispatcher with options instead.
func New(fd frontdoor.FrontDoor, af fac.Factory, maxActors int) *Dispatcher {
	return &Dispatcher{
		FD:  fd,
		AF:  af,
		Sem: make(chan struct{}, maxActors),
	}
}

func NewDispatcher(fd frontdoor.FrontDoor, af fac.Factory, semSize int, opts ...Option) *Dispatcher {
	d := &Dispatcher{
		FD:           fd,
		AF:           af,
		Sem:          make(chan struct{}, semSize),
		k8sAvailable: true, // assume available unless WithK8sUnavailable is set
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

type Option func(*Dispatcher)

func WithDefaultSink(s api.EventSink) Option {
	return func(d *Dispatcher) { d.defaultSink = s }
}

// 추가

func WithLoopBaseCtx(base context.Context) Option {
	return func(d *Dispatcher) { d.loopBaseCtx = base }
}
func WithEnqueueTimeout(dur time.Duration) Option {
	return func(d *Dispatcher) { d.enqueueTimeout = dur }
}

// WithRunStore attaches a RunStore to the Dispatcher.
// When set, Handle() enqueues the run as StateQueued before dispatching
// and transitions to StateAdmittedToDag on success.
// Bootstrap() uses the store to recover runs across restarts.
func WithRunStore(s store.RunStore) Option {
	return func(d *Dispatcher) { d.runStore = s }
}

// WithK8sUnavailable marks K8s as unreachable at startup.
// Handle() will transition queued runs to StateHeld instead of dispatching
// to the Actor, preventing K8s API calls to an unavailable cluster.
// ASSUMPTION: a health-check loop (not implemented here) calls SetK8sAvailable
// once connectivity is restored.
func WithK8sUnavailable() Option {
	return func(d *Dispatcher) { d.k8sAvailable = false }
}

// SetK8sAvailable toggles K8s availability at runtime.
// When availability transitions false→true, call Bootstrap() to re-queue
// held runs.
func (d *Dispatcher) SetK8sAvailable(available bool) {
	d.k8sAvailable = available
}

// Bootstrap scans the RunStore for runs in StateQueued and StateAdmittedToDag
// and returns them for the caller to re-dispatch.
//
// "queued" runs were never admitted (e.g., restart before dispatch).
// "admitted-to-dag" runs were admitted but the process died mid-execution.
//
// ASSUMPTION: actual re-dispatch requires the original RunSpec serialized in
// RunRecord.Payload. Callers should unmarshal Payload and call Handle() again.
// This implementation logs and returns the records; re-dispatch is the caller's
// responsibility.
func (d *Dispatcher) Bootstrap(ctx context.Context) ([]store.RunRecord, error) {
	if d.runStore == nil {
		return nil, nil
	}
	queued, err := d.runStore.ListByState(ctx, store.StateQueued)
	if err != nil {
		return nil, err
	}
	admitted, err := d.runStore.ListByState(ctx, store.StateAdmittedToDag)
	if err != nil {
		return nil, err
	}
	all := append(queued, admitted...)
	for _, r := range all {
		log.Printf("[bootstrap] recovered run: id=%s state=%s created=%s",
			r.RunID, r.State, r.CreatedAt.Format(time.RFC3339))
	}
	if len(all) == 0 {
		log.Printf("[bootstrap] no pending runs to recover")
	}
	return all, nil
}

// runIDFromInput extracts a stable run identifier from the ResolveInput.
// Primary source: RunSpec.RunID prefixed with TenantID.
// Fallback: TraceID. If neither is available, returns "".
func runIDFromInput(in frontdoor.ResolveInput) string {
	if rs, ok := in.Req.(*api.RunSpec); ok && rs.RunID != "" {
		if in.Meta.TenantID != "" {
			return in.Meta.TenantID + ":" + rs.RunID
		}
		return rs.RunID
	}
	return in.Meta.TraceID
}

// Handle Resolve → (없으면) 세마 확보 → Create+등록 → OnTerminate에서 반납 → go Loop → Enqueue
/*func (d *Dispatcher) Handle(ctx context.Context, in frontdoor.ResolveInput, sink api.EventSink) error {
	// 0) 라우팅
	rr, err := d.FD.Resolve(ctx, in)
	if err != nil {
		return err
	}

	// 1) 액터 조회
	act, ok := d.AF.Get(rr.SpawnKey)
	if !ok {
		// 2) slot=actor 정책: 세마포어 먼저 확보 (non-blocking)
		select {
		case d.Sem <- struct{}{}:
			// 3) 경합 고려: Create에서 이미 누가 등록했을 수 있음
			var created bool
			act, created, err = d.AF.Create(rr.SpawnKey)
			if err != nil {
				<-d.Sem // 롤백
				return err
			}

			if created {
				// 4) 새 액터: 종료 시 슬롯 반납 훅 설치 → 루프 시작
				act.OnTerminate(func() { <-d.Sem })
				// TODO actor 의 라이프사이클과 context 관리는 생각해줘야 한다.
				go act.Loop(ctx)
			} else {
				// 5) 이미 존재: 우리가 잡은 슬롯은 불필요 → 즉시 반납
				<-d.Sem
				// 루프는 기존 액터가 이미 가지고 있다고 가정(Admit-then-Start 설계)
			}

		default:
			return sErr.ErrSaturated
		}
	}
	// else: 이미 액터 있음 → slot=actor에선 재획득 불필요

	// 6) 이벤트 싱크 기본값
	s := sink
	if s == nil {
		if d.defaultSink != nil {
			s = d.defaultSink
		} else {
			s = NoopSink{}
		}
	}

	// 7) 커맨드 부착 및 전송 (백프레셔 정책: EnqueueCtx 사용 권장)
	rr.Cmd.Sink = s
	if ok := act.EnqueueCtx(ctx, rr.Cmd); !ok {
		return sErr.ErrMailboxFull
	}
	return nil
}*/

// Handle Resolve → (없으면) 세마확보 → Bind → Register → CmdBind → Enqueue
// 이미 바운드된 경우엔 세마 재획득/바인드 불필요, 바로 Enqueue.
//
// RunStore 경계 (runStore != nil 일 때):
//  1. Handle() 진입 시 run을 StateQueued로 Enqueue (idempotent).
//  2. K8s 불가 상태(k8sAvailable=false)이면 queued→held 전이 후 ErrK8sUnavailable 반환.
//     run은 RunStore에 held 상태로 보존된다 — panic 없음.
//  3. 디스패치 성공 시 queued→admitted-to-dag 전이.
//  4. ErrSaturated 시 run은 queued 상태 그대로 유지 (자연 재시도 가능).
func (d *Dispatcher) Handle(ctx context.Context, in frontdoor.ResolveInput, sink api.EventSink) error {
	// ── ingress gate: RunStore 경계 ────────────────────────────────────────────
	runID := runIDFromInput(in)
	if d.runStore != nil && runID != "" {
		payload, _ := json.Marshal(in.Req) // best-effort; nil on non-RunSpec
		enqErr := d.runStore.Enqueue(ctx, store.RunRecord{
			RunID:   runID,
			State:   store.StateQueued,
			Payload: payload,
		})
		if enqErr != nil && !errors.Is(enqErr, store.ErrAlreadyExists) {
			return enqErr
		}

		// K8s 불가: queued → held, 디스패치 건너뜀
		if !d.k8sAvailable {
			_ = d.runStore.UpdateState(ctx, runID, store.StateQueued, store.StateHeld)
			log.Printf("[ingress] k8s unavailable: run %s → held", runID)
			return sErr.ErrK8sUnavailable
		}
	}
	// ──────────────────────────────────────────────────────────────────────────

	// 0) 라우팅
	rr, err := d.FD.Resolve(ctx, in)
	if err != nil {
		return err
	}

	// 1) sink 기본값 확정
	s := sink
	if s == nil {
		if d.defaultSink != nil {
			s = d.defaultSink
		} else {
			s = NoopSink{}
		}
	}

	// 2) 바운드 조회
	act, ok := d.AF.Get(rr.SpawnKey)
	if !ok {
		// 3) 바인딩 기간 동안만 세마 점유 (non-blocking)
		select {
		case d.Sem <- struct{}{}:
			// 4) idle 워커 가져오거나 새로 생성
			var created bool
			act, created, err = d.AF.Bind(rr.SpawnKey)
			if err != nil {
				<-d.Sem // 롤백
				return err
			}

			// 5) 새 워커면 루프 시작 (idle에서 꺼냈는데 루프가 이미 돌고 있다면 그대로 OK)
			if created {
				base := d.loopBaseCtx
				if base == nil {
					base = ctx
				}
				go act.Loop(base)
			}

			// 6) 바운드 등록
			d.AF.Register(rr.SpawnKey, act)

			// 7) 액터에 바인드 이벤트 전달
			bindCmd := api.Command{
				Kind: api.CmdBind,
				Bind: &api.Bind{SpawnKey: rr.SpawnKey},
				Sink: s,
			}

			sendCtx := ctx
			cancel := func() {}
			if d.enqueueTimeout > 0 {
				sendCtx, cancel = context.WithTimeout(ctx, d.enqueueTimeout)
			}
			defer cancel()

			if ok := act.EnqueueCtx(sendCtx, bindCmd); !ok {
				// 바인드 실패 → 안전 언바인드 + 세마 반납
				d.AF.Unbind(rr.SpawnKey, act)
				<-d.Sem
				return sErr.ErrMailboxFull
			}

		default:
			return sErr.ErrSaturated
		}
	}

	// 8) 본 작업 커맨드 전송
	rr.Cmd.Sink = s

	sendCtx := ctx
	cancel := func() {}
	if d.enqueueTimeout > 0 {
		sendCtx, cancel = context.WithTimeout(ctx, d.enqueueTimeout)
	}
	defer cancel()

	if ok := act.EnqueueCtx(sendCtx, rr.Cmd); !ok {
		return sErr.ErrMailboxFull
	}

	// ── admitted: queued → admitted-to-dag ────────────────────────────────────
	if d.runStore != nil && runID != "" {
		if err := d.runStore.UpdateState(ctx, runID, store.StateQueued, store.StateAdmittedToDag); err != nil {
			// Log but don't fail: run is already dispatched.
			// ErrAlreadyExists-equivalent: run was re-submitted and already advanced.
			log.Printf("[ingress] warn: UpdateState admitted: %v", err)
		}
	}
	// ──────────────────────────────────────────────────────────────────────────

	return nil
}

// onPipelineDone: 파이프라인 완료 시 호출.
// 순서: CmdUnbind → Factory.Unbind → 세마 반납
func (d *Dispatcher) onPipelineDone(ctx context.Context, spawnKey string, sink api.EventSink) {
	act, ok := d.AF.Get(spawnKey)
	if !ok {
		return
	}

	s := sink
	if s == nil {
		if d.defaultSink != nil {
			s = d.defaultSink
		} else {
			s = NoopSink{}
		}
	}

	// 1) 액터에 언바인드 이벤트 전달 (메일박스 통해 순서/일관성 보장)
	_ = act.EnqueueCtx(ctx, api.Command{
		Kind:   api.CmdUnbind,
		Unbind: &api.Unbind{},
		Sink:   s,
	})

	// 2) 팩토리에서 언바인드 → idle 풀 환원
	d.AF.Unbind(spawnKey, act)

	// 3) 세마 반납 (바인딩 기간이 끝났음을 의미)
	select {
	case <-d.Sem:
	default:
		// 정상 경로에선 여기에 안 들어옴(반드시 점유 중이어야 함)
	}
}
