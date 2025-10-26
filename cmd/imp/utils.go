package imp

import (
	"time"

	"github.com/seoyhaein/spawner/pkg/api"
)

// ---- 이벤트 유틸 (그대로) ----
func emitState(s api.EventSink, spawnKey, runID string, st api.State, msg string) {
	ev := api.Event{SpawnKey: spawnKey, RunID: runID, When: time.Now(), State: st, Message: msg}
	sendWithTimeout(s, ev, 3*time.Second)
}
func emitErr(s api.EventSink, spawnKey, runID string, err error) {
	ev := api.Event{SpawnKey: spawnKey, RunID: runID, When: time.Now(), State: api.StateFailed, Message: err.Error()}
	sendWithTimeout(s, ev, 3*time.Second)
}
func sendWithTimeout(s api.EventSink, ev api.Event, d time.Duration) {
	if s == nil {
		return
	}
	done := make(chan struct{})
	go func() { s.Send(ev); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
	}
}
