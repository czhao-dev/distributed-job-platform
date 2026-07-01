# control-plane

A declarative infrastructure control plane: Deployment scheduling, Node registration and heartbeating, desired-state reconciliation, failure recovery, label-based Service discovery, and persistent state via an embedded key-value store (BoltDB).

This is one of three independent Go modules in the parent [load-balanced-orchestrator](../README.md) repo. See [docs/architecture.md](docs/architecture.md) for the component design.

## Component overview

| Orchestration component | This project |
|---|---|
| API Server | `cmd/control-plane` + `internal/api` |
| Persistent state store | `internal/state.BoltStore` (BoltDB, single-node) |
| Scheduler | `internal/scheduler` |
| Controller / Reconciler | `internal/reconciler` |
| Node agent | `internal/worker.Agent` + `cmd/worker` |
| CLI client | `cmd/infractl` |

## Binaries

- `cmd/control-plane` — the API server: Deployment/Pod/Node/Service store, Scheduler loop, Reconciler loop.
- `cmd/worker` — the node agent: registers as a Node, heartbeats, polls for assigned Pods, executes them as OS subprocesses.
- `cmd/infractl` — stdlib-only CLI client.

## Quickstart (no Docker)

```bash
# Start control plane (in-memory store; set CTRLPLANE_DB_PATH=./state.db for persistence)
go run ./cmd/control-plane &

# Start two node agents (register independently as Nodes)
go run ./cmd/worker &
go run ./cmd/worker &

# Submit a Deployment and watch it schedule Pods onto Nodes
go run ./cmd/infractl deployment submit examples/batch-job.yaml
go run ./cmd/infractl node list
go run ./cmd/infractl deployment status <deployment-id>

# Label-filtered node listing
go run ./cmd/infractl node list --label role=worker

# Service with selector-based backend resolution
go run ./cmd/infractl service add examples/proxy-service.yaml
go run ./cmd/infractl service backends <service-id>
```

`INFRACTL_SERVER` (default `http://localhost:7070`) points `infractl` at a different control plane. See [../docker-compose.yml](../docker-compose.yml) and [scripts/](scripts/) for the full Docker-based demo (multiple nodes, dynamic-discovery proxy, Prometheus/Grafana).

## Persistent state

Set `CTRLPLANE_DB_PATH=./ctrlplane.db` to enable BoltDB-backed persistence. State (Deployments, Pods, Nodes, Services) survives process restarts. Omit the env var for in-memory mode (ephemeral, useful for testing).

## Domain model

`Deployment` (desired state: command, replicas, retry policy, resources, namespace, labels) → `Pod` (one execution unit per replica, inheriting the Deployment's namespace and labels) → assigned to a `Node` by the Scheduler. `Service` (path prefix + Node label selector) drives health-aware backend discovery.

All four resource types follow the same state-machine pattern: `Status string` + `allowedTransitions map[Status][]Status` + pure `Transition(from, to) bool` validator, applied atomically inside the state store.

## Labels and namespaces

All resources support `labels map[string]string` for selector-based filtering. Deployments, Pods, and Services carry a `namespace` field (Nodes are cluster-scoped). Pods inherit their Deployment's namespace and labels at creation time (pod-template semantics).

API query params:
- `?namespace=<ns>` on `GET /api/v1/deployments` and `GET /api/v1/services`
- `?label=key=value` (repeatable) on `GET /api/v1/nodes`
- `infractl deployment list --namespace production --label app=web`
- `infractl node list --label role=worker`

## Testing

```bash
go test ./... -race
```

Unit tests cover model state-transition matrices, the in-memory store (including concurrent-access races), BoltDB persistence (including a survive-restart test), scheduler placement, reconciler failure scenarios, the HTTP API (via `httptest`), and the node agent (registration/heartbeat/poll/execute/graceful-shutdown against a fake control-plane server).

## Docs

- [docs/architecture.md](docs/architecture.md) — component design and documented simplifications
- [docs/scheduler.md](docs/scheduler.md) — placement policy (FIFO, resource-aware, least-loaded)
- [docs/reconciler.md](docs/reconciler.md) — desired-state reconciliation and failure recovery
- [docs/node-model.md](docs/node-model.md) — node agent lifecycle

## Documented simplifications

| Production orchestrators | This project |
|---|---|
| Distributed consensus store (e.g. etcd with Raft) | BoltDB (embedded, single-node, ACID) |
| Watch/informer event streams | Timer-based polling (scheduler: 500ms, reconciler: 2s) |
| Services select Pods via network | Services select Nodes (Pods are subprocesses with no network address) |
| Container runtime (OCI, runc) | `exec.CommandContext` — Pods run as OS subprocesses |
| RBAC, admission control, CRDs, autoscaling | Not implemented |
