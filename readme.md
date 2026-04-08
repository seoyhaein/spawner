# spawner

`spawner` is a lightweight execution backend for submitting and observing Kubernetes Jobs.

This repository currently provides:

- A small `driver.Driver` abstraction for execution backends
- `DriverK8s` for Kubernetes Job execution
- `BoundedDriver` for burst control on `Start()`
- `K8sObserver` for Kueue admission and kube-scheduler observation
- Dispatcher / Actor / FrontDoor experiments for ingress and worker orchestration
- Durable-lite `RunStore` implementations for restart-oriented experiments

## Current Status

The repository is usable as a PoC execution backend and is already being exercised from the sibling `poc` project.

From the `poc` integration point of view, the most valuable parts of this repository are:

- `cmd/imp/k8s_driver.go`
- `cmd/imp/bounded_driver.go`
- `cmd/imp/k8s_observer.go`
- `pkg/driver`
- `pkg/dispatcher`
- `pkg/store`

The codebase is strong enough for architecture validation, but it is not yet a fully productized execution service.

## Productization Review

Based on the `poc` integration review, the main reinforcement areas are:

- Replace `Wait()` polling with watch/informer-based observation
- Separate logical run identity from physical job/attempt identity
- Strengthen duplicate submit, retry, and recovery semantics
- Expand `RunSpec` for production needs
- Turn bootstrap/recovery into real executable behavior
- Upgrade observability from helper methods to operator-facing state/event models

Detailed bilingual review documents:

- Korean: [docs/SPAWNER_REINFORCEMENT_REVIEW.ko.md](docs/SPAWNER_REINFORCEMENT_REVIEW.ko.md)
- English: [docs/SPAWNER_REINFORCEMENT_REVIEW.en.md](docs/SPAWNER_REINFORCEMENT_REVIEW.en.md)

## Immediate Notes

The previous TODO list is still valid in spirit, but it is now superseded by the structured review documents above.

Short version:

- gRPC and integration tests need to be expanded
- Actor lifecycle and reuse policy should be clarified
- Driver errors and execution state should be surfaced consistently
- Recovery semantics should move from comments/assumptions to real code
