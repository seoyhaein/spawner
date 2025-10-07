package frontdoor

import (
	"strings"
	"time"
)

// MetaContext 같은 경우, SpawnKey 생성에 필요한 정보들을 담고 있을 수 있음.
// 따라서 향후 수정될 수도 있음.
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

// 편의 메서드 TODO 동시성 이슈가 있을지 확인하자.

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
