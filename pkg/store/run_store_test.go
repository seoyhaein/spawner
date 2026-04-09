package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/seoyhaein/spawner/pkg/store"
)

// ── state machine ─────────────────────────────────────────────────────────────

func TestValidateTransition_Valid(t *testing.T) {
	cases := [][2]store.RunState{
		{store.StateQueued, store.StateAdmittedToDag},
		{store.StateQueued, store.StateHeld},
		{store.StateQueued, store.StateCanceled},
		{store.StateHeld, store.StateResumed},
		{store.StateHeld, store.StateCanceled},
		{store.StateResumed, store.StateAdmittedToDag},
		{store.StateAdmittedToDag, store.StateRunning},
		{store.StateAdmittedToDag, store.StateCanceled},
		{store.StateRunning, store.StateFinished},
		{store.StateRunning, store.StateCanceled},
	}
	for _, c := range cases {
		if err := store.ValidateTransition(c[0], c[1]); err != nil {
			t.Errorf("expected valid %s→%s: %v", c[0], c[1], err)
		}
	}
}

func TestValidateTransition_Invalid(t *testing.T) {
	cases := [][2]store.RunState{
		{store.StateFinished, store.StateQueued},
		{store.StateCanceled, store.StateRunning},
		{store.StateRunning, store.StateQueued},
		{store.StateAdmittedToDag, store.StateHeld},
		{store.StateRunning, store.StateAdmittedToDag},
	}
	for _, c := range cases {
		if err := store.ValidateTransition(c[0], c[1]); err == nil {
			t.Errorf("expected invalid %s→%s to be rejected", c[0], c[1])
		}
	}
}

func TestIsTerminal(t *testing.T) {
	cases := []struct {
		state store.RunState
		want  bool
	}{
		{state: store.StateQueued, want: false},
		{state: store.StateHeld, want: false},
		{state: store.StateRunning, want: false},
		{state: store.StateFinished, want: true},
		{state: store.StateCanceled, want: true},
	}

	for _, tc := range cases {
		if got := store.IsTerminal(tc.state); got != tc.want {
			t.Fatalf("IsTerminal(%q) = %v, want %v", tc.state, got, tc.want)
		}
	}
}

func TestIsRecoverable(t *testing.T) {
	cases := []struct {
		state store.RunState
		want  bool
	}{
		{state: store.StateQueued, want: true},
		{state: store.StateAdmittedToDag, want: true},
		{state: store.StateHeld, want: false},
		{state: store.StateFinished, want: false},
		{state: store.StateCanceled, want: false},
	}

	for _, tc := range cases {
		if got := store.IsRecoverable(tc.state); got != tc.want {
			t.Fatalf("IsRecoverable(%q) = %v, want %v", tc.state, got, tc.want)
		}
	}
}

// ── InMemoryRunStore ──────────────────────────────────────────────────────────

// TestMemoryStore_DoesNotSurviveReset proves that a new InMemoryRunStore
// instance loses runs from a previous instance (simulating restart).
func TestMemoryStore_DoesNotSurviveReset(t *testing.T) {
	ctx := context.Background()
	s1 := store.NewInMemoryRunStore()
	_ = s1.Enqueue(ctx, store.RunRecord{RunID: "run-1", State: store.StateQueued})
	_ = s1.Enqueue(ctx, store.RunRecord{RunID: "run-2", State: store.StateQueued})

	s2 := store.NewInMemoryRunStore()
	recs, _ := s2.ListByState(ctx, store.StateQueued)
	if len(recs) != 0 {
		t.Fatalf("expected 0 after reset, got %d", len(recs))
	}
	t.Logf("OBSERVATION: InMemoryRunStore lost 2 queued runs on reset")
}

// TestMemoryStore_HeldOnK8sUnavailable proves the queued→held transition:
// when K8s is unavailable, runs transition to held instead of being dispatched.
func TestMemoryStore_HeldOnK8sUnavailable(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryRunStore()
	_ = s.Enqueue(ctx, store.RunRecord{RunID: "run-1", State: store.StateQueued})

	// Simulate k8s unavailable: transition queued → held
	if err := s.UpdateState(ctx, "run-1", store.StateQueued, store.StateHeld); err != nil {
		t.Fatalf("queued→held rejected: %v", err)
	}
	rec, ok, _ := s.Get(ctx, "run-1")
	if !ok || rec.State != store.StateHeld {
		t.Fatalf("expected held, got %v", rec.State)
	}
	t.Logf("PASS: run stays held when k8s unavailable (not lost, not admitted)")
}

// TestMemoryStore_StateTransition proves UpdateState enforces state machine policy.
func TestMemoryStore_StateTransition(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryRunStore()
	_ = s.Enqueue(ctx, store.RunRecord{RunID: "run-1", State: store.StateQueued})

	if err := s.UpdateState(ctx, "run-1", store.StateQueued, store.StateAdmittedToDag); err != nil {
		t.Fatalf("valid transition rejected: %v", err)
	}
	err := s.UpdateState(ctx, "run-1", store.StateAdmittedToDag, store.StateQueued)
	if err == nil {
		t.Fatal("invalid backward transition was accepted")
	}
	t.Logf("PASS: invalid backward transition rejected: %v", err)
}

func TestMemoryStore_ListByStateAndDelete(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryRunStore()

	for _, rec := range []store.RunRecord{
		{RunID: "queued-1", State: store.StateQueued},
		{RunID: "queued-2", State: store.StateQueued},
		{RunID: "held-1", State: store.StateHeld},
	} {
		if err := s.Enqueue(ctx, rec); err != nil {
			t.Fatalf("Enqueue(%s): %v", rec.RunID, err)
		}
	}

	queued, err := s.ListByState(ctx, store.StateQueued)
	if err != nil {
		t.Fatalf("ListByState: %v", err)
	}
	if len(queued) != 2 {
		t.Fatalf("expected 2 queued runs, got %d", len(queued))
	}

	if err := s.Delete(ctx, "queued-1"); err != nil {
		t.Fatalf("Delete existing run: %v", err)
	}
	if err := s.Delete(ctx, "queued-1"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound on repeated delete, got %v", err)
	}
	if _, ok, _ := s.Get(ctx, "queued-1"); ok {
		t.Fatal("deleted run still present in store")
	}
}

// ── JsonRunStore ──────────────────────────────────────────────────────────────

// TestJsonStore_RecoveryAfterRestart proves that JsonRunStore survives
// process restart (simulated by opening the same file twice).
func TestJsonStore_RecoveryAfterRestart(t *testing.T) {
	ctx := context.Background()
	f, _ := os.CreateTemp(t.TempDir(), "runstore-*.json")
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}

	s1, _ := store.NewJsonRunStore(path)
	_ = s1.Enqueue(ctx, store.RunRecord{RunID: "run-A", State: store.StateQueued})
	_ = s1.Enqueue(ctx, store.RunRecord{RunID: "run-B", State: store.StateQueued})
	_ = s1.UpdateState(ctx, "run-A", store.StateQueued, store.StateAdmittedToDag)

	s2, _ := store.NewJsonRunStore(path)
	queued, _ := s2.ListByState(ctx, store.StateQueued)
	admitted, _ := s2.ListByState(ctx, store.StateAdmittedToDag)

	if len(queued) != 1 {
		t.Fatalf("expected 1 queued after restart, got %d", len(queued))
	}
	if len(admitted) != 1 {
		t.Fatalf("expected 1 admitted after restart, got %d", len(admitted))
	}
	t.Logf("OBSERVATION: JsonRunStore recovered %d queued + %d admitted after restart",
		len(queued), len(admitted))
}
