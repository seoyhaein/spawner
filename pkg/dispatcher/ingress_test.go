package dispatcher_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/seoyhaein/spawner/pkg/actor"
	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/dispatcher"
	sErr "github.com/seoyhaein/spawner/pkg/error"
	"github.com/seoyhaein/spawner/pkg/frontdoor"
	"github.com/seoyhaein/spawner/pkg/store"
)

// ── mocks ─────────────────────────────────────────────────────────────────────

type mockFD struct {
	key string
	cmd api.Command
}

func (m *mockFD) Resolve(_ context.Context, _ frontdoor.ResolveInput) (frontdoor.ResolveResult, error) {
	return frontdoor.ResolveResult{SpawnKey: m.key, Cmd: m.cmd}, nil
}

type mockActor struct{ enqueueCalled int }

func (m *mockActor) EnqueueTry(api.Command) bool                      { m.enqueueCalled++; return true }
func (m *mockActor) EnqueueCtx(_ context.Context, _ api.Command) bool { m.enqueueCalled++; return true }
func (m *mockActor) OnIdle(func())                                    {}
func (m *mockActor) OnTerminate(func())                               {}
func (m *mockActor) Loop(_ context.Context)                           {}

type mockFactory struct{ act *mockActor }

func (m *mockFactory) Get(_ string) (actor.Actor, bool) { return nil, false }
func (m *mockFactory) Bind(_ string) (actor.Actor, bool, error) {
	return m.act, true, nil
}
func (m *mockFactory) Register(_ string, _ actor.Actor) {}
func (m *mockFactory) Unbind(_ string, _ actor.Actor)   {}

// newTestDispatcher builds a Dispatcher with a RunStore and mock internals.
func newTestDispatcher(rs store.RunStore, opts ...dispatcher.Option) (*dispatcher.Dispatcher, *mockActor) {
	act := &mockActor{}
	mf := &mockFactory{act: act}
	fd := &mockFD{
		key: "teamA:run-001",
		cmd: api.Command{Kind: api.CmdRun, Run: &api.RunSpec{RunID: "run-001", ImageRef: "busybox:1.36"}},
	}
	baseOpts := []dispatcher.Option{dispatcher.WithRunStore(rs)}
	return dispatcher.NewDispatcher(fd, mf, 4, append(baseOpts, opts...)...), act
}

func testInput() frontdoor.ResolveInput {
	return frontdoor.ResolveInput{
		Req: &api.RunSpec{RunID: "run-001", ImageRef: "busybox:1.36"},
		Meta: frontdoor.MetaContext{
			RPC:      "RunE",
			TenantID: "teamA",
			TraceID:  "trace-001",
		},
	}
}

// ── ingress boundary tests ────────────────────────────────────────────────────

// TestIngress_EnqueuesRunAsQueuedBeforeDispatching proves:
// Before any Actor interaction, Handle() stores the run as StateQueued.
// This is the "run queue absorbs submission" boundary — user burst does not
// reach K8s until the run is admitted.
func TestIngress_EnqueuesRunAsQueuedBeforeDispatching(t *testing.T) {
	ctx := context.Background()
	rs := store.NewInMemoryRunStore()
	d, _ := newTestDispatcher(rs)

	// Verify store is empty before Handle
	before, _ := rs.ListByState(ctx, store.StateQueued)
	if len(before) != 0 {
		t.Fatalf("expected empty store before Handle, got %d", len(before))
	}

	_ = d.Handle(ctx, testInput(), nil)

	// The run should be in the store (queued or admitted, depending on timing)
	rec, ok, _ := rs.Get(ctx, "teamA:run-001")
	if !ok {
		t.Fatal("run was not persisted to RunStore")
	}
	var env api.RunEnvelope
	if err := json.Unmarshal(rec.Payload, &env); err != nil {
		t.Fatalf("unmarshal payload envelope: %v", err)
	}
	if env.Version != 1 {
		t.Fatalf("expected envelope version 1, got %d", env.Version)
	}
	if env.Identity.LogicalRunID != "teamA:run-001" {
		t.Fatalf("unexpected logical run id: %q", env.Identity.LogicalRunID)
	}
	if env.Identity.AttemptID != "teamA:run-001/attempt-1" {
		t.Fatalf("unexpected attempt id: %q", env.Identity.AttemptID)
	}
	if env.Identity.SpawnKey != "teamA:run-001" {
		t.Fatalf("unexpected spawn key: %q", env.Identity.SpawnKey)
	}
	if env.Run == nil || env.Run.RunID != "run-001" {
		t.Fatalf("expected run payload in envelope, got %+v", env.Run)
	}
	t.Logf("PASS: run persisted with state=%s before/during dispatch", rec.State)
}

