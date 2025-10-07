package frontdoor

import (
	"context"
	"fmt"
	"time"

	"github.com/seoyhaein/spawner/pkg/api"
	sErr "github.com/seoyhaein/spawner/pkg/error"
	ply "github.com/seoyhaein/spawner/pkg/policy"
)

type ResolveInput struct {
	Req  any
	Meta MetaContext
}

type ResolveResult struct {
	SpawnKey string
	Cmd      api.Command
}

type FrontDoor interface {
	// Resolve 여기서 SpawnKey 생성해서, 단일 actor 로 요청이 가도록 해야한다.
	// 여기서 단일 actor 란, 동일한 SpawnKey 로 라우팅 되는 actor 를 의미하는데, 그 이유는 여러 작업들이 일어날 수 있고, 여러 사용자가 있을 수 있기 때문이다.
	Resolve(ctx context.Context, in ResolveInput) (ResolveResult, error)
}

type TableFrontDoor struct{ rules []Rule }

func NewTableFrontDoor(rules ...Rule) *TableFrontDoor {
	return &TableFrontDoor{rules: rules}
}

// Resolve TODO 적용 규칙에 대해서는 생각해줘야 함.

func (fd *TableFrontDoor) Resolve(ctx context.Context, in ResolveInput) (ResolveResult, error) {
	for _, rl := range fd.rules {
		if rl.Match(in) {
			cmd, err := rl.BuildCmd(in)
			if err != nil {
				return ResolveResult{}, err
			}
			if cmd.Policy.Timeout == 0 {
				cmd.Policy = ply.DefaultPolicyB(30 * time.Minute) // TODO cmd 에 넣은 것은 생각을 해주자.
			}
			return ResolveResult{
				SpawnKey: rl.SpawnKeyFn(in),
				Cmd:      cmd,
			}, nil
		}
	}
	return ResolveResult{}, fmt.Errorf("%w: rpc=%s", sErr.ErrNoMatch, in.Meta.RPC)
}
