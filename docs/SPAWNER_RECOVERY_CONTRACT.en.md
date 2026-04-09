# Spawner Recovery Contract

This document locks down the current `spawner` contract for recovery, replay, and duplicate submit behavior.

## Core Model

- `LogicalRunID`: logical run identity
- `AttemptID`: concrete execution-attempt identity
- `RunStore`: stores both `RunRecord` summary and `AttemptRecord` history

## Persisted State

Persisted states currently mean:

| State | Meaning | Recoverable |
| --- | --- | --- |
| `queued` | request accepted but not fully handed off to execution yet | yes |
| `held` | backend unavailable or policy hold | no |
| `resumed` | hold lifted, waiting for re-entry | no |
| `admitted-to-dag` | handed to dispatcher/actor boundary | yes |
| `running` | actively executing | no |
| `finished` | terminal completion | no |
| `canceled` | terminal cancellation | no |

Under the current fast-fail default, only `queued` and `admitted-to-dag` are automatic recovery candidates.

## Event State

Event states are transient, operator-facing execution states.

- `queued`
- `starting`
- `running`
- `cancelling`
- `cancelled`
- `succeeded`
- `failed`

These are not a 1:1 mirror of persisted states. Persisted states carry durable ingress/recovery meaning, while event states carry observation meaning.

## Attempt Cause

`AttemptRecord` now uses a typed cause instead of a free-form string.

- `initial-submit`
- `recovery-replay`
- `manual-requeue`
- `auto-retry`

## Decision Table

| Scenario | New Attempt | Attempt Cause | Notes |
| --- | --- | --- | --- |
| first submit | no | `initial-submit` | creates the first attempt |
| duplicate submit with same attempt id | no | unchanged | idempotent no-op |
| recovery replay | policy-driven | `recovery-replay` when a new attempt is created | default policy keeps the current attempt |
| manual requeue | policy-driven | `manual-requeue` when a new attempt is created | default policy allocates a new attempt |
| auto retry | policy-driven | `auto-retry` when a new attempt is created | default policy allocates a new attempt |

## Current Defaults

- recovery replay: keep existing attempt
- manual requeue: allocate a new attempt
- auto retry: allocate a new attempt

So the current default philosophy separates "fast-fail execution" from "recovery-safe persistence".

- failed runs are not automatically retried
- ambiguous in-between states are not lost and remain replayable

