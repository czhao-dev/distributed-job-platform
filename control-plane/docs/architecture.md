# Architecture

This module is a declarative infrastructure control plane. It implements the same architectural components found in production orchestration systems, simplified to run OS-subprocess workloads instead of OCI containers.

## Component mapping

| Orchestration component | This project |
|---|---|
| API Server | `cmd/control-plane` + `internal/api` |
| Persistent state store | `internal/state.BoltStore` (BoltDB, single-node — see below) |
| Scheduler | `internal/scheduler` |
| Controller Manager / Reconciler | `internal/reconciler` |
| Node agent | `internal/worker.Agent` + `cmd/worker` |
| CLI client | `cmd/infractl` |

## Topology

```
infractl (CLI client)
      |
      v
Control Plane API (:7070)      ← API Server
      |
      +--> BoltStore / MemoryStore  ← persistent state (see CTRLPLANE_DB_PATH)
      +--> Scheduler           ← assigns PENDING pods to HEALTHY nodes
      +--> Reconciler          ← desired-state loop, failure detection
      |
      v
Node Agents (cmd/worker)       ← node agent (kubelet-style process)
      |  registers, heartbeats, polls /nodes/{id}/pods/poll
      +--> subprocess execution (pods run as OS processes, not containers)
      |
      v
Reverse Proxy (dynamic-discovery mode)
      |
      +--> polls GET /api/v1/proxy/backends
      +--> routes to HEALTHY nodes only (via Node.Address)
```

## Control plane responsibilities

- Receive `Deployment` specs (declarative desired state: command, replicas, retry policy, resources, namespace, labels).
- Register `Node`s and track liveness via heartbeats.
- Schedule `Pod`s (one execution unit per Deployment replica) onto nodes with available capacity.
- Reconcile actual pod/node state against desired state every tick: create missing pods (inheriting the Deployment's namespace and labels), cancel excess ones, detect heartbeat timeouts, reschedule orphaned pods, dead-letter pods that exhaust their retry budget.
- Expose `Service`/backend data so the reverse proxy can discover and health-route to the live node fleet.
- Resolve `Service.Selector` (label-based node matching) via `GET /api/v1/services/{id}/backends`.

## Node agent responsibilities

`cmd/worker` is a separate, independently-running process (see [node-model.md](node-model.md)) that:

- Registers with the control plane on startup (capacity, address) → gets a Node ID.
- Heartbeats on a fixed interval.
- Polls for pods assigned to it (`SCHEDULED` status), executes them as subprocesses, and reports status transitions back.
- Drains in-flight pods on shutdown rather than dropping them.

## Documented simplifications

These are intentional design decisions for the scope of this project, not bugs:

| Production orchestrators | This project |
|---|---|
| Distributed consensus store (e.g. etcd with Raft, gRPC) | BoltDB: embedded single-file ACID store, no network |
| Watch/informer event streams from the state store | Node agent polls on a timer (500 ms); reconciler ticks on a timer (2 s) |
| Services select Pods by label via overlay networking | Services select Nodes by label (Pods have no network address; nodes are the routable entities) |
| Container runtime (OCI, containerd, runc) | `exec.CommandContext` — pods run as OS subprocesses |
| RBAC, admission webhooks, CRDs, autoscaling | Not implemented |

## Why node agents are a separate process from the control plane

Unlike a single-binary setup (where the worker pool is in-process goroutines), here the node agent is a standalone binary that can run on a different host, register/deregister independently, and be killed without taking the scheduler down with it — that decoupling is what makes node-failure recovery (see [reconciler.md](reconciler.md)) a meaningful thing to demonstrate.

## Why the proxy is not part of this module

`reverse-proxy-load-balancer` is a separate, independently-buildable Go module (see the root README). Rather than duplicating proxy logic here, this module only exposes the HTTP API (`GET /api/v1/proxy/backends`) that the proxy's `backend.ConfigProvider` polls — the proxy decides what to do with that data (load-balancing strategy, health checks, retries) entirely on its own.
