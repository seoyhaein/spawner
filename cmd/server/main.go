package main

import (
	"context"
	"time"

	"github.com/seoyhaein/spawner/cmd/imp"
	"github.com/seoyhaein/spawner/pkg/actor"
	"github.com/seoyhaein/spawner/pkg/api"
	"github.com/seoyhaein/spawner/pkg/dispatcher"
	"github.com/seoyhaein/spawner/pkg/driver"
	"github.com/seoyhaein/spawner/pkg/factory"
	fdr "github.com/seoyhaein/spawner/pkg/frontdoor"
	ply "github.com/seoyhaein/spawner/pkg/policy"
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

	r := fdr.NewTableFrontDoor(runRule())

	// 드라이버/액터 생성자 둘 다 주입
	af := factory.NewFactory(
		rootCtx,
		func(key string) driver.Driver {
			return imp.NewK8s("default") // DriverMaker
		},
		func(key string, d driver.Driver, mb int) actor.Actor {
			return imp.NewSpawnActor(key, d, mb) // ActorMaker
		},
		128,
	)

	// d := dispatcher.New(r, af, 2)
	d := dispatcher.NewDispatcher(r, af, 2)
	// d := dispatcher.NewDispatcher(r, af, 2, dispatcher.WithDefaultSink(mySink))

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
		panic(err)
	}
	time.Sleep(1 * time.Second)
}
