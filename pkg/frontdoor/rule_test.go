package frontdoor_test

import (
	"testing"

	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/frontdoor"
)

func TestPredicates_ComposeAsExpected(t *testing.T) {
	in := frontdoor.ResolveInput{
		Req: &api.RunSpec{RunID: "run-1"},
		Meta: frontdoor.MetaContext{
			RPC:       "RunE",
			TenantID:  "team-a",
			Principal: "user/alice",
		},
	}
	in.Meta.Set("hdr.x-cmd", "Run")

	if !frontdoor.And(
		frontdoor.HasRPC("RunE"),
		frontdoor.HasTenant(),
		frontdoor.TenantIn("team-a", "team-b"),
		frontdoor.HasReqType[api.RunSpec](),
		frontdoor.NotSystemPrincipal(),
		frontdoor.MetaEq("hdr.x-cmd", "Run"),
	)(in) {
		t.Fatal("expected composed predicate to match")
	}

	if frontdoor.Or(
		frontdoor.HasRPC("Cancel"),
		frontdoor.TenantIn("team-x"),
	)(in) {
		t.Fatal("expected Or predicate to fail")
	}

	if !frontdoor.Not(frontdoor.HasRPC("Cancel"))(in) {
		t.Fatal("expected Not predicate to invert the result")
	}
}

func TestCancelRule_PrefersSpawnIDThenIdemKey(t *testing.T) {
	withSpawnID := frontdoor.ResolveInput{
		Req:  &api.CancelReq{SpawnID: "spawn-123", RunID: "run-1"},
		Meta: frontdoor.MetaContext{RPC: "Cancel", TenantID: "team-a"},
	}
	if !frontdoor.CancelRule.Match(withSpawnID) {
		t.Fatal("expected cancel rule to match Cancel RPC")
	}
	if got := frontdoor.CancelRule.SpawnKeyFn(withSpawnID); got != "spawn-123" {
		t.Fatalf("expected SpawnID to win, got %q", got)
	}

	cmd, err := frontdoor.CancelRule.BuildCmd(withSpawnID)
	if err != nil {
		t.Fatalf("BuildCmd: %v", err)
	}
	if cmd.Kind != api.CmdCancel {
		t.Fatalf("expected CmdCancel, got %v", cmd.Kind)
	}
	if cmd.Cancel == nil || cmd.Cancel.RunID != "run-1" {
		t.Fatal("expected CancelReq to be preserved in the command")
	}

	withIdemKey := frontdoor.ResolveInput{
		Req:  &api.CancelReq{IdemKey: "idem-1"},
		Meta: frontdoor.MetaContext{RPC: "Cancel", TenantID: "team-a"},
	}
	if got := frontdoor.CancelRule.SpawnKeyFn(withIdemKey); got != "" {
		t.Fatalf("expected unresolved idemKey lookup to return empty string, got %q", got)
	}
}
