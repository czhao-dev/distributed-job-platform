# Scheduler

`internal/scheduler` assigns `PENDING` pods to `HEALTHY` nodes on a fixed poll interval (`CTRLPLANE_SCHEDULER_INTERVAL`, default 500ms — same ticker-loop shape as `ml-job-orchestrator`'s scheduler).

## Policy

Each tick (`Scheduler.Tick`):

1. List `PENDING` pods, sorted FIFO by `CreatedAt`. A pod whose `RunAfter` is still in the future (set by the reconciler as a backoff gate after a failure) is skipped until that time passes.
2. List all nodes; `placement.SelectNode` filters to `HEALTHY` ones with enough available CPU/memory and concurrency headroom for the pod's deployment's `Resources`, then picks the **least-loaded** (lowest `RunningJobs`), breaking ties by earliest `RegisteredAt` for determinism.
3. On a match: assign `NodeID`, transition the pod to `SCHEDULED`, and decrement the node's `Available` capacity / increment `RunningJobs`.
4. On no match (`ErrNoCapacity`): the pod stays `PENDING` and is retried next tick.

Processing pods in FIFO order satisfies the spec's "FIFO" requirement; the capacity/least-loaded filter inside `SelectNode` satisfies "resource-aware" — one function does both, since FIFO is about pod *order* and resource-awareness is about node *choice*, and they don't conflict.

## What the scheduler does NOT do

Retry/backoff logic for *failed* pods lives in the reconciler, not here (see [reconciler.md](reconciler.md)) — the scheduler's only job is "pick up anything that's `PENDING` right now," regardless of *why* it became pending (first attempt, or a reconciler-driven retry).

## Capacity bookkeeping

Capacity is reserved at schedule time (`Available -= Resources`, `RunningJobs++`) and released at pod-completion time (`POST /api/v1/nodes/{id}/pods/{pod_id}/status` handler, or by the reconciler if a node disappears mid-pod) — never adjusted on the intermediate "now running" report, since the slot was already accounted for when the pod was dispatched.

## Metrics

`ctrlplane_scheduler_queue_depth` (gauge, pods still pending after the tick) and `ctrlplane_scheduler_latency_seconds` (histogram, time from `Pod.CreatedAt` to being scheduled).
