# Spawner Sprint Plan

이 문서는 `spawner`의 원래 제품화 계획을 현재 코드 상태 기준으로 다시 산정한 스프린트 계획이다.

이번 재산정은 단순 문서 정리가 아니라, 실제 저장소 상태를 다시 점검한 결과를 반영한다.

- 최근 반영 사항: lifecycle guard, command validation, replay-safe envelope, fast-fail recovery, policy-driven attempt, run/attempt history
- 아직 남은 핵심: state contract 명문화, duplicate/replay semantics 강화, backend-neutral naming, `MetaContext`/`policy`/`RunSpec` 표면 정리
- 제외 범위: K8s informer 전환, 실제 cluster integration hardening, 운영 배포 자동화

## Recheck Summary

`2026-04-09` 기준 현재 평가는 아래와 같다.

- 실행 경로는 PoC 단계 치고 충분히 단단해졌다.
- 원래 Sprint 1, Sprint 2 목표는 대부분 이미 선반영되었다.
- Sprint 3도 recovery helper 수준을 넘어 replay helper와 attempt 정책까지 일부 앞당겨졌다.
- 따라서 기존 4개 스프린트를 그대로 유지하는 것보다, 남은 공백 중심으로 `3개 잔여 스프린트`로 재패키징하는 편이 맞다.

## Current Status Snapshot

완료 또는 대부분 완료된 항목:

- dispatcher lifecycle release 및 sem 회수
- empty spawn key / invalid command guard
- `Command` 생성자 및 `Validate()`
- `RunSpec.Validate()`
- logical run id / attempt id 분리
- replay-safe `RunEnvelope`
- fast-fail recovery candidate 판정
- `RecoverableRuns()` / replay helper
- attempt policy 도입
- `RunStore` summary + attempt history 구조

아직 남은 핵심 항목:

- persisted state 와 event state 의 canonical mapping 문서/테스트
- duplicate submit / replay / manual requeue semantics를 더 엄격히 고정
- attempt reason/cause 모델 정리
- `k8sAvailable`, `ErrK8sUnavailable` 같은 backend-specific naming 제거
- `MetaContext` copy semantics 또는 immutability 정리
- `policy` validation/defaulting 확장
- `RunSpec` 제품형 필드 확장
- `cmd/server` bootstrap 예제 수준을 실제 서비스 계약 쪽으로 정리

## Planning Assumptions

- 기준일: `2026-04-09`
- 스프린트 길이: `2주`
- 시작 가정일: `2026-04-13`
- 팀 가정: 메인 구현자 1명
- 현재 baseline: core replay/recovery path는 이미 구현되어 있음

## Revised Sprint Summary

| Sprint | Dates | Theme | Primary Goal |
| --- | --- | --- | --- |
| Sprint R1 | 2026-04-13 ~ 2026-04-24 | Recovery Contract Hardening | recovery/state/duplicate semantics를 코드와 문서로 고정 |
| Sprint R2 | 2026-04-27 ~ 2026-05-08 | Library Surface Neutralization | backend-neutral naming, meta/policy contract 정리 |
| Sprint R3 | 2026-05-11 ~ 2026-05-22 | Product Surface Expansion | `RunSpec` 및 예제/문서/API 표면 정리 |

## Sprint R1

기간: `2026-04-13 ~ 2026-04-24`

목표:

- recovery를 "helper"가 아니라 실제 서비스 계약으로 명확히 정의한다.
- persisted state와 event state 관계를 문서/테스트/코드에서 일치시킨다.
- fast-fail 철학을 유지하면서 duplicate/replay/manual requeue를 구분한다.

세부 작업:

- persisted lifecycle 과 transient event lifecycle mapping 문서화
- duplicate submit / recovery replay / manual requeue / auto retry 동작 표 작성
- `AttemptRecord.Reason`을 enum 또는 typed cause 수준으로 격상
- replay 시 schema mismatch / malformed envelope 처리 규칙 고정
- recovery 대상 상태와 비대상 상태 테스트 보강
- operator-visible 의미 기준으로 terminal semantics 정리

산출물:

- 상태 계약 문서
- recovery semantics 회귀 테스트
- typed attempt cause 모델
- duplicate/replay decision table

종료 기준:

- "이 상태는 왜 recoverable 인가"를 문서와 코드가 같은 답으로 설명할 수 있음
- replay, manual requeue, duplicate submit이 서로 다른 경로로 테스트에 고정됨
- attempt history에 남는 reason/cause가 문자열 자유 입력이 아니라 계약으로 설명 가능함

