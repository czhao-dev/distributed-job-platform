package scheduler

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/czhao-dev/control-plane/internal/metrics"
	"github.com/czhao-dev/control-plane/internal/model"
	"github.com/czhao-dev/control-plane/internal/state"
)

// Scheduler assigns PENDING pods to HEALTHY nodes with available capacity
// on a fixed poll interval.
type Scheduler struct {
	store    state.Store
	interval time.Duration
	logger   *slog.Logger

	mu    sync.Mutex
	stats Stats
}

// Stats is a snapshot of the scheduler's most recent tick, exposed via
// GET /api/v1/scheduler/stats.
type Stats struct {
	LastTickAt    time.Time `json:"last_tick_at"`
	ScheduledLast int       `json:"scheduled_last_tick"`
	PendingNow    int       `json:"pending_now"`
}

func New(st state.Store, interval time.Duration, logger *slog.Logger) *Scheduler {
	return &Scheduler{store: st, interval: interval, logger: logger}
}

// Run blocks, ticking every interval until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

// Tick runs one scheduling pass. Exported so the "rebalance" API and tests
// can trigger it synchronously outside the regular ticker cadence.
func (s *Scheduler) Tick(ctx context.Context) {
	pending, err := s.store.ListPodsByStatus(ctx, model.PodPending)
	if err != nil {
		s.logger.Error("scheduler: list pending pods", "error", err)
		return
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt.Before(pending[j].CreatedAt) })

	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		s.logger.Error("scheduler: list nodes", "error", err)
		return
	}

	now := time.Now()
	scheduled := 0
	for _, pod := range pending {
		if !pod.RunAfter.IsZero() && pod.RunAfter.After(now) {
			continue // backoff gate set by the reconciler; not ready yet
		}

		var required model.ResourceRequest
		if deployment, err := s.store.GetDeployment(ctx, pod.DeploymentID); err == nil {
			required = deployment.Resources
		}

		node, err := SelectNode(nodes, required)
		if err != nil {
			continue // no capacity right now; retry next tick
		}

		// Set NodeID via UpdatePod first (pod.Status still matches the
		// stored PENDING value here), then flip status via TransitionPod --
		// doing it in the other order would have TransitionPod's internal
		// load/modify/store clobber the NodeID set on this local copy.
		pod.NodeID = node.ID
		if err := s.store.UpdatePod(ctx, pod); err != nil {
			s.logger.Warn("scheduler: assign node", "pod_id", pod.ID, "error", err)
			continue
		}
		if err := s.store.TransitionPod(ctx, pod.ID, model.PodScheduled, ""); err != nil {
			s.logger.Warn("scheduler: transition pod", "pod_id", pod.ID, "error", err)
			continue
		}

		node.Available.CPU -= required.CPU
		node.Available.MemoryMB -= required.MemoryMB
		node.RunningJobs++
		if err := s.store.UpdateNode(ctx, node); err != nil {
			s.logger.Warn("scheduler: update node capacity", "node_id", node.ID, "error", err)
		}

		metrics.SchedulerLatencySeconds.Observe(time.Since(pod.CreatedAt).Seconds())
		scheduled++
	}

	metrics.SchedulerQueueDepth.Set(float64(len(pending) - scheduled))

	s.mu.Lock()
	s.stats = Stats{LastTickAt: time.Now(), ScheduledLast: scheduled, PendingNow: len(pending) - scheduled}
	s.mu.Unlock()
}

func (s *Scheduler) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}
