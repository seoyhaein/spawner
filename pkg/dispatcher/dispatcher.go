package dispatcher

import (
	"context"
	"errors"

	"github.com/seoyhaein/spawner/pkg/actor"
	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/frontdoor"
)

type Dispatcher struct {
	FD  frontdoor.FrontDoor
	AF  actor.Factory
	Sem chan struct{} // slot=actor
}

var ErrSaturated = errors.New("dispatcher saturated: max concurrent actors reached")

func New(fd frontdoor.FrontDoor, af actor.Factory, maxActors int) *Dispatcher {
	return &Dispatcher{
		FD:  fd,
		AF:  af,
		Sem: make(chan struct{}, maxActors)}
}

func (d *Dispatcher) Handle(ctx context.Context, in frontdoor.ResolveInput, sink EventSink) error {
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
		default:
			return ErrSaturated
		}
	}

	// attach sink coming from transport
	rr.Cmd.Sink = eventSinkAdapter{sink}

	if ok := act.Enqueue(rr.Cmd); !ok {
		return errors.New("mailbox full")
	}
	return nil
}

// thin adapter to avoid import cycle with api.EventSink
type eventSinkAdapter struct{ s EventSink }

func (a eventSinkAdapter) Send(e api.Event) { a.s.Send(e) }