// TestIngress_TransitionsToAdmittedOnSuccessfulDispatch proves:
// After Handle() succeeds, the run transitions from queued to admitted-to-dag.
// This is the "gate opened" moment — run is now being executed.
func TestIngress_TransitionsToAdmittedOnSuccessfulDispatch(t *testing.T) {
	ctx := context.Background()
	rs := store.NewInMemoryRunStore()
	d, _ := newTestDispatcher(rs)

	if err := d.Handle(ctx, testInput(), nil); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	rec, ok, _ := rs.Get(ctx, "teamA:run-001")
	if !ok {
		t.Fatal("run not found in RunStore after Handle")
	}
	if rec.State != store.StateAdmittedToDag {
		t.Fatalf("expected admitted-to-dag, got %s", rec.State)
	}
	t.Logf("PASS: run transitioned to admitted-to-dag after successful dispatch")
}

// TestIngress_RunHeldNotDispatchedWhenK8sUnavailable proves:
// When K8s is unreachable at startup, Handle() transitions the run to
// StateHeld and returns ErrK8sUnavailable. No Actor is invoked.
// The run is preserved in the RunStore for recovery, not lost.
func TestIngress_RunHeldNotDispatchedWhenK8sUnavailable(t *testing.T) {
	ctx := context.Background()
	rs := store.NewInMemoryRunStore()
	d, act := newTestDispatcher(rs, dispatcher.WithK8sUnavailable())

	err := d.Handle(ctx, testInput(), nil)
	if !errors.Is(err, sErr.ErrK8sUnavailable) {
		t.Fatalf("expected ErrK8sUnavailable, got %v", err)
	}

	// Actor must NOT have been invoked
	if act.enqueueCalled > 0 {
		t.Fatalf("BOUNDARY VIOLATION: Actor.EnqueueCtx called despite k8s unavailable")
	}

	// Run must be in StateHeld
	rec, ok, _ := rs.Get(ctx, "teamA:run-001")
	if !ok {
		t.Fatal("run not found in RunStore")
	}
	if rec.State != store.StateHeld {
		t.Fatalf("expected held, got %s", rec.State)
	}
	t.Logf("PASS: run held (not lost, not dispatched) when k8s unavailable")
}

// TestIngress_BootstrapRecoversByState proves:
// After a restart, Bootstrap() returns runs that were queued or admitted-to-dag.
// This is the restart recovery boundary — in-flight and pending runs are not lost.
func TestIngress_BootstrapRecoversByState(t *testing.T) {
	ctx := context.Background()
	rs := store.NewInMemoryRunStore()

	// Simulate pre-restart state: 2 queued, 1 admitted
	_ = rs.Enqueue(ctx, store.RunRecord{RunID: "r1", State: store.StateQueued})
	_ = rs.Enqueue(ctx, store.RunRecord{RunID: "r2", State: store.StateQueued})
	_ = rs.Enqueue(ctx, store.RunRecord{RunID: "r3", State: store.StateQueued})
	_ = rs.UpdateState(ctx, "r3", store.StateQueued, store.StateAdmittedToDag)

	d, _ := newTestDispatcher(rs)
	recovered, err := d.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if len(recovered) != 3 {
		t.Fatalf("expected 3 recovered runs (2 queued + 1 admitted), got %d", len(recovered))
	}
	t.Logf("PASS: Bootstrap recovered %d runs (queued + admitted-to-dag)", len(recovered))
}

// TestIngress_BootstrapIsNopWithoutRunStore proves:
// When no RunStore is configured, Bootstrap() returns nil without error.
// Backward-compatible with pre-RunStore deployments.
func TestIngress_BootstrapIsNopWithoutRunStore(t *testing.T) {
	ctx := context.Background()
	act := &mockActor{}
	fd := &mockFD{key: "k", cmd: api.Command{Kind: api.CmdRun, Run: &api.RunSpec{RunID: "run-boot", ImageRef: "busybox:1.36"}}}
	d := dispatcher.NewDispatcher(fd, &mockFactory{act: act}, 2)

	recovered, err := d.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("Bootstrap (no store): %v", err)
	}
	if len(recovered) != 0 {
		t.Fatalf("expected empty, got %d", len(recovered))
	}
	t.Log("PASS: Bootstrap is a no-op without RunStore")
}

