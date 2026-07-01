// Package metrics defines Prometheus metrics for the control-plane binary
// (cmd/control-plane) only -- see internal/agentmetrics for the node agent's
// metrics, kept in a separate package so each binary's process-global registry
// only contains the metrics it actually owns. All metric names are prefixed
// ctrlplane_ to avoid collisions with ml-job-orchestrator's mlorch_* and
// reverse-proxy-load-balancer's proxy_* metrics scraped by the same Prometheus
// instance.
//
// Note: Prometheus wire metric names (ctrlplane_workloads_total, etc.) are
// intentionally left unchanged to avoid breaking existing Grafana dashboards
// and alert rules. Only the Go variable names have been updated to reflect
// the new Deployment/Pod/Node vocabulary.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	DeploymentsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ctrlplane_workloads_total",
		Help: "Total deployments submitted",
	})

	JobsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ctrlplane_jobs_total",
		Help: "Total pods created",
	})

	// JobsPending/JobsRunning are gauges (current counts), not counters.
	JobsPending = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ctrlplane_jobs_pending",
		Help: "Pods currently pending",
	})

	JobsRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ctrlplane_jobs_running",
		Help: "Pods currently running",
	})

	JobsSucceeded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ctrlplane_jobs_succeeded_total",
		Help: "Total pods that succeeded",
	})

	JobsFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ctrlplane_jobs_failed_total",
		Help: "Total pod attempts that failed (including ones later retried)",
	})

	JobsDeadLetter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ctrlplane_jobs_dead_letter_total",
		Help: "Total pods that exhausted their retry budget and were dead-lettered",
	})

	SchedulerQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ctrlplane_scheduler_queue_depth",
		Help: "Number of pods currently pending scheduling",
	})

	SchedulerLatencySeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ctrlplane_scheduler_latency_seconds",
		Help:    "Time from pod creation to being scheduled onto a node",
		Buckets: prometheus.DefBuckets,
	})

	ReconcilerIterations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ctrlplane_reconciler_iterations_total",
		Help: "Total reconciler loop iterations",
	})

	WorkerHeartbeats = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ctrlplane_worker_heartbeats_total",
		Help: "Total node heartbeats received",
	})

	UnhealthyWorkers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ctrlplane_unhealthy_workers",
		Help: "Number of nodes currently marked unhealthy",
	})
)
