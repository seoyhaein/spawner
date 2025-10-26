package dispatcher

import (
	"context"
	"time"

	"github.com/seoyhaein/spawner/pkg/api"
	sErr "github.com/seoyhaein/spawner/pkg/error"
	fac "github.com/seoyhaein/spawner/pkg/factory"
	"github.com/seoyhaein/spawner/pkg/frontdoor"
)

type Dispatcher struct {
	FD          frontdoor.FrontDoor
	AF          fac.Factory
	Sem         chan struct{} // slot=actor, 바인딩 기간 동안만 점유
	defaultSink api.EventSink // 지정 안 하면 NoopSink 사용
	// 추가
	loopBaseCtx    context.Context // (옵션) 액터 루프 베이스 컨텍스트
	enqueueTimeout time.Duration   // (옵션) EnqueueCtx 타임아웃
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
		FD:  fd,
		AF:  af,
		Sem: make(chan struct{}, semSize),
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
func (d *Dispatcher) Handle(ctx context.Context, in frontdoor.ResolveInput, sink api.EventSink) error {
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
