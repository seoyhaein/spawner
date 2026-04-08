package main

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/seoyhaein/spawner/cmd/imp"
	"github.com/seoyhaein/spawner/pkg/actor"
	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/dispatcher"
	"github.com/seoyhaein/spawner/pkg/driver"
	sErr "github.com/seoyhaein/spawner/pkg/error"
	"github.com/seoyhaein/spawner/pkg/factory"
	fdr "github.com/seoyhaein/spawner/pkg/frontdoor"
	ply "github.com/seoyhaein/spawner/pkg/policy"
	"github.com/seoyhaein/spawner/pkg/store"
)

func runRule() fdr.Rule {
	return fdr.Rule{
		Match: func(in fdr.ResolveInput) bool { return in.Meta.RPC == "RunE" },
		SpawnKeyFn: func(in fdr.ResolveInput) string {
			rs := in.Req.(*api.RunSpec)
			return in.Meta.TenantID + ":" + rs.RunID
		},
		BuildCmd: func(in fdr.ResolveInput) (api.Command, error) {
			rs := in.Req.(*api.RunSpec)
			return api.Command{
				Kind:   api.CmdRun,
				Run:    rs,
				Policy: ply.DefaultPolicyB(5 * time.Minute),
			}, nil
		},
	}
}

func main() {
	rootCtx := context.Background()

	// ── RunStore: durable-lite JsonRunStore for restart recovery ──────────────
	// Queued/admitted runs survive process restart.
	// ASSUMPTION: production replaces with PostgreSQL/Redis.
	storePath := os.Getenv("RUN_STORE_PATH")
	if storePath == "" {
		storePath = "/tmp/spawner-runstore.json"
	}
	rs, err := store.NewJsonRunStore(storePath)
	if err != nil {
		log.Fatalf("runstore init: %v", err)
	}
	log.Printf("[server] RunStore: %s", storePath)

	// ── K8s driver: graceful init — no panic on failure ───────────────────────
	// If K8s is unreachable, NopDriver is used and Dispatcher marks k8sAvailable=false.
	// Incoming runs are held in the RunStore rather than dispatched to Actor.
	// No K8s API call is attempted against an unavailable cluster.
	//
	// ASSUMPTION: a health-check loop (not shown here) calls d.SetK8sAvailable(true)
	// and d.Bootstrap() once connectivity is restored.
	var drvFn factory.DriverMaker
	dispOpts := []dispatcher.Option{
		dispatcher.WithRunStore(rs),
	}

	drv, k8sErr := imp.NewK8sFromKubeconfig("default", "")
	if k8sErr != nil {
		log.Printf("[server] WARN: K8s init failed (%v) — NopDriver active; runs will be held", k8sErr)
		drvFn = func(_ string) driver.Driver { return &imp.NopDriver{} }
		dispOpts = append(dispOpts, dispatcher.WithK8sUnavailable())
	} else {
		log.Printf("[server] K8s connected")
		drvFn = func(_ string) driver.Driver { return drv }
	}

	r := fdr.NewTableFrontDoor(runRule())

	af := factory.NewFactory(
		drvFn,
		func(key string, d driver.Driver, mb int) actor.Actor {
			return imp.NewK8sActor(key, d, mb)
		},
		128,
	)

	d := dispatcher.NewDispatcher(r, af, 2, dispOpts...)

	// ── Bootstrap: recover runs from previous process instance ────────────────
	recovered, bootstrapErr := d.Bootstrap(rootCtx)
	if bootstrapErr != nil {
		log.Printf("[server] bootstrap error: %v", bootstrapErr)
	} else if len(recovered) > 0 {
		log.Printf("[server] bootstrap: %d run(s) pending re-dispatch", len(recovered))
		// ASSUMPTION: re-dispatch requires deserializing RunRecord.Payload into
		// api.RunSpec and calling d.Handle(). Omitted here; callers are
		// responsible for this loop in production.
	}

	// ── Example submission ────────────────────────────────────────────────────
	in := fdr.ResolveInput{
		Req: &api.RunSpec{
			RunID:    "run-001",
			ImageRef: "ghcr.io/acme/tool@sha256:deadbeef...",
			Env:      map[string]string{"SAMPLE_ID": "HG001"},
			Mounts: []api.Mount{
				{Source: "/data/HG001", Target: "/in", ReadOnly: true},
				{Source: "workvol", Target: "/work", ReadOnly: false},
			},
			Resources: api.Resources{CPU: "2", Memory: "4Gi"},
		},
		Meta: fdr.MetaContext{
			RPC:       "RunE",
			TenantID:  "teamA",
			Principal: "alice",
			TraceID:   "trace-xyz",
		},
	}

	if err := d.Handle(rootCtx, in, nil); err != nil {
		if errors.Is(err, sErr.ErrK8sUnavailable) {
			log.Printf("[server] run-001 → held (k8s unavailable; preserved in RunStore)")
		} else {
			log.Printf("[server] Handle error: %v", err)
		}
	} else {
		log.Printf("[server] run-001 → admitted-to-dag")
	}
}
