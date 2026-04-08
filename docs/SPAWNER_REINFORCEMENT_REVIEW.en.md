# Spawner Reinforcement Review

> Date: 2026-04-08  
> Basis: review driven by how the sibling `poc` project uses `spawner` as a real execution backend

## 1. Summary

`spawner` already has clear value as a PoC execution backend.
Several parts of the repository are structurally strong:

- A small and understandable `driver.Driver` abstraction
- `DriverK8s` as a focused Kubernetes Job backend
- `BoundedDriver` for controlling `Start()` burst pressure
- `K8sObserver` for separating Kueue admission from scheduler placement
- `Dispatcher + RunStore` as the beginning of a real ingress boundary

That said, from a productization point of view, the repository is still closer to
"a good PoC backend" than "a recoverable production execution service."

The main reinforcement areas are:

1. Move `Wait()` from polling to watch/informer-based observation
2. Separate logical run identity from physical job/attempt identity
3. Strengthen duplicate submit, retry, and recovery semantics
4. Expand `RunSpec`
5. Turn observability and bootstrap/recovery into real operational behavior

## 2. Current Strengths

### 2.1 The driver boundary is small and clear

`pkg/driver/driver.go` defines a compact interface:

- `Prepare`
- `Start`
- `Wait`
- `Signal`
- `Cancel`

This boundary made it easy for `poc` to integrate `spawner` through `SpawnerNode`,
and it is straightforward to mock in tests.

### 2.2 BoundedDriver has real product value

`cmd/imp/bounded_driver.go` is simple, but it is useful.
In `poc`, it already proved valuable for limiting Kubernetes Job creation bursts
during fan-out and multi-run burst scenarios.

This is not throwaway experiment code. It is likely to survive into a real product path.

### 2.3 K8sObserver is directionally correct

`cmd/imp/k8s_observer.go` tries to distinguish:

- Kueue pending
- kube-scheduler unschedulable

That distinction matters a lot for operators, so the direction is correct.

## 3. Areas That Need Reinforcement Before Productization

### 3.1 P0: Replace polling-based Wait()

Today, `DriverK8s.Wait()` in `cmd/imp/k8s_driver.go` is based on 2-second polling.

Problems:

- API server read load grows with job count
- state transitions are observed late
- it only models terminal completion, not the richer intermediate states needed operationally

Why this matters:

The `poc` project already exercises timeout, pending, fast-fail, burst control,
and scheduler blocking. That means `spawner` will need more than a simple
success/failure wait loop if it is going to become a product backend.

Recommended direction:

- `Job` watch
- `Pod` watch when needed
- `Workload` watch when needed
- unify them behind a common event stream

Recommended outcome:

- `Wait()` may still return a terminal event, but should use watches internally
- running/admitted/pending signals should be exposed via events, callbacks, or sinks

### 3.2 P0: Separate Run ID from Job/Attempt ID

Right now the flow is effectively:

- `RunID -> sanitizeName -> Job.Name`

Problems:

- reruns collide with prior job names
- retries and reruns are hard to distinguish
- crash recovery cannot cleanly tell whether a job belongs to the current or previous attempt

The sibling `poc` project already exposed this weakness in rerun scenarios.

Recommended direction:

- logical run id: the user-facing execution unit
- physical attempt id: the concrete Kubernetes Job attempt
- driver handle should retain more than a job name, ideally job UID or attempt id

Example:

- run id: `sample-123`
- attempt id: `sample-123-attempt-01`
- job name: `sample-123-a1-7f2c9d`

### 3.3 P0: Strengthen duplicate, retry, and recovery semantics

The Dispatcher has started to become a real ingress boundary,
but duplicate submit and recovery semantics are still weak.

Current problems:

- `ErrAlreadyExists` is effectively swallowed
- state transition failures are logged and ignored
- bootstrap reads recovered records but does not actually redispatch or reconcile them

This is acceptable in a PoC, but risky in a product.

Questions that must be answered:

- should the same run id be rejected, treated as idempotent success, or replayed?
- how should resubmission behave while a run is still active?
- after a process restart, how should `admitted-to-dag` records be reconciled?
- what is the replay policy after `canceled`, `failed`, or `succeeded`?

Recommended direction:

- document duplicate policy explicitly
- separate retryable from terminal states
- make bootstrap recovery reconcile-first rather than blindly redispatching

### 3.4 P1: Expand RunSpec

`pkg/api/types.go` defines a `RunSpec` that is fine for PoC use,
but too thin for a production API.

Likely future additions:

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

Important note:

It is better not to stuff everything directly into `RunSpec`.
A cleaner split may be:

- `RunSpec`: execution-facing fields
- `RunMeta` or `SubmitRequest`: identity, trace, and policy fields

### 3.5 P1: Turn bootstrap/recovery into real behavior

Today `cmd/server/main.go` logs bootstrap results, but does not act on them.

That is enough for illustrating direction, but not enough for a real service.

Needed improvements:

- scan `queued`, `held`, and `admitted` records on startup
- detect whether a corresponding Kubernetes Job still exists
- redispatch if missing, reattach/reconcile if present
- emit recovery results into logs and events

Recommended questions:

- if a run is `admitted-to-dag` but no Job exists, should it be retried automatically?
- if the Job exists but actor state is gone, should the process reattach?
- how should pending cancel requests behave after restart?

### 3.6 P2: Promote observability from helper methods to an operator-facing model

`K8sObserver` is already useful as a helper.
But operators do not want raw helper calls, they want a coherent state model.

Examples of meaningful operator-facing states:

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

In other words, `spawner` needs its internal event model and its observation model
to converge.

## 4. Recommended Development Order

### Stage 1. Fix execution semantics first

Decide and document:

- duplicate semantics
- retry semantics
- rerun semantics
- cancel semantics
- recovery semantics

Tests and documents should come before feature expansion here.

### Stage 2. Reinforce DriverK8s

Then implement:

- watch-based waiting
- attempt identity
- richer event emission

This will significantly improve the quality of the execution backend.

### Stage 3. Reinforce Dispatcher / Server

Then implement:

- real bootstrap behavior
- health / readiness / recovery loops
- operator-facing status queries

### Stage 4. Expand the API

Finally:

- restructure `RunSpec` / `SubmitRequest`
- refine the gRPC API
- expand integration and end-to-end tests

## 5. Priority TODO

### P0

1. Replace `DriverK8s.Wait()` with a watch-based implementation
2. Separate run id from attempt/job id
3. Document duplicate, retry, and recovery semantics
4. Implement bootstrap redispatch or reconcile behavior

### P1

5. Expand `RunSpec` or split out `RunMeta`
6. Unify Job / Pod / Workload state into an internal event model
7. Handle Dispatcher state-transition failures explicitly

### P2

8. Add an operator-facing status query interface
9. Add gRPC / bufconn tests
10. Make integration tests easier to run in CI

## 6. Final Opinion

`spawner` is not code that should be thrown away.
If anything, the sibling `poc` project already shows that it is worth evolving further
as the backend of a DAG-oriented execution system.

The next step should not be "add more features first."
The next step should be to stabilize semantics.

The most important order is:

1. fix execution and recovery semantics
2. clean up identity and event modeling
3. then expand watch/recovery/API behavior

If that order is followed, `spawner` can evolve from a PoC helper
into a real execution service component.
