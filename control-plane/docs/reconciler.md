# Reconciler

`internal/reconciler` maintains desired state under failure on a fixed interval (`CTRLPLANE_RECONCILE_INTERVAL`, default 2s). Each tick runs two independent passes.

## Pass 1: replica reconciliation

For every non-`CANCELLED` deployment:

- A `PENDING` deployment is activated (`ACTIVE`) the first time the reconciler sees it.
- `active` = count of pods whose status counts toward a replica slot (`Pod.Active()`: `PENDING`/`SCHEDULED`/`RUNNING`/`RETRYING`/`FAILED`/`SUCCEEDED` — only `DEAD_LETTER` and `CANCELLED` free a slot). This treats `Replicas` as "run this many pod instances" (batch semantics), not "keep N running forever" — see the comment on `Pod.Active()` for why, and for the documented gap around `restart_policy: always`.
- If `active < Replicas`: create the missing pods as `PENDING`, copying the deployment's `Command`/`Args` and inheriting the deployment's `Namespace` and `Labels` (pod-template semantics: the Deployment is the template, Pods are instances).
- If `active > Replicas` (scale-down): cancel the newest excess `PENDING`/`SCHEDULED` pods first. **A `RUNNING` pod is never cancelled by scale-down** — it is left to finish.
- If any pod for the deployment is `DEAD_LETTER`, the deployment is marked `DEGRADED` (and back to `ACTIVE` once no dead-lettered pods remain) — a cheap signal that something needed manual attention.

## Pass 2: node heartbeat timeout detection

For every node whose `now - LastHeartbeatAt` exceeds `CTRLPLANE_HEARTBEAT_TIMEOUT` (default 15s, ~3x the node agent's own 5s heartbeat interval):

- `HEALTHY → UNHEALTHY`, then reschedule its pods (see below).
- `DRAINING → REMOVED` instead — an operator-initiated decommission (`infractl node drain`) that has gone quiet is treated as a completed removal, not a failure.

A node recovers (`UNHEALTHY → HEALTHY`) the instant it heartbeats again — that direction is owned by the heartbeat HTTP handler, not the reconciler, since receiving a heartbeat is definitionally proof of life. **Known gap:** a node that crashes and never sends another heartbeat stays `UNHEALTHY` forever; there is no automatic garbage-collection for permanently-dead nodes (only the `DRAINING → REMOVED` path fully removes one).

### Rescheduling a failed node's pods

- A pod that was `SCHEDULED` but never reached `RUNNING` (the node died between dispatch and pickup) is sent straight back to `PENDING` — it never executed, so it does not burn a retry attempt.
- A pod that was actually `RUNNING` is pessimistically assumed lost: `Attempt++`, and if `Attempt <= Deployment.MaxRetries`, it is requeued to `PENDING` with an exponential-backoff `RunAfter` (`2^attempt` seconds, capped at 60s — same formula as `ml-job-orchestrator/internal/retry`). If the retry budget is exhausted, it goes to `DEAD_LETTER` instead.

This two-case split was found by actually running the node-failure demo against a live cluster — an earlier version only handled the `RUNNING` case, leaving `SCHEDULED` pods permanently stuck pointing at a dead node (see `TestReconciler_OrphanedScheduledPodRequeuedWithoutBurningRetry`).
