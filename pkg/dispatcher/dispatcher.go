package dispatcher

import (
	"context"

	"github.com/seoyhaein/spawner/pkg/api"
	sErr "github.com/seoyhaein/spawner/pkg/error"
	fac "github.com/seoyhaein/spawner/pkg/factory"
	"github.com/seoyhaein/spawner/pkg/frontdoor"
)

//type EventSink = api.EventSink

type Dispatcher struct {
	FD  frontdoor.FrontDoor
	AF  fac.Factory
	Sem chan struct{} // slot=actor

	defaultSink api.EventSink // 지정 안 하면 NoopSink 사용
}

// Deprecated: Use NewDispatcher instead.
// New is deprecated.
func New(fd frontdoor.FrontDoor, af fac.Factory, maxActors int) *Dispatcher {
	return &Dispatcher{
		FD:  fd,
		AF:  af,
		Sem: make(chan struct{}, maxActors)}
}

// NewDispatcher TODO 테스트 필요.
func NewDispatcher(fd frontdoor.FrontDoor, af fac.Factory, semSize int, opts ...Option) *Dispatcher {
	d := &Dispatcher{
		FD: fd,
		AF: af,
		Sem: make(chan struct{},
			semSize)}

	for _, o := range opts {
		o(d)
	}
	return d
}

type Option func(*Dispatcher)

func WithDefaultSink(s api.EventSink) Option { return func(d *Dispatcher) { d.defaultSink = s } }

func (d *Dispatcher) Handle(ctx context.Context, in frontdoor.ResolveInput, sink api.EventSink) error {
	rr, err := d.FD.Resolve(ctx, in)
	if err != nil {
		return err
	}

	// spawnKey 단위로 액터를 가져오거나 생성
	act, created, err := d.AF.GetOrCreate(rr.SpawnKey)
	if err != nil {
		return err
	}

	// 새로 만든 액터라면: 세마포어 슬롯을 점유하고, 종료 시 반납하도록 훅 설치
	if created {
		select {
		case d.Sem <- struct{}{}:
			// 팩토리가 루프를 이미 시작하는 설계라면, 여기서는 종료 훅만 건다.
			act.OnTerminate(func() { <-d.Sem })
			// TODO 생각해보기.
			// go act.Loop(ctx)
		default:
			// 슬롯이 없다면 포화. (주의: 이 경우 막 생성된 액터는 팩토리 쪽 수명 정책을 따름)
			// 완전한 레이스 제거를 원하면, 팩토리에 "지연 시작" 옵션을 두고
			// 세마포어 확보 후 디스패처가 go act.Loop(ctx) 하도록 구조를 바꾸는 것을 권장.
			return sErr.ErrSaturated
		}
	}

	// 이벤트 싱크 설정 (전달된 sink가 없으면 기본값 사용)
	s := sink
	if s == nil {
		if d.defaultSink != nil {
			s = d.defaultSink
		} else {
			s = NoopSink{}
		}
	}

	// 커맨드에 싱크 부착 후 큐에 적재
	rr.Cmd.Sink = s
	if ok := act.Enqueue(rr.Cmd); !ok {
		return sErr.ErrMailboxFull
	}
	return nil
}
