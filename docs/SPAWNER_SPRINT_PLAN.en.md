# Spawner Sprint Plan

This document lays out a sprint plan for moving `spawner` from a PoC execution backend toward a product-grade library and service core.

The plan is based on two inputs:

- what `pipeline-lite-poc` actually validated in practice
- what the static `spawner` review identified as weak library contracts, lifecycle gaps, recovery gaps, and validation gaps

The main interpretation is straightforward:

- the execution backend path (`DriverK8s`, `BoundedDriver`, `K8sObserver`) looks directionally strong for the PoC
- the library-facing contracts around `Dispatcher`, `Factory`, `Actor`, `FrontDoor`, and `RunStore` are still loose
- therefore the next work should prioritize lifecycle, validation, identity, and recovery envelopes before feature expansion

## Planning Assumptions

- Baseline date: `2026-04-09`
- Sprint length: `2 weeks`
- Assumed start date: `2026-04-13`
- Team assumption: one main implementer
- In scope: hardening `spawner` as a library/service boundary
- Out of scope: informer migration, full Kueue/cluster hardening, deployment automation

## Sprint Summary

| Sprint | Dates | Theme | Primary Goal |
| --- | --- | --- | --- |
| Sprint 1 | 2026-04-13 ~ 2026-04-24 | Lifecycle Stabilization | Remove real failure risks in dispatcher/actor/factory boundaries |
| Sprint 2 | 2026-04-27 ~ 2026-05-08 | Safe Command And Identity | Lock command validation, logical run identity, and stored envelope contracts |
| Sprint 3 | 2026-05-11 ~ 2026-05-22 | Recovery And State Contract | Promote recovery to a real API and define canonical persisted/event state mapping |
| Sprint 4 | 2026-05-25 ~ 2026-06-05 | Library Surface Hardening | Clean up policy, backend-neutral naming, and public API surface |

## Sprint 1

Dates: `2026-04-13 ~ 2026-04-24`

Goals:

- fix the structure where `Dispatcher.Sem` is not clearly released on the normal path
- lock actor unbind / terminate / reuse lifecycle with code and tests
- reduce immediate hazards such as empty spawn keys, unchecked bind success, and blocking sink leak risk

Work items:

- make bind/register/unbind/release timing explicit in `Dispatcher`
- add `OnTerminate`-driven cleanup or explicitly constrain the current contract
- document `Factory` idle-pool reuse rules
- add empty spawn key guards
- validate resolved routing results
- harden event delivery so timeout paths do not leak goroutines behind blocking sinks
- add regression tests showing repeated distinct runs do not saturate the dispatcher after normal completion

Deliverables:

- documented lifecycle sequence
- dispatcher/factory/actor regression test set
- guards for empty spawn keys and invalid resolve results
- safe event-delivery wrapper or non-blocking sink adapter

Exit criteria:

- distinct `runID`s can be processed sequentially without permanent saturation after normal completion
- actor reuse behavior is locked by tests
- empty spawn keys are rejected before dispatcher admission
- blocking sink behavior has an explicit anti-leak strategy in code

## Sprint 2

Dates: `2026-04-27 ~ 2026-05-08`

Goals:

- make `Command` and input payloads type-safe
- separate logical run id, spawn key, and attempt identity
- replace best-effort payload storage with a replay-safe envelope

Work items:

- add `NewRunCommand`, `NewCancelCommand`, `NewSignalCommand`, and similar constructors
- add `Command.Validate()`
- add nil guards at actor entry points
- add `RunSpec.Validate()`
- define stable logical run id rules
- introduce attempt ids
- introduce `RunEnvelope` or equivalent
- store `version`, `kind`, `meta`, `spec`, and `identity` in persisted payloads

Deliverables:

- typed constructors and validation API
- documented run/attempt/spawn identity rules
- replay-safe envelope type
- removal of `json.Marshal(in.Req)` best-effort payload persistence

Exit criteria:

- invalid `Command` combinations are rejected at construction or validation time
- `RunID` is no longer overloaded across logical and physical identities
- persisted payloads contain the minimum information needed for safe replay

## Sprint 3

Dates: `2026-05-11 ~ 2026-05-22`

Goals:

- promote bootstrap from a helper into a real recovery API
- define canonical mapping between persisted lifecycle state and event state

Work items:

- connect `Bootstrap()` to an actual replay workflow
- design `Recover()` or `ReplayRecoveredRuns()` style APIs
- document persisted lifecycle and transient event state separately
- define terminal semantics, retry transitions, and cancel transitions
- distinguish duplicate submit, replay submit, and already-running resubmission behavior
- define schema mismatch and validation failure handling during recovery

Deliverables:

- recovery API
- state mapping document and tests
- duplicate/replay semantics tests
- persisted envelope version mismatch handling strategy

Exit criteria:

- queued and admitted runs can follow a standard replay path after restart
- persisted state and event state relationships are aligned across code, docs, and tests
- duplicate submit and replay submit are behaviorally distinct and explicit

## Sprint 4

Dates: `2026-05-25 ~ 2026-06-05`

Goals:

- harden the public library surface for product use
- clean up policy semantics, naming, and API shape

Work items:

- add validation/defaulting/merge semantics to `policy`
- replace backend-specific names such as `k8sAvailable` with generic executor terminology
- expand `RunSpec`
- define `MetaContext` immutability or copy semantics
- improve `EventSink` with error-aware or buffered adapters
- update docs and examples

Deliverables:

- stronger policy package
- backend-neutral naming cleanup
- expanded `RunSpec`
- library examples and migration notes

Exit criteria:

- public APIs have documented validation and defaulting behavior
- dispatcher terminology is no longer tightly coupled to a specific backend name
- examples are updated to the newer contract

## Recommended Sequencing Inside Each Sprint

Within each sprint, the recommended order is:

1. write the contract doc first
2. add failing regression tests
3. implement the minimum change needed to pass
4. clean up naming, docs, and examples
5. run lint, test, coverage, and update release notes

## Effort Estimate

Rough estimate for one main implementer:

- Sprint 1: `8 ~ 10` working days
- Sprint 2: `8 ~ 10` working days
- Sprint 3: `8 ~ 10` working days
- Sprint 4: `6 ~ 8` working days

Overall estimate:

- focused implementation: `30 ~ 38` working days
- calendar duration: about `8 weeks`

## What Must Not Slip

These items should not be deferred:

- dispatcher normal-path lifecycle release
- invalid command combinations
- logical run id vs attempt id confusion
- best-effort payload persistence
- empty spawn key acceptance

## Suggested Milestones

- `2026-04-24`: lifecycle bugs and validation P0 closed
- `2026-05-08`: command, identity, and envelope contracts fixed
- `2026-05-22`: recovery flow and state mapping fixed
- `2026-06-05`: first hardening pass on the public library surface complete

## Final Recommendation

`spawner` should harden its library contract before expanding execution behavior further.

The first priority is not adding more features.

- lifecycle release
- command validation
- identity separation
- replay-safe envelope

Those four items should be fixed first. Recovery, policy, and API expansion become meaningful only after that.
