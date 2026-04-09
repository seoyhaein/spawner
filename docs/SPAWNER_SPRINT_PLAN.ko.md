# Spawner Sprint Plan

이 문서는 `spawner`를 PoC 실행 백엔드에서 제품형 라이브러리/서비스 코어로 강화하기 위한 스프린트 계획이다.

기준 입력은 다음 두 가지다.

- `pipeline-lite-poc`가 실제로 검증한 범위
- `spawner` 정적 리뷰에서 확인된 라이브러리 계약, 수명관리, 복구, validation 공백

핵심 해석은 간단하다.

- 실행 백엔드 경로(`DriverK8s`, `BoundedDriver`, `K8sObserver`)는 PoC 기준으로 꽤 괜찮다.
- 반면 `Dispatcher`, `Factory`, `Actor`, `FrontDoor`, `RunStore` 중심의 라이브러리 계약은 아직 느슨하다.
- 따라서 우선순위는 새 기능 확장보다 lifecycle, validation, identity, recovery envelope를 먼저 고정하는 쪽이 맞다.

## Planning Assumptions

- 기준일: `2026-04-09`
- 스프린트 길이: `2주`
- 시작 가정일: `2026-04-13`
- 팀 가정: 메인 구현자 1명 기준
- 포함 범위: `spawner` 자체의 라이브러리/서비스 경계 강화
- 제외 범위: K8s informer 전환, 실제 Kueue/cluster integration hardening, 운영 배포 자동화

## Sprint Summary

| Sprint | Dates | Theme | Primary Goal |
| --- | --- | --- | --- |
| Sprint 1 | 2026-04-13 ~ 2026-04-24 | Lifecycle Stabilization | 실제 고장 가능성이 있는 dispatcher/actor/factory 경계 버그 제거 |
| Sprint 2 | 2026-04-27 ~ 2026-05-08 | Safe Command And Identity | command validation, logical run id, stored envelope 계약 고정 |
| Sprint 3 | 2026-05-11 ~ 2026-05-22 | Recovery And State Contract | recovery API와 persisted/event state mapping 명문화 |
| Sprint 4 | 2026-05-25 ~ 2026-06-05 | Library Surface Hardening | policy, backend-neutral naming, RunSpec/API surface 정리 |

## Sprint 1

기간: `2026-04-13 ~ 2026-04-24`

목표:

- 정상 경로에서 `Dispatcher.Sem`이 회수되지 않는 구조를 정리한다.
- actor unbind / terminate / reuse lifecycle을 코드와 테스트로 고정한다.
- empty spawn key, bind success 확인 누락, blocking sink 누수 가능성 같은 즉시 위험을 줄인다.

세부 작업:

- `Dispatcher`의 bind/register/unbind/release 시점을 명시적으로 분리
- actor 종료 시 `OnTerminate` 기반 정리 경로 도입 또는 현재 계약상 불가능함을 코드로 제한
- `Factory` idle pool 재사용 계약 문서화
- empty spawn key guard 추가
- resolve 결과 검증 추가
- event send timeout 경로에서 goroutine leak이 생기지 않도록 sink adapter 구조 보강
- saturation 없이 연속 run 처리 가능한지 회귀 테스트 추가

산출물:

- lifecycle sequence 문서화
- dispatcher/factory/actor 회귀 테스트 세트
- empty spawn key / invalid resolve result 방어 코드
- event delivery safety wrapper 또는 non-blocking sink adapter

종료 기준:

- 서로 다른 `runID`를 연속 제출해도 정상 종료 후 saturation으로 막히지 않음
- actor 재사용 경로가 테스트로 고정됨
- empty spawn key가 dispatcher 진입 전에 거부됨
- event sink blocking 상황에서 누수 방지 전략이 코드에 반영됨

리스크:

- 현재 actor contract가 termination callback만으로는 충분하지 않을 수 있음
- sem release 타이밍을 잘못 잡으면 중복 실행 또는 premature release 위험이 있음

## Sprint 2

기간: `2026-04-27 ~ 2026-05-08`

목표:

- `Command`와 입력 payload를 타입 안전하게 만든다.
- logical run id, spawn key, attempt identity를 분리한다.
- 저장 payload를 replay 가능한 envelope로 고정한다.

세부 작업:

- `NewRunCommand`, `NewCancelCommand`, `NewSignalCommand` 등 생성자 추가
- `Command.Validate()` 추가
- actor 진입부 nil guard 추가
- `RunSpec.Validate()` 추가
- stable logical run id 규칙 도입
- attempt id 도입
- `RunEnvelope` 또는 유사 구조 도입
- 저장 payload에 `version`, `kind`, `meta`, `spec`, `identity` 포함

산출물:

