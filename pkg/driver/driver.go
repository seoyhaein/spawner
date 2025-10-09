package driver

import (
	"context"

	"github.com/seoyhaein/spawner/pkg/api"
)

type Prepared struct{}

type Handle struct{}

// TODO 디버깅 및 추후 확장성을 위해 ID 필드 추가 고려
// type Handle struct{ ID string }

type Driver interface {
	Prepare(ctx context.Context, spec api.RunSpec) (Prepared, error)
	Start(ctx context.Context, p Prepared) (Handle, error)
	Wait(ctx context.Context, h Handle) (api.Event, error)
	Signal(ctx context.Context, h Handle, sig api.Signal) error
	Cancel(ctx context.Context, h Handle) error
}
