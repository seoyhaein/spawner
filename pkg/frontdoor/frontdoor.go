package frontdoor

import (
	"context"
	"fmt"
	"time"

	"github.com/seoyhaein/spawner/api"
	sErr "github.com/seoyhaein/spawner/error"
)

// MetaContext 같은 경우, SpawnKey 생성에 필요한 정보들을 담고 있을 수 있음.
// 따라서 향후 수정될 수도 있음.
type MetaContext struct {
	RPC       string // "RunE", "Cancel", ...
	TenantID  string
	Principal string // user/service id
	TraceID   string
}

type ResolveInput struct {
	Req  any
	Meta MetaContext
}

type ResolveResult struct {
	SpawnKey string
	Cmd      api.Command
}

type AdmissionRule struct {
	Match        func(ResolveInput) bool
	KeyOf        func(ResolveInput) string
	BuildCommand func(ResolveInput) (api.Command, error)
}

type FrontDoor interface {
	// Resolve 여기서 SpawnKey 생성해서, 단일 actor 로 요청이 가도록 해야한다.
	// 여기서 단일 actor 란, 동일한 SpawnKey 로 라우팅 되는 actor 를 의미하는데, 그 이유는 여러 작업들이 일어날 수 있고, 여러 사용자가 있을 수 있기 때문이다.
	Resolve(ctx context.Context, in ResolveInput) (ResolveResult, error)
}

func DefaultPolicy(timeout time.Duration) api.AdmitPolicy {
	var rp api.AdmitPolicy
	rp.Timeout = timeout
	rp.Retry.Max = 0
	rp.Retry.Backoff = 0
	return rp
}

// TODO 수정하자.

/*type AdmissionRule struct {
	Match        func(ResolveInput) bool
	KeyOf        func(ResolveInput) SpawnKey
	BuildCommand func(ResolveInput) (api.Command, error)
}*/

type TableFrontDoor struct{ rules []AdmissionRule }

func NewTableFrontDoor(rules ...AdmissionRule) *TableFrontDoor {
	return &TableFrontDoor{rules: rules}
}

func (fd *TableFrontDoor) Resolve(ctx context.Context, in ResolveInput) (ResolveResult, error) {
	for _, rl := range fd.rules {
		if rl.Match(in) {
			cmd, err := rl.BuildCommand(in)
			if err != nil {
				return ResolveResult{}, err
			}
			if cmd.Policy.Timeout == 0 {
				cmd.Policy = DefaultPolicy(30 * time.Minute)
			}
			return ResolveResult{
				SpawnKey: rl.KeyOf(in),
				Cmd:      cmd,
			}, nil
		}
	}
	return ResolveResult{}, fmt.Errorf("%w: rpc=%s", sErr.ErrNoMatch, in.Meta.RPC)
}
