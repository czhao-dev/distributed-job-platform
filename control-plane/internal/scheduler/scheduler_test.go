package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/czhao-dev/control-plane/internal/model"
	"github.com/czhao-dev/control-plane/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestScheduler_AssignsPendingPodToHealthyNode(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()

	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{
		ID: "d1", Namespace: "default", Resources: model.ResourceRequest{CPU: 1, MemoryMB: 256},
	}))
	require.NoError(t, st.CreatePod(ctx, &model.Pod{ID: "p1", DeploymentID: "d1", Namespace: "default", Status: model.PodPending, CreatedAt: time.Now()}))
	require.NoError(t, st.RegisterNode(ctx, &model.Node{
		ID: "n1", Status: model.NodeRegistering, Capacity: model.ResourceCapacity{CPU: 2, MemoryMB: 1024}, Available: model.ResourceCapacity{CPU: 2, MemoryMB: 1024},
	}))
	require.NoError(t, st.TransitionNode(ctx, "n1", model.NodeHealthy))

	sched := New(st, time.Hour, testLogger())
	sched.Tick(ctx)

	pod, err := st.GetPod(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, model.PodScheduled, pod.Status)
	assert.Equal(t, "n1", pod.NodeID)

	node, _ := st.GetNode(ctx, "n1")
	assert.Equal(t, 1, node.RunningJobs)
	assert.Equal(t, 1.0, node.Available.CPU)
	assert.Equal(t, 768, node.Available.MemoryMB)

	stats := sched.Stats()
	assert.Equal(t, 1, stats.ScheduledLast)
	assert.Equal(t, 0, stats.PendingNow)
}

func TestScheduler_LeavesPodPendingWhenNoCapacity(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()

	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{ID: "d1", Namespace: "default", Resources: model.ResourceRequest{CPU: 4, MemoryMB: 4096}}))
	require.NoError(t, st.CreatePod(ctx, &model.Pod{ID: "p1", DeploymentID: "d1", Namespace: "default", Status: model.PodPending, CreatedAt: time.Now()}))
	require.NoError(t, st.RegisterNode(ctx, &model.Node{
		ID: "n1", Status: model.NodeRegistering, Capacity: model.ResourceCapacity{CPU: 1, MemoryMB: 512}, Available: model.ResourceCapacity{CPU: 1, MemoryMB: 512},
	}))
	require.NoError(t, st.TransitionNode(ctx, "n1", model.NodeHealthy))

	sched := New(st, time.Hour, testLogger())
	sched.Tick(ctx)

	pod, _ := st.GetPod(ctx, "p1")
	assert.Equal(t, model.PodPending, pod.Status)
}

func TestScheduler_RespectsRunAfterBackoffGate(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()

	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{ID: "d1", Namespace: "default"}))
	require.NoError(t, st.CreatePod(ctx, &model.Pod{
		ID: "p1", DeploymentID: "d1", Namespace: "default", Status: model.PodPending, CreatedAt: time.Now(),
		RunAfter: time.Now().Add(time.Hour),
	}))
	require.NoError(t, st.RegisterNode(ctx, &model.Node{ID: "n1", Status: model.NodeRegistering, Available: model.ResourceCapacity{CPU: 10, MemoryMB: 10000}}))
	require.NoError(t, st.TransitionNode(ctx, "n1", model.NodeHealthy))

	sched := New(st, time.Hour, testLogger())
	sched.Tick(ctx)

	pod, _ := st.GetPod(ctx, "p1")
	assert.Equal(t, model.PodPending, pod.Status, "pod with future RunAfter must not be scheduled yet")
}
