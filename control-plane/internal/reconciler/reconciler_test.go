package reconciler

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

func TestReconciler_UnderstaffedDeploymentCreatesMissingPods(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()
	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{
		ID: "d1", Namespace: "default", Status: model.DeploymentPending, Replicas: 3, Command: "echo",
	}))

	rc := New(st, time.Hour, time.Hour, testLogger())
	rc.Tick(ctx)

	d, err := st.GetDeployment(ctx, "d1")
	require.NoError(t, err)
	assert.Equal(t, model.DeploymentActive, d.Status, "reconciler activates a PENDING deployment on first tick")

	pods, _ := st.ListPodsByDeployment(ctx, "d1")
	assert.Len(t, pods, 3)
	for _, p := range pods {
		assert.Equal(t, model.PodPending, p.Status)
		assert.Equal(t, "echo", p.Command)
	}

	// A second tick must not over-create pods.
	rc.Tick(ctx)
	pods, _ = st.ListPodsByDeployment(ctx, "d1")
	assert.Len(t, pods, 3)
}

func TestReconciler_PodInheritsDeploymentNamespaceAndLabels(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()
	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{
		ID: "d1", Namespace: "production", Labels: map[string]string{"app": "web", "env": "prod"},
		Status: model.DeploymentPending, Replicas: 1, Command: "server",
	}))

	rc := New(st, time.Hour, time.Hour, testLogger())
	rc.Tick(ctx)

	pods, _ := st.ListPodsByDeployment(ctx, "d1")
	require.Len(t, pods, 1)
	assert.Equal(t, "production", pods[0].Namespace)
	assert.Equal(t, map[string]string{"app": "web", "env": "prod"}, pods[0].Labels)
}

func TestReconciler_ScaleDownCancelsExcessPendingPods(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()
	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{ID: "d1", Namespace: "default", Status: model.DeploymentActive, Replicas: 1}))
	for i := 0; i < 3; i++ {
		require.NoError(t, st.CreatePod(ctx, &model.Pod{
			ID: "p" + string(rune('a'+i)), DeploymentID: "d1", Namespace: "default", Status: model.PodPending,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}))
	}

	rc := New(st, time.Hour, time.Hour, testLogger())
	rc.Tick(ctx)

	pods, _ := st.ListPodsByDeployment(ctx, "d1")
	active := 0
	cancelled := 0
	for _, p := range pods {
		if p.Active() {
			active++
		}
		if p.Status == model.PodCancelled {
			cancelled++
		}
	}
	assert.Equal(t, 1, active, "scale-down must leave exactly Replicas pods active")
	assert.Equal(t, 2, cancelled)
}

func TestReconciler_ScaleDownNeverCancelsRunningPods(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()
	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{ID: "d1", Namespace: "default", Status: model.DeploymentActive, Replicas: 0}))
	require.NoError(t, st.CreatePod(ctx, &model.Pod{ID: "p1", DeploymentID: "d1", Namespace: "default", Status: model.PodPending, CreatedAt: time.Now()}))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodScheduled, ""))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodRunning, ""))

	rc := New(st, time.Hour, time.Hour, testLogger())
	rc.Tick(ctx)

	pod, _ := st.GetPod(ctx, "p1")
	assert.Equal(t, model.PodRunning, pod.Status, "a RUNNING pod must never be cancelled by scale-down")
}

func TestReconciler_DeadLetteredPodMarksDeploymentDegraded(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()
	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{ID: "d1", Namespace: "default", Status: model.DeploymentActive, Replicas: 1}))
	require.NoError(t, st.CreatePod(ctx, &model.Pod{ID: "p1", DeploymentID: "d1", Namespace: "default", Status: model.PodPending, CreatedAt: time.Now()}))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodScheduled, ""))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodRunning, ""))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodFailed, "boom"))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodDeadLetter, ""))

	rc := New(st, time.Hour, time.Hour, testLogger())
	rc.Tick(ctx)

	d, _ := st.GetDeployment(ctx, "d1")
	assert.Equal(t, model.DeploymentDegraded, d.Status)

	// A replacement pod should have been created to refill the dead-lettered slot.
	pods, _ := st.ListPodsByDeployment(ctx, "d1")
	assert.Len(t, pods, 2)
}

