# JUMI Priority Plan For `spawner`

> 작성일: 2026-04-18
> 목적: `spawner`를 JUMI의 실행 backend 계층으로 재사용하기 위해 필요한 우선순위를 고정한다.

---

## 1. 현재 판단

`spawner`는 JUMI에서 가장 재사용 가치가 높은 의존 프로젝트다.
특히 아래 구성요소는 JUMI의 초기 실행 경로와 직접 맞닿아 있다.

- `pkg/driver`
- `cmd/imp/k8s_driver.go`
- `cmd/imp/bounded_driver.go`
- `cmd/imp/k8s_observer.go`

반면 JUMI의 현재 원칙과 충돌하거나, 아직 제품 수준으로 부족한 부분도 명확하다.

- 현재 `DriverK8s`는 Kueue 연동 가정이 강하다.
- JUMI는 Kueue optional이어야 한다.
- 현재 `Wait()`는 polling 기반이다.
- run/node/attempt 의미론은 JUMI 기준으로 더 분리되어야 한다.

이 문서는 그 차이를 메우기 위한 실제 우선순위를 제안한다.

---

## 2. 최우선 원칙

`spawner`가 JUMI와 함께 가기 위해 먼저 만족해야 하는 원칙은 아래와 같다.

- Kubernetes native execution이 기본 경로여야 한다.
- Kueue는 optional integration이어야 한다.
- Kueue가 없어도 `Prepare -> Start -> Wait -> Cancel`이 정상 동작해야 한다.
- driver contract는 logical run과 physical job/attempt를 구분할 수 있어야 한다.
- backend 상태와 관찰 신호는 JUMI가 이벤트/상태 모델로 올리기 쉽게 정리되어야 한다.

---

## 3. 우선순위

### Priority 0. Kueue optional화

목표:

- `spawner`가 Kueue 없이도 정상 실행되도록 만든다.

해야 할 일:

- `DriverK8s`의 `suspend=true` 고정 제거 또는 옵션화
- queue-name label 주입을 optional path로 이동
- Kueue 없는 Job 생성 경로 테스트 추가
- Kueue integration failure와 core backend failure를 분리

완료 기준:

- Kueue 없는 클러스터에서 Job이 정상 생성/실행/완료된다.
- Kueue가 있는 경우만 admission 관련 동작이 활성화된다.

이 단계가 먼저여야 하는 이유:

- JUMI의 핵심 원칙과 직접 충돌하는 부분이기 때문이다.

### Priority 1. Backend contract 정리

목표:

- `driver.Driver`를 JUMI의 run/node/attempt 모델이 감쌀 수 있는 안정된 execution boundary로 만든다.

해야 할 일:

- logical run identity와 physical job identity 분리
- handle/prepared 타입에 attempt 관점 정보 추가 검토
- `RunSpec`에서 K8s-specific field와 실행 의미 field를 분리할 준비
- backend error 분류를 prepare/start/wait/cancel 기준으로 정리

완료 기준:

- JUMI가 `spawner`를 backend adapter로 사용할 때 identity mismatch 없이 감쌀 수 있다.
- JUMI 이벤트 모델이 backend phase를 명확히 매핑할 수 있다.

### Priority 2. Observation hardening

목표:

- JUMI가 운영 상태를 더 정확히 읽을 수 있게 한다.

해야 할 일:

- `Wait()` polling에서 watch/informer 기반으로 전환
- Job/Pod 상태 관찰의 세밀도 향상
- Kueue optional 조건에서 observer path 분기 정리
- scheduler pending과 running failure 구분을 더 명확히 유지

완료 기준:

- polling 의존이 줄고, 상태 반영 지연과 race가 완화된다.
- JUMI가 stop cause를 backend 신호에 더 안정적으로 매핑할 수 있다.

### Priority 3. Cancel / recovery semantics 정리

목표:

- JUMI의 cancel/recovery 의미론을 backend가 더 잘 받쳐주도록 만든다.

해야 할 일:

- cancel race에 대한 일관된 결과 반환 규칙 정리
- bootstrap/recovery를 주석이 아니라 실제 코드 계약으로 정리
- duplicate submit / replay / idempotency에 대한 기준선 보강

완료 기준:

- JUMI가 cancel과 restart recovery를 backend 위에서 예측 가능하게 처리할 수 있다.

### Priority 4. Dispatcher / Actor 경로 정리

목표:

- `spawner` 내부 실험 경로와 JUMI 기준 backend 경로를 구분한다.

해야 할 일:

- Dispatcher/Actor/FrontDoor의 역할을 JUMI 기준에서 재정리
- JUMI가 직접 쓰지 않을 실험 경로는 experimental 성격을 더 명확히 표시
- `pkg/driver` 중심 경계와 `cmd/server` 실험 경계를 분리

완료 기준:

- JUMI 개발자가 `spawner`에서 무엇을 재사용해야 하는지 혼동하지 않는다.

---

## 4. JUMI 기준 사용 계획

JUMI가 현재 `spawner`에서 우선적으로 의존해야 하는 부분은 아래와 같다.

- `pkg/driver`
- `cmd/imp/k8s_driver.go`
- `cmd/imp/bounded_driver.go`
- `cmd/imp/k8s_observer.go`

JUMI가 바로 기준선으로 삼지 말아야 할 부분은 아래와 같다.

- Dispatcher / Actor / FrontDoor를 포함한 ingress orchestration 실험 경로
- durable-lite store를 제품 저장소로 간주하는 usage

---

## 5. 즉시 실행 순서

1. `DriverK8s`에서 Kueue optional화
2. no-Kueue 테스트 추가
3. backend contract에서 identity와 error phase 정리
4. `Wait()` hardening
5. cancel/recovery semantics 보강

---

## 6. 한 줄 결론

`spawner`는 JUMI의 가장 중요한 backend 후보지만, 가장 먼저 해결해야 할 과제는 Kueue optional화와 backend contract 정리다.
