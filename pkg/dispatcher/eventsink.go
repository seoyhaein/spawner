package dispatcher

import (
	"github.com/seoyhaein/spawner/pkg/api"
)

// Wire to your transport (gRPC streaming) in cmd/server.

type EventSink interface{ Send(api.Event) }
