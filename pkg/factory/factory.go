package factory

import (
	"context"
	"sync"

	"github.com/seoyhaein/spawner/pkg/actor"
	"github.com/seoyhaein/spawner/pkg/driver"
)

type Factory interface {
	GetOrCreate(spawnKey string) (actor.Actor, bool, error)
}

type DriverMaker func(spawnKey string) driver.Driver
type ActorMaker func(spawnKey string, drv driver.Driver, mbSize int) actor.Actor

type FactoryImp struct {
	ctx       context.Context
	mu        sync.RWMutex
	reg       map[string]actor.Actor
	makeDrv   DriverMaker
	makeActor ActorMaker
	mbSize    int
}

func NewFactory(ctx context.Context, mkDrv DriverMaker, mkActor ActorMaker, mbSize int) *FactoryImp {
	return &FactoryImp{
		ctx:       ctx,
		reg:       map[string]actor.Actor{},
		makeDrv:   mkDrv,
		makeActor: mkActor,
		mbSize:    mbSize,
	}
}

func (f *FactoryImp) GetOrCreate(spawnKey string) (actor.Actor, bool, error) {
	f.mu.RLock()
	if a, ok := f.reg[spawnKey]; ok {
		f.mu.RUnlock()
		return a, false, nil
	}
	f.mu.RUnlock()

	f.mu.Lock()
	defer f.mu.Unlock()

	if a, ok := f.reg[spawnKey]; ok {
		return a, false, nil
	}

	drv := f.makeDrv(spawnKey)
	act := f.makeActor(spawnKey, drv, f.mbSize)
	f.reg[spawnKey] = act

	go act.Loop(f.ctx) // 외부에서 주입받은 ctx로 수명 제어
	return act, true, nil
}