func TestReconciler_NodeHeartbeatTimeoutMarksUnhealthyAndRequeuesRunningPod(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()
	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{ID: "d1", Namespace: "default", Status: model.DeploymentActive, Replicas: 1, MaxRetries: 2}))
	require.NoError(t, st.CreatePod(ctx, &model.Pod{ID: "p1", DeploymentID: "d1", NodeID: "n1", Namespace: "default", Status: model.PodPending, CreatedAt: time.Now()}))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodScheduled, ""))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodRunning, ""))

	require.NoError(t, st.RegisterNode(ctx, &model.Node{
		ID: "n1", Status: model.NodeRegistering, LastHeartbeatAt: time.Now().Add(-time.Hour),
	}))
	require.NoError(t, st.TransitionNode(ctx, "n1", model.NodeHealthy))

	rc := New(st, time.Hour, 10*time.Second, testLogger())
	rc.Tick(ctx)

	node, _ := st.GetNode(ctx, "n1")
	assert.Equal(t, model.NodeUnhealthy, node.Status)

	pod, _ := st.GetPod(ctx, "p1")
	assert.Equal(t, model.PodPending, pod.Status, "running pod on a timed-out node is requeued to PENDING")
	assert.Equal(t, 1, pod.Attempt)
	assert.True(t, pod.RunAfter.After(time.Now()), "requeued pod should have a backoff RunAfter set")
}

func TestReconciler_RunningPodExceedingMaxRetriesGoesDeadLetter(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()
	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{ID: "d1", Namespace: "default", Status: model.DeploymentActive, Replicas: 1, MaxRetries: 0}))
	require.NoError(t, st.CreatePod(ctx, &model.Pod{ID: "p1", DeploymentID: "d1", NodeID: "n1", Namespace: "default", Status: model.PodPending, CreatedAt: time.Now()}))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodScheduled, ""))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodRunning, ""))

	require.NoError(t, st.RegisterNode(ctx, &model.Node{
		ID: "n1", Status: model.NodeRegistering, LastHeartbeatAt: time.Now().Add(-time.Hour),
	}))
	require.NoError(t, st.TransitionNode(ctx, "n1", model.NodeHealthy))

	rc := New(st, time.Hour, 10*time.Second, testLogger())
	rc.Tick(ctx)

	pod, _ := st.GetPod(ctx, "p1")
	assert.Equal(t, model.PodDeadLetter, pod.Status, "with MaxRetries=0, a single failure exhausts the budget")
}

func TestReconciler_OrphanedScheduledPodRequeuedWithoutBurningRetry(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()
	require.NoError(t, st.CreateDeployment(ctx, &model.Deployment{ID: "d1", Namespace: "default", Status: model.DeploymentActive, Replicas: 1, MaxRetries: 2}))
	require.NoError(t, st.CreatePod(ctx, &model.Pod{ID: "p1", DeploymentID: "d1", NodeID: "n1", Namespace: "default", Status: model.PodPending, CreatedAt: time.Now()}))
	require.NoError(t, st.TransitionPod(ctx, "p1", model.PodScheduled, ""))
	// Node died between dispatch and pickup: pod never reached RUNNING.

	require.NoError(t, st.RegisterNode(ctx, &model.Node{
		ID: "n1", Status: model.NodeRegistering, LastHeartbeatAt: time.Now().Add(-time.Hour),
	}))
	require.NoError(t, st.TransitionNode(ctx, "n1", model.NodeHealthy))

	rc := New(st, time.Hour, 10*time.Second, testLogger())
	rc.Tick(ctx)

	pod, _ := st.GetPod(ctx, "p1")
	assert.Equal(t, model.PodPending, pod.Status, "an orphaned SCHEDULED pod must be requeued, not left pointing at a dead node")
	assert.Equal(t, 0, pod.Attempt, "a pod that never ran must not burn a retry attempt")
	assert.Equal(t, "", pod.NodeID)
}

func TestReconciler_DrainingNodeTimeoutBecomesRemoved(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemoryStore()
	require.NoError(t, st.RegisterNode(ctx, &model.Node{
		ID: "n1", Status: model.NodeRegistering, LastHeartbeatAt: time.Now().Add(-time.Hour),
	}))
	require.NoError(t, st.TransitionNode(ctx, "n1", model.NodeHealthy))
	require.NoError(t, st.TransitionNode(ctx, "n1", model.NodeDraining))

	rc := New(st, time.Hour, 10*time.Second, testLogger())
	rc.Tick(ctx)

	node, _ := st.GetNode(ctx, "n1")
	assert.Equal(t, model.NodeRemoved, node.Status, "a draining node that times out is removed, not marked unhealthy")
}