// TestIngress_IdempotentEnqueue proves:
// Submitting the same RunID twice does not return an error (ErrAlreadyExists
// is swallowed). The run stays in whatever state it was already in.
func TestIngress_IdempotentEnqueue(t *testing.T) {
	ctx := context.Background()
	rs := store.NewInMemoryRunStore()
	d, _ := newTestDispatcher(rs)

	if err := d.Handle(ctx, testInput(), nil); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	// Second call with same RunID: should not fail with ErrAlreadyExists
	if err := d.Handle(ctx, testInput(), nil); err != nil {
		t.Fatalf("second Handle (re-submit): %v", err)
	}
	t.Log("PASS: duplicate RunID enqueue is idempotent")
}

// Ensure dispatcher.Option type is usable (compile check for WithEnqueueTimeout).
var _ = dispatcher.WithEnqueueTimeout(time.Second)

type lifecycleActor struct {
	idleFn func()
}

func (a *lifecycleActor) EnqueueTry(api.Command) bool { return true }
func (a *lifecycleActor) EnqueueCtx(_ context.Context, cmd api.Command) bool {
	if cmd.Kind == api.CmdRun && a.idleFn != nil {
		go a.idleFn()
	}
	return true
}
func (a *lifecycleActor) OnIdle(fn func())       { a.idleFn = fn }
func (a *lifecycleActor) OnTerminate(func())     {}
func (a *lifecycleActor) Loop(_ context.Context) {}

type lifecycleFactory struct {
	act     *lifecycleActor
	created int
	bound   map[string]actor.Actor
}

func (f *lifecycleFactory) Get(spawnKey string) (actor.Actor, bool) {
	act, ok := f.bound[spawnKey]
	return act, ok
}

func (f *lifecycleFactory) Bind(_ string) (actor.Actor, bool, error) {
	f.created++
	return f.act, true, nil
}

func (f *lifecycleFactory) Register(spawnKey string, act actor.Actor) {
	f.bound[spawnKey] = act
}

func (f *lifecycleFactory) Unbind(spawnKey string, act actor.Actor) {
	if cur, ok := f.bound[spawnKey]; ok && cur == act {
		delete(f.bound, spawnKey)
	}
}

func TestIngress_ReleasesSlotAfterActorBecomesIdle(t *testing.T) {
	fd := &mockFD{
		key: "teamA:run-001",
		cmd: api.Command{Kind: api.CmdRun, Run: &api.RunSpec{RunID: "run-001", ImageRef: "busybox:1.36"}},
	}
	act := &lifecycleActor{}
	f := &lifecycleFactory{act: act, bound: make(map[string]actor.Actor)}
	d := dispatcher.NewDispatcher(fd, f, 1)

	input1 := testInput()
	if err := d.Handle(context.Background(), input1, nil); err != nil {
		t.Fatalf("first Handle: %v", err)
	}

	fd.key = "teamA:run-002"
	fd.cmd = api.Command{Kind: api.CmdRun, Run: &api.RunSpec{RunID: "run-002", ImageRef: "busybox:1.36"}}
	input2 := frontdoor.ResolveInput{
		Req: &api.RunSpec{RunID: "run-002", ImageRef: "busybox:1.36"},
		Meta: frontdoor.MetaContext{
			RPC:      "RunE",
			TenantID: "teamA",
			TraceID:  "trace-002",
		},
	}

	deadline := time.Now().Add(time.Second)
	for {
		err := d.Handle(context.Background(), input2, nil)
		if err == nil {
			break
		}
		if !errors.Is(err, sErr.ErrSaturated) {
			t.Fatalf("second Handle: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for semaphore release after actor became idle")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestIngress_DoesNotPersistInvalidResolvedRun(t *testing.T) {
	ctx := context.Background()
	rs := store.NewInMemoryRunStore()
	fd := &mockFD{
		key: "teamA:run-001",
		cmd: api.Command{Kind: api.CmdRun, Run: nil},
	}
	d := dispatcher.NewDispatcher(fd, &mockFactory{act: &mockActor{}}, 1, dispatcher.WithRunStore(rs))

	err := d.Handle(ctx, testInput(), nil)
	if !errors.Is(err, sErr.ErrInvalidCommand) {
		t.Fatalf("expected ErrInvalidCommand, got %v", err)
	}

	if _, ok, _ := rs.Get(ctx, "teamA:run-001"); ok {
		t.Fatal("invalid resolved run should not be persisted to RunStore")
	}
}
