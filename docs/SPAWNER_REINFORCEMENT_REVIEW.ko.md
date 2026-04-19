# Spawner 보강 리뷰

> 작성일: 2026-04-08  
> 기준: sibling `poc` 프로젝트에서 `spawner`를 실제 실행 백엔드로 사용한 결과를 기준으로 검토

## 1. 요약

`spawner`는 현재 상태로도 PoC 실행 백엔드로서 충분한 가치가 있다.
특히 아래 영역은 이미 꽤 좋은 구조를 가지고 있다.

- `driver.Driver` 인터페이스를 기준으로 실행 백엔드를 분리한 점
- `DriverK8s`로 Kubernetes Job 제출/대기/취소를 단순하게 감싼 점
- `BoundedDriver`로 Job 생성 burst를 제한한 점
- `K8sObserver`로 Kueue pending 과 scheduler unschedulable 을 구분하려 한 점
- `Dispatcher + RunStore`로 ingress boundary 를 세우기 시작한 점

반면 제품화 관점에서는 아직 "좋은 PoC backend"에 가깝고,
"복구 가능한 실행 서비스"로 보기에는 몇 가지 핵심 축이 부족하다.

핵심 보강 포인트는 아래 다섯 가지다.

1. `Wait()`를 polling 에서 watch/informer 기반으로 전환
2. logical run id 와 physical job/attempt id 분리
3. duplicate submit / retry / recovery 의미론 강화
4. `RunSpec` 확장
5. 관측 모델과 bootstrap/recovery 실제 구현

## 2. 현재 강점

### 2.1 Driver 경계가 작고 명확함

`pkg/driver/driver.go`의 인터페이스는 작고 이해하기 쉽다.

- `Prepare`
- `Start`
- `Wait`
- `Signal`
- `Cancel`

이 경계는 `poc`에서 `SpawnerNode`를 통해 dag-go 와 붙이기 쉬웠고,
테스트 대체도 간단했다.

### 2.2 BoundedDriver는 실제 가치가 있음

`cmd/imp/bounded_driver.go`는 단순하지만 효과적이다.
`poc`에서 fan-out 이나 multi-run burst 상황에서 K8s API create burst를 제한하는 용도로 잘 맞았다.

즉, 이 컴포넌트는 "실험용 코드"가 아니라 제품에서도 살아남을 가능성이 높다.

### 2.3 K8sObserver 방향은 맞음

`cmd/imp/k8s_observer.go`는 아래 두 상태를 분리하려는 시도가 좋다.

- Kueue pending
- kube-scheduler unschedulable

운영자 입장에서는 이 둘을 반드시 구분해야 하므로, 방향 자체는 맞다.

## 3. 제품화 전에 보강이 필요한 부분

### 3.1 P0: Wait() polling 구조 교체

현재 `cmd/imp/k8s_driver.go`의 `Wait()`는 2초 polling 기반이다.

문제:

- Job 수가 늘면 API server read 부하가 커진다.
- 상태 전환 감지가 느리다.
- 실행 완료만 볼 뿐, admitted/running/pending/unschedulable 같은 중간 상태를 자연스럽게 surface 하지 못한다.

왜 중요한가:

`poc`는 이미 timeout, pending, fast-fail, burst control, scheduler block 까지 다루고 있다.
즉, `spawner`가 제품화되면 단순 성공/실패 대기 이상의 상태 모델이 필요하다.

권장 방향:

- `Job` watch
- 필요 시 `Pod` watch
- 필요 시 `Workload` watch
- 공통 event stream 으로 통합

권장 결과:

- `Wait()`는 최종 상태만 반환하더라도 내부는 watch 기반으로 변경
- 별도 상태 스트림 또는 callback/event sink로 running/admitted/pending 정보 노출

### 3.2 P0: Run ID 와 Job/Attempt ID 분리

현재는 `RunID -> sanitizeName -> Job.Name` 구조다.

문제:

- 동일 run 재실행 시 이름 충돌
- retry 와 rerun 을 구분하기 어려움
- crash recovery 시 "이 Job이 이전 attempt 인가, 새 attempt 인가" 추적이 약함

`poc`에서도 이미 동일 run 재실행 시 삭제/재제출 이슈가 관찰됐다.

권장 방향:

- logical run id: 사용자가 제출한 실행 단위
- physical attempt id: 실제 K8s Job 단위
- handle 에 job name 뿐 아니라 job UID 또는 attempt id 포함

예시:

- run id: `sample-123`
- attempt id: `sample-123-attempt-01`
- job name: `sample-123-a1-7f2c9d`

### 3.3 P0: duplicate / retry / recovery semantics 강화

현재 Dispatcher 는 ingress boundary 를 세우기 시작했지만,
duplicate submit 과 recovery semantics 는 아직 약하다.

현재 문제:

- `ErrAlreadyExists`를 사실상 삼키고 계속 진행
- 상태 전이 실패를 로그만 남기고 통과
- bootstrap 은 recovered record 를 읽기만 하고 실제 재디스패치는 안 함

