package actor

import (
	"context"

	"github.com/seoyhaein/spawner/pkg/api"
)

type Actor interface {
	Enqueue(api.Command) bool
	OnTerminate(func())
	Loop(context.Context)
}
