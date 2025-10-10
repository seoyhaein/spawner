package imp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/seoyhaein/spawner/pkg/actor"
	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/driver"
)

type SpawnActor struct {
	key string
	mb  *actor.Mailbox[api.Command]
	drv driver.Driver // ✅ 구체타입(DriverK8s) 말고 인터페이스

	onTerm func()

	mu      sync.Mutex
	running bool
	cur     struct {
		runID  string
		h      driver.Handle // 인터페이스 (포인터 금지)
		cancel context.CancelFunc
	}
}

func NewSpawnActor(key string, drv driver.Driver, mbSize int) *SpawnActor {
	return &SpawnActor{key: key, drv: drv, mb: actor.NewMailbox[api.Command](mbSize)}
}

func (a *SpawnActor) OnTerminate(fn func())      { a.onTerm = fn }
func (a *SpawnActor) Enqueue(c api.Command) bool { return a.mb.Enqueue(c) }

func (a *SpawnActor) isCurrent(runID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running && a.cur.runID == runID
}

func (a *SpawnActor) Loop(ctx context.Context) {
	defer func() {
		a.mu.Lock()
		if a.running && a.cur.cancel != nil {
			a.cur.cancel()
		}
		a.mu.Unlock()
		if a.onTerm != nil {
			a.onTerm()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case cmd, ok := <-a.mb.C():
			if !ok {
				return
			}

			switch cmd.Kind {
			case api.CmdRun:
				a.mu.Lock()
				if a.running {
					a.mu.Unlock()
					emitErr(cmd.Sink, a.key, cmd.Run.RunID, errors.New("actor busy: run already in progress"))
					continue
				}
				a.running = true
				a.cur.runID = cmd.Run.RunID
				a.mu.Unlock()

				emitState(cmd.Sink, a.key, cmd.Run.RunID, api.StateStarting, "")

				runCtx, cancel := context.WithCancel(ctx)
				if d := cmd.Policy.Timeout; d > 0 {
					runCtx, cancel = context.WithTimeout(ctx, d)
				}
				a.mu.Lock()
				a.cur.cancel = cancel
				a.mu.Unlock()

				go func(runID string, c api.Command) {
					defer func() {
						a.mu.Lock()
						if a.cur.cancel != nil {
							a.cur.cancel()
						}
						a.running = false
						a.cur.runID, a.cur.h, a.cur.cancel = "", nil, nil
						a.mu.Unlock()
					}()

					p, err := a.drv.Prepare(runCtx, *c.Run)
					if err == nil {
						h, err2 := a.drv.Start(runCtx, p)
						if err2 == nil {
							a.mu.Lock()
							a.cur.h = h // ✅ 포인터 아님
							a.mu.Unlock()

							_, err = a.drv.Wait(runCtx, h)
						} else {
							err = err2
						}
					}
					if err != nil {
						emitErr(c.Sink, a.key, runID, err)
					} else {
						emitState(c.Sink, a.key, runID, api.StateSucceeded, "")
					}
				}(cmd.Run.RunID, cmd)

			case api.CmdCancel:
				target := strings.TrimSpace(cmd.Cancel.RunID)

				a.mu.Lock()
				h := a.cur.h
				cancel := a.cur.cancel
				running := a.running
				curID := a.cur.runID
				a.mu.Unlock()

				if !running {
					emitErr(cmd.Sink, a.key, target, errors.New("no run in progress"))
					continue
				}
				if target != "" && target != curID {
					emitErr(cmd.Sink, a.key, target, errors.New("cancel target mismatch: not current run"))
					continue
				}

				if h != nil {
					_ = a.drv.Cancel(context.WithoutCancel(ctx), h)
				} // ✅ 포인터 제거
				if cancel != nil {
					cancel()
				}
				emitState(cmd.Sink, a.key, curID, api.StateCancelling, "")

			case api.CmdSignal:
				a.mu.Lock()
				h := a.cur.h
				running := a.running
				curID := a.cur.runID
				a.mu.Unlock()

				if !running || h == nil || cmd.Signal == nil {
					emitErr(cmd.Sink, a.key, curID, errors.New("no active handle or empty signal"))
					continue
				}
				_ = a.drv.Signal(context.WithoutCancel(ctx), h, *cmd.Signal)

			case api.CmdQuery:
				a.mu.Lock()
				state := api.StateIdle
				runID := a.cur.runID
				if a.running {
					state = api.StateRunning
				}
				a.mu.Unlock()
				emitState(cmd.Sink, a.key, runID, state, "")
			}
		}
	}
}

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