이건 PoC 에서는 괜찮지만 제품에서는 위험하다.

결정해야 할 것:

- 같은 run id 재제출 시 reject 할지, idempotent success 로 볼지
- running 중 재제출은 어떻게 처리할지
- admitted 상태에서 프로세스가 죽었을 때 restart 후 어떻게 reconcile 할지
- canceled / failed / succeeded 이후 replay 정책은 무엇인지

권장 방향:

- duplicate 정책을 문서화
- retryable / terminal 상태 구분
- bootstrap 시 record 재주입이 아니라 reconcile-first 정책 도입

### 3.4 P1: RunSpec 확장

현재 `pkg/api/types.go`의 `RunSpec`은 PoC 수준으로는 충분하지만 제품용으로는 얇다.

앞으로 필요한 후보:

- spec version
- submitter identity
- trace / correlation id
- execution class
- retry policy
- timeout policy
- TTL / cleanup policy
- node selector / toleration / affinity
- image pull secret / service account
- annotations
- artifact / volume intent

중요한 점:

필드를 무작정 늘리기보다 "driver 에 필요한 것"과 "상위 orchestration 에 필요한 것"을 분리하는 것이 좋다.

예:

- `RunSpec`: 실행에 직접 필요한 것
- `RunMeta` 또는 `SubmitRequest`: identity, trace, policy 같은 제어 정보

### 3.5 P1: bootstrap/recovery 를 실제 기능으로 만들기

현재 `cmd/server/main.go`는 bootstrap 결과를 로그로만 보여준다.

이건 방향 소개로는 충분하지만 서버 동작으로는 부족하다.

필요한 보강:

- startup 시 queued / held / admitted state scan
- 기존 K8s Job 존재 여부 확인
- 없으면 재디스패치, 있으면 reattach/reconcile
- recovery 결과를 이벤트와 로그로 남기기

권장 질문:

- admitted-to-dag 상태인데 Job 이 실제로 없다면 재실행할 것인가
- Job 은 남아 있는데 actor state 만 없으면 어떻게 reattach 할 것인가
- cancel 요청이 쌓인 상태에서 restart 하면 어떤 순서로 처리할 것인가

### 3.6 P2: Observability 를 helper 에서 operator-facing model 로 승격

현재 `K8sObserver`는 helper 로는 좋다.
하지만 운영자가 실제로 보고 싶은 것은 함수 호출 결과가 아니라 일관된 상태 모델이다.

예를 들면 아래 상태들이 필요하다.

- queued
- held
- admitted by dispatcher
- pending in Kueue
- unschedulable in scheduler
- running
- cancelling
- canceled
- failed
- succeeded

즉, `spawner` 내부 이벤트 모델과 관측 모델을 맞춰야 한다.

## 4. 권장 개발 순서

### Stage 1. 실행 의미론 고정

먼저 정해야 할 것:

- duplicate semantics
- retry semantics
- rerun semantics
- cancel semantics
- recovery semantics

이 단계에서 문서와 테스트가 먼저 나와야 한다.

### Stage 2. DriverK8s 보강

그 다음 작업:

- watch 기반 Wait
- attempt identity 도입
- richer event emission

이 단계가 끝나면 실행 백엔드로서의 품질이 크게 올라간다.

### Stage 3. Dispatcher / Server 보강

그 다음 작업:

- bootstrap 실동작 구현
- health / readiness / recovery loop
- operator-facing status 조회

### Stage 4. API 확장

마지막으로:

- `RunSpec` / `SubmitRequest` 구조 재정리
- gRPC API 정리
- integration / end-to-end test 확장

## 5. 우선순위 TODO

### P0

1. `DriverK8s.Wait()`를 watch 기반으로 전환
2. run id 와 attempt/job id 분리
3. duplicate / retry / recovery semantics 문서화
4. bootstrap 재디스패치 또는 reconcile 구현

### P1

5. `RunSpec` 확장 또는 `RunMeta` 분리
6. Job / Pod / Workload 상태를 내부 event model 로 통합
7. Dispatcher 상태 전이 실패를 명시적으로 처리

### P2

8. operator-facing status 조회 인터페이스 추가
9. gRPC / bufconn 테스트 추가
10. integration test 를 CI 에 올릴 수 있는 형태로 정리

## 6. 최종 의견

`spawner`는 버려야 할 코드가 아니다.
오히려 `poc`에서 이미 증명된 것처럼, dag 기반 실행기의 backend 로 발전시킬 가치가 충분하다.

다만 다음 단계는 기능 추가보다 의미론 정리가 우선이다.

가장 중요한 순서는 아래다.

1. 실행과 복구 의미론을 고정한다.
2. identity 와 event model 을 정리한다.
3. 그 다음 watch / recovery / API 를 확장한다.

이 순서로 가면 `spawner`는 단순 PoC helper 가 아니라,
제품용 실행 서비스의 핵심 구성요소가 될 수 있다.
