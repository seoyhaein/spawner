package actor

import (
	"context"

	"github.com/seoyhaein/spawner/pkg/api"
)

type Actor interface {
	EnqueueTry(api.Command) bool
	EnqueueCtx(context.Context, api.Command) bool
	OnTerminate(func())
	Loop(context.Context)
}
