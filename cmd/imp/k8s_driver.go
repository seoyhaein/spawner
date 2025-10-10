package imp

import (
	"context"
	"fmt"
	"time"

	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/driver"
)

// driver 와 actor 실제 설계를 잘 해야 함.

type preparedPod struct {
	driver.BasePrepared /* + spec fields */
}
type handlePod struct {
	driver.BaseHandle
	name, ns string
}

type DriverK8s struct {
	driver.BaseDriver
	ns string
}

var _ driver.Driver = (*DriverK8s)(nil) // 컴파일타임 어서션

func NewK8s(ns string) *DriverK8s { return &DriverK8s{ns: ns} }

func (d *DriverK8s) Prepare(ctx context.Context, spec api.RunSpec) (driver.Prepared, error) {
	// TODO: spec 검증/가공
	return preparedPod{}, nil
}
func (d *DriverK8s) Start(ctx context.Context, p driver.Prepared) (driver.Handle, error) {
	pp, ok := p.(preparedPod)
	if !ok {
		return nil, fmt.Errorf("prepared origin mismatch")
	}
	_ = pp
	// TODO: create resource
	return handlePod{name: fmt.Sprintf("run-%d", time.Now().UnixNano()), ns: d.ns}, nil
}
func (d *DriverK8s) Wait(ctx context.Context, h driver.Handle) (api.Event, error) {
	hd, ok := h.(handlePod)
	if !ok {
		return api.Event{}, fmt.Errorf("handle origin mismatch")
	}
	_ = hd
	// TODO: watch/wait
	return api.Event{When: time.Now(), State: api.StateSucceeded}, nil
}
func (d *DriverK8s) Signal(ctx context.Context, h driver.Handle, sig api.Signal) error {
	hd, ok := h.(handlePod)
	if !ok {
		return fmt.Errorf("handle origin mismatch")
	}
	_ = hd
	_ = sig
	return nil
}
func (d *DriverK8s) Cancel(ctx context.Context, h driver.Handle) error {
	hd, ok := h.(handlePod)
	if !ok {
		return fmt.Errorf("handle origin mismatch")
	}
	_ = hd
	return nil
}
