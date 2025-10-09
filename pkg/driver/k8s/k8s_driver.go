package k8s

import (
	"context"

	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/driver"
)

// TODO 껍데기만 잡아놈. 메서드의 껍데기도 수정 가능하고, 내용도 추가 구현해야 함.

type DriverK8s struct{}

func New() *DriverK8s { return &DriverK8s{} }

func (d *DriverK8s) Prepare(ctx context.Context, spec api.RunSpec) (driver.Prepared, error) {
	return driver.Prepared{}, nil
}
func (d *DriverK8s) Start(ctx context.Context, p driver.Prepared) (driver.Handle, error) {
	return driver.Handle{}, nil
}
func (d *DriverK8s) Wait(ctx context.Context, h driver.Handle) (api.Event, error) {
	return api.Event{}, nil
}
func (d *DriverK8s) Signal(ctx context.Context, h driver.Handle, sig api.Signal) error { return nil }
func (d *DriverK8s) Cancel(ctx context.Context, h driver.Handle) error                 { return nil }
