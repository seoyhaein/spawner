package k8s

import (
	"context"

	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/driver"
)

// TODO 껍데기만 잡아놈. 메서드의 껍데기도 수정 가능하고, 내용도 추가 구현해야 함.

type K8sDriver struct{}

func New() *K8sDriver { return &K8sDriver{} }

func (d *K8sDriver) Prepare(ctx context.Context, spec api.RunSpec) (driver.Prepared, error) {
	return driver.Prepared{}, nil
}
func (d *K8sDriver) Start(ctx context.Context, p driver.Prepared) (driver.Handle, error) {
	return driver.Handle{}, nil
}
func (d *K8sDriver) Wait(ctx context.Context, h driver.Handle) (api.Event, error) {
	return api.Event{}, nil
}
func (d *K8sDriver) Signal(ctx context.Context, h driver.Handle, sig api.Signal) error { return nil }
func (d *K8sDriver) Cancel(ctx context.Context, h driver.Handle) error                 { return nil }
