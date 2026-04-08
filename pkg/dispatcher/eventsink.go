package dispatcher

import (
	"github.com/seoyhaein/spawner/pkg/api"
)

type NoopSink struct{}

func (NoopSink) Send(api.Event) {}
