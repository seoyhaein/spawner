package actor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/driver"
)

type SpawnActor struct {
	key string
	mb  *Mailbox[api.Command]
	drv driver.Driver

	onTerm func()

	mu      sync.Mutex
	running bool
	cur     struct {
		runID  string
		h      driver.Handle
		cancel context.CancelFunc
	}
}

type Actor interface {
	Enqueue(api.Command) bool
	OnTerminate(func())
	Loop(context.Context)
}

func NewSpawnActor(key string, drv driver.Driver, mbSize int) *SpawnActor {
	return &SpawnActor{key: key, drv: drv, mb: NewMailbox[api.Command](mbSize)}
}

func (a *SpawnActor) OnTerminate(fn func())      { a.onTerm = fn }
func (a *SpawnActor) Enqueue(c api.Command) bool { return a.mb.Enqueue(c) }

// 내부 헬퍼: 현재 실행 중인지와 runID 일치 여부
func (a *SpawnActor) isCurrent(runID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running && a.cur.runID == runID
}

// TODO 확인해야함.

func (a *SpawnActor) Loop(ctx context.Context) {
	defer func() {
		// 종료 시 현재 실행 중이면 취소
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
				// 동시에 하나만 실행 (slot=actor 정책 전제)
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

				// per-run 컨텍스트 구성 (타임아웃 적용)
				runCtx, cancel := context.WithCancel(ctx)
				if d := cmd.Policy.Timeout; d > 0 {
					runCtx, cancel = context.WithTimeout(ctx, d)
				}
				// 현재 실행 컨텍스트 등록
				a.mu.Lock()
				a.cur.cancel = cancel
				a.mu.Unlock()

				// 실행
				go func(runID string, c api.Command) {
					defer func() {
						// 실행 종료 정리
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
							// 핸들 등록
							a.mu.Lock()
							a.cur.h = h
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
				// 대상 runID가 지정돼 있다면 현재 것과 매칭
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

				// 우선 드라이버 취소 시도
				_ = a.drv.Cancel(ctx, h)
				// 추가로 컨텍스트 취소(soft cancel)
				if cancel != nil {
					cancel()
				}
				emitState(cmd.Sink, a.key, curID, api.StateCancelling, "")

			case api.CmdSignal:
				// 신호는 현재 핸들에만 적용
				a.mu.Lock()
				h := a.cur.h
				running := a.running
				curID := a.cur.runID
				a.mu.Unlock()

				if !running || h == nil || cmd.Signal == nil {
					emitErr(cmd.Sink, a.key, curID, errors.New("no active handle or empty signal"))
					continue
				}
				_ = a.drv.Signal(ctx, h, *cmd.Signal)

			case api.CmdQuery:
				// 최소 구현: 현재 상태만 발행
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

// ---- 이벤트 유틸 ----

// 상태/에러를 구분해 보내기 (Send가 블로킹일 수 있어 타임아웃 가드)
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
	go func() {
		s.Send(ev) // 인터페이스가 에러 반환 없다고 가정
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		// 드랍(또는 로깅 훅)
	}
}
