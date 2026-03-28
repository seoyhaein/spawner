package imp

import (
	"context"
	"errors"

	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/driver"
)

// ErrK8sUnavailable is returned by NopDriver when the K8s cluster is
// unreachable (e.g., kubeconfig not found at server startup).
// Callers that observe this error should keep the run in StateQueued or
// StateHeld instead of transitioning to StateAdmittedToDag.
var ErrK8sUnavailable = errors.New("k8s unavailable: driver not initialized")

// NopDriver is a driver.Driver that always returns ErrK8sUnavailable.
// Used as a safe fallback when NewK8sFromKubeconfig fails at startup.
// This prevents the process from panicking and allows the RunStore to
// preserve queued/held runs until K8s connectivity is restored.
//
// ASSUMPTION: production adds a liveness/readiness probe that switches
// back to DriverK8s once connectivity is confirmed.
type NopDriver struct {
	driver.BaseDriver
}

var _ driver.Driver = (*NopDriver)(nil)

func (NopDriver) Prepare(_ context.Context, _ api.RunSpec) (driver.Prepared, error) {
	return nil, ErrK8sUnavailable
}
func (NopDriver) Start(_ context.Context, _ driver.Prepared) (driver.Handle, error) {
	return nil, ErrK8sUnavailable
}
func (NopDriver) Wait(_ context.Context, _ driver.Handle) (api.Event, error) {
	return api.Event{}, ErrK8sUnavailable
}
func (NopDriver) Signal(_ context.Context, _ driver.Handle, _ api.Signal) error {
	return ErrK8sUnavailable
}
func (NopDriver) Cancel(_ context.Context, _ driver.Handle) error {
	return ErrK8sUnavailable
}
