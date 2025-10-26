package imp

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/seoyhaein/spawner/pkg/actor"
	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/driver"
)

// TODO actor 안에 mailbox 는 반드시 들어가야 한다. 이거 구현해주자.
// TODO 좀더 깔끔하게 만들 수 있음. driver 참고. 사실 Loop 만 생각해줘야 하나. 아닐 거 같기도 하고. 초기 설계를 어떻게 잘해갈것인지가 중요.

type SpawnActor struct {
	key string
	mb  *actor.Mailbox[api.Command]
	drv driver.Driver // 구체타입(DriverK8s) 말고 인터페이스

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
	return &SpawnActor{
		key: key,
		drv: drv,
		mb:  actor.NewMailbox[api.Command](mbSize)}
}

func (a *SpawnActor) OnTerminate(fn func()) {
	a.onTerm = fn
}

// 정책 분리: 드롭(논블로킹) vs 백프레셔(컨텍스트 대기)

func (a *SpawnActor) EnqueueTry(c api.Command) bool {
	return a.mb.TryEnqueue(c)
}

func (a *SpawnActor) EnqueueCtx(ctx context.Context, c api.Command) bool {
	return a.mb.Enqueue(ctx, c)
}

// CloseInbox 외부에서 명시적으로 새 전송 금지시키고 싶을 때
func (a *SpawnActor) CloseInbox() {
	a.mb.Close()
}

func (a *SpawnActor) isCurrent(runID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running && a.cur.runID == runID
}

/*func (a *SpawnActor) Loop(ctx context.Context) {
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
}*/

func (a *SpawnActor) Loop(ctx context.Context) {
	defer func() {
		// 더 이상 새 메시지 금지 + 생산자 종료 후 데이터채널 close
		a.mb.Close()

		// 현재 실행 중이면 취소
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
				return // 메일박스가 닫혀서 데이터 채널이 닫힌 경우
			}

			switch cmd.Kind {
			case api.CmdRun:
				// 동시에 하나만 실행
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

				// Timeout/Cancel 컨텍스트 명확 분기
				var runCtx context.Context
				var cancel context.CancelFunc
				if d := cmd.Policy.Timeout; d > 0 {
					runCtx, cancel = context.WithTimeout(ctx, d)
				} else {
					runCtx, cancel = context.WithCancel(ctx)
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
							a.cur.h = h
							a.mu.Unlock()

							// RUNNING 상태(선택) — Start 성공 시점
							emitState(c.Sink, a.key, runID, api.StateRunning, "")

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

				// 스냅샷 추출(락 짧게)
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

				// 상위 취소 전파 방지
				if h != nil {
					_ = a.drv.Cancel(context.WithoutCancel(ctx), h)
				}
				if cancel != nil {
					cancel()
				}
				emitState(cmd.Sink, a.key, curID, api.StateCancelling, "")

			case api.CmdSignal:
				// 스냅샷 추출
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
