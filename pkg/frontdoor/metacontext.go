package frontdoor

import (
	"strings"
	"time"
)

// MetaContext carries transport and routing metadata.
//
// Contract:
// callers should treat MetaContext as immutable once attached to ResolveInput.
// Internal code that needs to mutate bag values should work on a cloned copy.
type MetaContext struct {
	RPC       string // "RunE", "Cancel", ...
	TenantID  string
	Principal string // user/service id
	TraceID   string

	// 전송 무관 K/V 저장소 (HTTP 헤더, gRPC metadata, 플래그 등을 통일해서 담기)
	Bag map[string]string

	// 선택: 있으면 편한 것들
	RequestID  string    // 요청 식별 (로그 상관관계)
	Deadline   time.Time // 클라이언트 데드라인(있으면)
	Transport  string    // "http"|"grpc"|"mq" 등 (감사·디버그용)
	ReceivedAt time.Time // 서버가 받은 시각 (옵션)
}

func (m MetaContext) Clone() MetaContext {
	cp := m
	if len(m.Bag) > 0 {
		cp.Bag = make(map[string]string, len(m.Bag))
		for k, v := range m.Bag {
			cp.Bag[strings.ToLower(k)] = v
		}
	}
	return cp
}

func (m *MetaContext) Get(k string) (string, bool) {
	if m == nil || m.Bag == nil {
		return "", false
	}
	v, ok := m.Bag[strings.ToLower(k)]
	return v, ok
}

func (m *MetaContext) Set(k, v string) {
	if m.Bag == nil {
		m.Bag = make(map[string]string, 8)
	}
	m.Bag[strings.ToLower(k)] = v
}
