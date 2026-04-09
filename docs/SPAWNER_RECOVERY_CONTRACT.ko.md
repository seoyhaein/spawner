# Spawner Recovery Contract

이 문서는 현재 `spawner`의 recovery, replay, duplicate submit 계약을 짧게 고정한다.

## Core Model

- `LogicalRunID`: 논리 run 식별자
- `AttemptID`: 실제 실행 시도 식별자
- `RunStore`: `RunRecord` summary + `AttemptRecord` history를 함께 저장

## Persisted State

저장 상태는 아래 의미를 가진다.

| State | Meaning | Recoverable |
| --- | --- | --- |
| `queued` | 요청은 받았지만 아직 실행 경계에 완전히 들어가지 않음 | yes |
| `held` | backend unavailable 또는 정책상 보류 | no |
| `resumed` | hold 해제 후 재진입 대기 | no |
| `admitted-to-dag` | dispatcher/actor 경계로 넘겨짐 | yes |
| `running` | 실행 중 | no |
| `finished` | 정상 종료 또는 완료 terminal | no |
| `canceled` | 취소 terminal | no |

현재 fast-fail 기본 정책상 자동 recovery 대상은 `queued`, `admitted-to-dag` 뿐이다.

## Event State

이벤트 상태는 operator-facing transient 상태다.

- `queued`
- `starting`
- `running`
- `cancelling`
- `cancelled`
- `succeeded`
- `failed`

이 상태는 persisted state와 1:1 대응이 아니다. persisted state는 recovery와 durable gate 의미를, event state는 관측 의미를 담당한다.

현재 canonical projection은 아래처럼 본다.

| Persisted State | Primary Event |
| --- | --- |
| `queued` | `queued` |
| `held` | none |
| `resumed` | none |
| `admitted-to-dag` | `starting` |
| `running` | `running` |
| `finished` | `succeeded` |
| `canceled` | `cancelled` |

`held`, `resumed`는 durable control state라서 직접 event로 투영하지 않는다.

## Attempt Cause

`AttemptRecord`는 자유 문자열 reason 대신 typed cause를 사용한다.

- `initial-submit`
- `recovery-replay`
- `manual-requeue`
- `auto-retry`

## Decision Table

| Scenario | New Attempt | Attempt Cause | Notes |
| --- | --- | --- | --- |
| first submit | no | `initial-submit` | 최초 attempt 생성 |
| duplicate submit with same attempt id | no | unchanged | idempotent no-op |
| recovery replay | policy-driven | `recovery-replay` when new attempt is created | default policy keeps current attempt |
| manual requeue | policy-driven | `manual-requeue` when new attempt is created | default policy allocates new attempt |
| auto retry | policy-driven | `auto-retry` when new attempt is created | default policy allocates new attempt |

## Current Defaults

- recovery replay: 기존 attempt 유지
- manual requeue: 새 attempt 생성
- auto retry: 새 attempt 생성

즉 현재 기본 철학은 "fast-fail 실행"과 "recovery-safe 저장"을 분리한다.

- 실패한 run을 자동 재시도하지는 않는다
- 하지만 애매한 중간 상태는 유실하지 않고 replay 가능하게 둔다
