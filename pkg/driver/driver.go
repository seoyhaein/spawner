package driver

import (
	"context"

	"github.com/seoyhaein/spawner/pkg/api"
)

type Prepared struct{}

type Handle struct{}

type Driver interface {
	Prepare(ctx context.Context, spec api.RunSpec) (Prepared, error)
	Start(ctx context.Context, p Prepared) (Handle, error)
	Wait(ctx context.Context, h Handle) (api.Event, error)
	Signal(ctx context.Context, h Handle, sig api.Signal) error
	Cancel(ctx context.Context, h Handle) error
}
