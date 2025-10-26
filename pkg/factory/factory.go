package factory

import (
	"context"
	"sync"

	"github.com/seoyhaein/spawner/pkg/actor"
	"github.com/seoyhaein/spawner/pkg/driver"
)

// Factory 는 두 가지 경로를 모두 지원
// 1) 레거시: Get/Create/Delete  (spawnKey ↔ actor 고정 매핑)
// 2) 재활용: Bind/Register/Unbind (Idle 풀에서 워커 재사용, 바인딩 기간만 매핑)

type Factory interface {
	// Get 이미 등록된 액터를 조회 (루프 상태와 무관)
	// 현재 "바운드된" 액터(있다면)를 조회. (재활용 모드에선 regBound를 우선 확인)
	Get(spawnKey string) (actor.Actor, bool)

	// Create 아직 없다면 생성·등록. 여기서는 절대 Loop를 시작하지 않음. -> 시작하면 좀비 될 수 있음.
	// 경합 시 이미 누가 등록했다면 (act, false, nil) 반환.
	Create(spawnKey string) (act actor.Actor, created bool, err error)

	// Delete 롤백/GC용: 동일 인스턴스일 때만 안전 삭제
	Delete(spawnKey string, act actor.Actor) bool

	// Bind TODO 생각해볼 점: Unregister vs Delete
	//  === 재활용 경로 ===
	// (a) 이미 해당 spawnKey로 바운드된 액터가 있으면 그걸 반환(created=false),
	// (b) 아니면 idle 풀에서 하나 꺼내거나 새로 생성해 반환(created=true).
	// 이 단계에서는 등록하지 않는다(Dispatcher가 Register 호출).

	Bind(spawnKey string) (act actor.Actor, created bool, err error)
	// Register : spawnKey ↔ actor 바인딩 등록
	Register(spawnKey string, act actor.Actor)
	// Unbind : 바인딩 해제 후 actor를 idle 풀로 되돌림
	Unbind(spawnKey string, act actor.Actor)
}

// 외부에서 주입할 팩토리 메이커들

type DriverMaker func(spawnKey string) driver.Driver
type ActorMaker func(spawnKey string, drv driver.Driver, mbSize int) actor.Actor

type FactoryImp struct {
	ctx context.Context
	mu  sync.RWMutex

	// actor 저장소
	reg map[string]actor.Actor

	// TODO 이렇게 두 개로 나누어 놓았는데 이건 생각해보자.
	// 재활용 모드: 현재 바운드된 액터와 idle 풀
	regBound map[string]actor.Actor // spawnKey -> actor (바인딩 중)
	idlePool []actor.Actor          // 바인딩 해제된 워커(루프는 살아있다고 가정)

	makeDrv   DriverMaker
	makeActor ActorMaker
	mbSize    int
}

func NewFactory(ctx context.Context, mkDrv DriverMaker, mkActor ActorMaker, mbSize int) *FactoryImp {
	return &FactoryImp{
		ctx: ctx,
		// 추가
		reg:      make(map[string]actor.Actor),
		regBound: make(map[string]actor.Actor),

		idlePool:  nil,
		makeDrv:   mkDrv,
		makeActor: mkActor,
		mbSize:    mbSize,
	}
}

func (f *FactoryImp) Get(spawnKey string) (actor.Actor, bool) {
	// 재활용 경로 우선(바운딩된 액터가 있으면 그걸 반환)
	f.mu.RLock()
	if a, ok := f.regBound[spawnKey]; ok {
		f.mu.RUnlock()
		return a, true
	}
	// 그 외엔 레거시 저장소를 조회(기존 코드 호환)
	a, ok := f.reg[spawnKey]
	f.mu.RUnlock()
	return a, ok
}

/*func (f *FactoryImp) Create(spawnKey string) (actor.Actor, bool, error) {
	// 1차 조회 (fast path)
	if a, ok := f.Get(spawnKey); ok {
		return a, false, nil
	}

	// 등록 경합 보호
	f.mu.Lock()
	defer f.mu.Unlock()

	if a, ok := f.reg[spawnKey]; ok {
		return a, false, nil
	}

	drv := f.makeDrv(spawnKey)
	act := f.makeActor(spawnKey, drv, f.mbSize)
	f.reg[spawnKey] = act
	// 여기서는 절대 Loop를 시작하지 않는다.
	return act, true, nil
}*/

func (f *FactoryImp) Create(spawnKey string) (actor.Actor, bool, error) {
	// fast path
	if a, ok := f.Get(spawnKey); ok {
		return a, false, nil
	}

	// 등록 경합 보호
	f.mu.Lock()
	defer f.mu.Unlock()

	// 다시 확인 (경합)
	if a, ok := f.regBound[spawnKey]; ok {
		return a, false, nil
	}
	if a, ok := f.reg[spawnKey]; ok {
		return a, false, nil
	}

	drv := f.makeDrv(spawnKey)
	act := f.makeActor(spawnKey, drv, f.mbSize)
	// 레거시 경로에선 reg에 등록
	f.reg[spawnKey] = act
	// 여기서는 절대 Loop를 시작하지 않는다.
	return act, true, nil
}

func (f *FactoryImp) Delete(spawnKey string, act actor.Actor) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cur, ok := f.reg[spawnKey]; ok && cur == act {
		delete(f.reg, spawnKey)
		return true
	}
	return false
}

// ===== 재활용 경로 =====

// Bind 은 현재 바운드된 액터가 있으면 그걸 반환(created=false).
// 없으면 idle 풀에서 하나 꺼내거나 새로 생성(created=true)하여 반환한다.
// 주의: 여기서는 regBound에 등록하지 않는다. (Dispatcher가 Register를 호출)
func (f *FactoryImp) Bind(spawnKey string) (actor.Actor, bool, error) {
	// fast path: 이미 바운드되어 있다면 그걸 반환
	f.mu.RLock()
	if a, ok := f.regBound[spawnKey]; ok {
		f.mu.RUnlock()
		return a, false, nil
	}
	f.mu.RUnlock()

	f.mu.Lock()
	defer f.mu.Unlock()

	// 경합 재확인
	if a, ok := f.regBound[spawnKey]; ok {
		return a, false, nil
	}

	// idle 풀에서 하나 재사용
	var act actor.Actor
	if n := len(f.idlePool); n > 0 {
		act = f.idlePool[n-1]
		f.idlePool = f.idlePool[:n-1]
	} else {
		// 없으면 새로 생성
		drv := f.makeDrv(spawnKey)
		act = f.makeActor(spawnKey, drv, f.mbSize)
	}
	// 등록은 Dispatcher.Register가 수행
	return act, true, nil
}

// Register 는 바인딩 등록을 수행한다.
func (f *FactoryImp) Register(spawnKey string, act actor.Actor) {
	f.mu.Lock()
	f.regBound[spawnKey] = act
	f.mu.Unlock()
}

// Unbind 는 바인딩을 해제하고 워커를 idle 풀로 환원한다.
func (f *FactoryImp) Unbind(spawnKey string, act actor.Actor) {
	f.mu.Lock()
	if cur, ok := f.regBound[spawnKey]; ok && cur == act {
		delete(f.regBound, spawnKey)
		f.idlePool = append(f.idlePool, act)
	}
	f.mu.Unlock()
}