리스크:

- state 통합을 과하게 밀면 persisted state와 transient event가 다시 섞일 수 있음
- duplicate 정책을 너무 일찍 강하게 고정하면 상위 orchestrator 요구와 충돌할 수 있음

## Sprint R2

기간: `2026-04-27 ~ 2026-05-08`

목표:

- dispatcher와 driver 경계에서 K8s 특화 용어를 걷어내고 라이브러리 중립성을 높인다.
- mutable meta bag와 policy surface를 좀 더 안전한 계약으로 바꾼다.

세부 작업:

- `k8sAvailable` -> generic executor/backend availability naming 정리
- `ErrK8sUnavailable` -> generic backend unavailable 계열 에러로 정리
- `MetaContext` copy semantics 또는 불변 사용 규칙 추가
- `policy` validation/defaulting/merge 규칙 추가
- `AttemptPolicy`와 `AdmitPolicy` 사용 예제 정리
- `NopDriver`와 dispatcher hold semantics 문서화

산출물:

- backend-neutral naming 변경
- `MetaContext` 안전 계약
- 강화된 `policy` 테스트 및 문서
- migration note

종료 기준:

- dispatcher가 특정 backend 이름에 덜 묶여 있음
- meta bag 전달 시 호출자 side effect 위험을 문서/코드로 줄임
- policy defaulting과 validation이 테스트에 고정됨

리스크:

- naming 변경은 외부 호출 코드 수정 범위를 넓힐 수 있음
- `MetaContext`를 값 복사로 바꾸면 성능보다 사용성 쪽 영향이 먼저 보일 수 있음

## Sprint R3

기간: `2026-05-11 ~ 2026-05-22`

목표:

- 현재 확보한 core contract 위에 제품형 API 표면을 얹는다.
- `RunSpec` 확장과 예제/문서 정리를 통해 실제 사용 방향을 더 분명히 한다.

세부 작업:

- `RunSpec`에 version/annotations/cleanup/retry correlation 등 최소 확장 필드 검토
- `cmd/server` 예제와 실제 bootstrap/replay 계약 정리
- README 및 bilingual review 문서 업데이트
- `poc` 연동 관점 migration note 작성
- 필요 시 public constructor / helper 재정리

산출물:

- 확장된 `RunSpec`
- 예제/문서 갱신
- migration note
- 제품형 사용 가이드 초안

종료 기준:

- `RunSpec`가 더 이상 PoC payload에만 머물지 않음
- bootstrap/replay 예제가 실제 계약을 오해 없이 보여줌
- sibling `poc`가 새 계약을 따라가기 쉬운 문서가 생김

리스크:

- 표면 확장을 너무 넓히면 core contract가 다시 흐려질 수 있음
- 예제 코드가 실제 서비스 코드처럼 오해될 수 있으므로 문서화가 중요함

## Deferred Items

아래는 중요하지만 이번 잔여 스프린트의 직접 범위에서는 뺀다.

- `DriverK8s.Wait()` polling -> watch/informer 전환
- 실제 Kueue/cluster integration hardening
- 운영 배포/HA/observability pipeline 자동화

## Effort Estimate

단일 구현자 기준 거친 추정:

- Sprint R1: `7 ~ 9` working days
- Sprint R2: `6 ~ 8` working days
- Sprint R3: `6 ~ 8` working days

전체 추정:

- 잔여 집중 구현 기간: `19 ~ 25` working days
- 달력 기준: 약 `6주`

## Suggested Milestones

- `2026-04-24`: recovery/state/duplicate semantics 고정
- `2026-05-08`: backend-neutral naming + policy/meta contract 정리
- `2026-05-22`: `RunSpec`/docs/examples 기준 제품형 표면 1차 정리

## Final Recommendation

현재 `spawner`는 "기초 공사 단계"를 꽤 지난 상태다.

따라서 이제는 원래 계획의 앞 스프린트를 다시 반복하는 게 아니라, 남은 계약 공백을 좁히는 식으로 일정을 다시 잡는 게 맞다.

지금 가장 중요한 것은 새 기능 추가가 아니다.

- recovery semantics를 설명 가능한 계약으로 고정
- duplicate/replay/manual requeue 구분 고정
- backend-specific naming 제거
- policy/meta/API surface를 제품형 경계로 정리

이 네 축이 정리되면, 그 다음에야 informer 전환이나 실제 운영 하드닝 일정이 의미를 갖는다.
