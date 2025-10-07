package actor

import (
	"context"
	"sync"

	"github.com/seoyhaein/spawner/pkg/driver"
)

// TODO 팩토리 패턴으로 별도의 인터페이스를 만들어서 이렇게 actor 를 만들어주는 형태로 해주었는데, 이거 코드만 늘어나는 거 같은데.
// TODO driver 를 연결시켜주는 문제가 있어서 인터페이스를 빼는 것이 더 낳을 수도 있다라는 생각도 있음.

type Factory interface {
	GetOrCreate(spawnKey string) (Actor, bool, error)
}

/*type Factory interface {
	GetOrCreate(spawnKey string) (*SessionActor, bool, error)
}*/

type FactoryInMem struct {
	mu       sync.RWMutex
	reg      map[string]*SpawnActor
	mkDriver func(key string) driver.Driver
	mbSize   int
}

func NewFactoryInMem(mkDrv func(string) driver.Driver, mbSize int) *FactoryInMem {
	return &FactoryInMem{
		reg:      map[string]*SpawnActor{},
		mkDriver: mkDrv, mbSize: mbSize}
}

func (f *FactoryInMem) GetOrCreate(spawnKey string) (Actor, bool, error) {
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

	drv := f.mkDriver(spawnKey)
	a := NewSpawnActor(spawnKey, drv, f.mbSize)
	f.reg[spawnKey] = a

	// start loop
	go a.Loop(context.Background())
	return a, true, nil
}