- typed constructor + validation API
- run/attempt/spawn identity 규약 문서
- replay-safe envelope 타입
- 기존 `json.Marshal(in.Req)` best-effort 저장 제거

종료 기준:

- 잘못된 `Command` 조합이 생성 단계 또는 validate 단계에서 거부됨
- `RunID` 필드가 logical id와 physical id를 혼용하지 않음
- RunStore payload만으로 replay에 필요한 최소 정보가 복원 가능함

리스크:

- public API 변경으로 인해 `poc`와 예제 코드의 수정이 필요할 수 있음
- envelope 버전 필드를 넣는 순간 backward compatibility 전략이 필요해짐

## Sprint 3

기간: `2026-05-11 ~ 2026-05-22`

목표:

- helper 수준의 bootstrap을 실제 recovery API로 올린다.
- persisted state와 event state의 canonical mapping을 정의한다.

세부 작업:

- `Bootstrap()`를 단순 조회가 아니라 replay workflow와 연결
- `Recover()` 또는 `ReplayRecoveredRuns()` 계열 API 설계
- persisted lifecycle과 transient event state를 문서화
- terminal semantics, retry transition, cancel transition 표 정의
- duplicate submit / replay / already-running 재제출 동작 구분
- recovery 시 validation failure와 schema mismatch 처리 규칙 정의

산출물:

- recovery API
- state mapping 문서와 테스트
- duplicate/replay semantics 테스트
- stored envelope version mismatch 처리 전략

종료 기준:

- restart 후 queued/admitted run을 재실행 가능한 표준 흐름이 존재함
- persisted state와 event state 관계가 코드/문서/테스트에서 동일함
- duplicate submit과 replay submit의 동작 차이가 명확함

리스크:

- recovery semantics를 너무 빨리 확정하면 이후 orchestration 상위 계층 요구와 충돌할 수 있음
- admitted 상태의 replay를 어디까지 자동화할지 결정이 필요함

## Sprint 4

기간: `2026-05-25 ~ 2026-06-05`

목표:

- 라이브러리 표면을 제품형 계약에 맞게 정리한다.
- 정책, naming, API surface를 운영 가능한 수준으로 다듬는다.

세부 작업:

- `policy` 패키지의 defaulting/validation/merge semantics 추가
- `k8sAvailable` 같은 backend-specific 이름을 generic executor terminology로 정리
- `RunSpec` 확장
- `MetaContext` immutability 또는 copy semantics 정리
- `EventSink` error-aware/buffered adapter 정리
- docs/examples 업데이트

산출물:

- 강화된 policy package
- backend-neutral naming 정리
- 확장된 `RunSpec`
- 라이브러리 사용 예제와 migration note

종료 기준:

- public API에 validation/defaulting 문서가 붙음
- backend-neutral naming으로 dispatcher가 특정 실행기 이름에 덜 묶임
- 예제 코드가 새 API 계약 기준으로 정리됨

리스크:

- 표면 확장이 과해지면 라이브러리 복잡도가 급격히 증가할 수 있음
- 너무 많은 옵션을 한 번에 넣으면 핵심 contract가 다시 흐려질 수 있음

## Recommended Sequencing Inside Each Sprint

각 스프린트 안에서는 아래 순서를 권장한다.

1. 계약 문서 1차 작성
2. 실패하는 회귀 테스트 먼저 작성
3. 최소 구현으로 테스트 통과
4. naming / docs / examples 정리
5. lint, test, coverage, release note 반영

## Effort Estimate

단일 구현자 기준 거친 추정:

- Sprint 1: `8 ~ 10` working days
- Sprint 2: `8 ~ 10` working days
- Sprint 3: `8 ~ 10` working days
- Sprint 4: `6 ~ 8` working days

전체 추정:

- 집중 구현 기간: `30 ~ 38` working days
- 달력 기준: 약 `8주`

## What Must Not Slip

이 항목들은 뒤로 미루면 안 된다.

- dispatcher normal path의 lifecycle release 문제
- invalid command 조합 방치
- logical run id / attempt id 혼용
- best-effort payload 저장
- empty spawn key 허용

## Suggested Milestones

- `2026-04-24`: lifecycle 버그와 validation P0 정리 완료
- `2026-05-08`: command/identity/envelope 계약 고정
- `2026-05-22`: recovery 흐름과 state mapping 고정
- `2026-06-05`: public library surface hardening 1차 완료

## Final Recommendation

지금 `spawner`는 실행 경로를 더 넓히기 전에 라이브러리 계약을 먼저 단단하게 만들어야 한다.

가장 먼저 해야 할 일은 새 기능 추가가 아니다.

- lifecycle release
- command validation
- identity separation
- replay-safe envelope

이 네 가지를 먼저 고정해야 이후 recovery, policy, surface expansion도 의미가 생긴다.
