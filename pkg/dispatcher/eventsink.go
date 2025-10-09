package dispatcher

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/seoyhaein/spawner/pkg/api"
)

type NoopSink struct{}

func (NoopSink) Send(api.Event) {}

type PrintSink struct{ w io.Writer }

func NewPrintSink(w io.Writer) PrintSink {
	if w == nil {
		w = os.Stdout // 기본 출력으로 폴백
	}
	return PrintSink{w: w}
}

func (s PrintSink) Send(ev api.Event) {
	if s.w == nil {
		return // 안전 가드
	}
	_, _ = fmt.Fprintf( // 리턴값은 무시 (원하면 로그 처리)
		s.w,
		"[%s] spawn=%s run=%s state=%s msg=%s\n",
		ev.When.Format(time.RFC3339),
		ev.SpawnKey,
		ev.RunID,
		ev.State,
		ev.Message,
	)
}

type MultiSink []api.EventSink

func (m MultiSink) Send(ev api.Event) {
	for _, s := range m {
		if s != nil {
			s.Send(ev)
		}
	}
}
