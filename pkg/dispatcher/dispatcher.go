package dispatcher

import (
	"context"

	"github.com/seoyhaein/spawner/pkg/actor"
	"github.com/seoyhaein/spawner/pkg/api"
	sErr "github.com/seoyhaein/spawner/pkg/error"
	"github.com/seoyhaein/spawner/pkg/frontdoor"
)

//type EventSink = api.EventSink

type Dispatcher struct {
	FD  frontdoor.FrontDoor
	AF  actor.Factory
	Sem chan struct{} // slot=actor

	defaultSink api.EventSink // 지정 안 하면 NoopSink 사용
}

// Deprecated: Use NewDispatcher instead.
// New is deprecated.
func New(fd frontdoor.FrontDoor, af actor.Factory, maxActors int) *Dispatcher {
	return &Dispatcher{
		FD:  fd,
		AF:  af,
		Sem: make(chan struct{}, maxActors)}
}

// NewDispatcher TODO 테스트 필요.
func NewDispatcher(fd frontdoor.FrontDoor, af actor.Factory, semSize int, opts ...Option) *Dispatcher {
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

	// Create/get session actor (slot=actor policy)
	// 루프 돌림.
	act, created, err := d.AF.GetOrCreate(rr.SpawnKey)
	if err != nil {
		return err
	}
	if created {
		select {
		case d.Sem <- struct{}{}:

			act.OnTerminate(func() { <-d.Sem })
			// TODO 생각해보기.
			// go act.Loop(ctx)
		default:
			return sErr.ErrSaturated
		}
	}
	s := sink
	if s == nil {
		if d.defaultSink != nil {
			s = d.defaultSink
		} else {
			s = NoopSink{}
		}
	}

	// attach sink coming from transport
	rr.Cmd.Sink = s

	if ok := act.Enqueue(rr.Cmd); !ok {
		return sErr.ErrMailboxFull
	}
	return nil
}
