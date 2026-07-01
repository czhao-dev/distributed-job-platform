package state

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/czhao-dev/control-plane/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ctx = context.Background()

func newTestDeployment(id string) *model.Deployment {
	return &model.Deployment{ID: id, Name: "test", Namespace: "default", Status: model.DeploymentPending, Replicas: 1}
}

func newTestPod(id, deploymentID string) *model.Pod {
	return &model.Pod{ID: id, DeploymentID: deploymentID, Namespace: "default", Status: model.PodPending}
}

func newTestNode(id string) *model.Node {
	return &model.Node{ID: id, Status: model.NodeRegistering}
}

func TestDeploymentCRUD(t *testing.T) {
	s := NewMemoryStore()
	d := newTestDeployment("d1")

	require.NoError(t, s.CreateDeployment(ctx, d))
	require.ErrorIs(t, s.CreateDeployment(ctx, d), ErrAlreadyExists)

	got, err := s.GetDeployment(ctx, "d1")
	require.NoError(t, err)
	assert.Equal(t, "test", got.Name)

	_, err = s.GetDeployment(ctx, "missing")
	require.ErrorIs(t, err, ErrNotFound)

	got.Name = "renamed"
	require.NoError(t, s.UpdateDeployment(ctx, got))
	got2, _ := s.GetDeployment(ctx, "d1")
	assert.Equal(t, "renamed", got2.Name)

	require.NoError(t, s.TransitionDeployment(ctx, "d1", model.DeploymentActive))
	require.ErrorIs(t, s.TransitionDeployment(ctx, "d1", model.DeploymentPending), ErrInvalidTransition)

	list, err := s.ListDeployments(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	require.NoError(t, s.DeleteDeployment(ctx, "d1"))
	require.ErrorIs(t, s.DeleteDeployment(ctx, "d1"), ErrNotFound)
}

func TestDeploymentsByNamespace(t *testing.T) {
	s := NewMemoryStore()
	d1 := newTestDeployment("d1")
	d1.Namespace = "default"
	d2 := newTestDeployment("d2")
	d2.Namespace = "production"
	require.NoError(t, s.CreateDeployment(ctx, d1))
	require.NoError(t, s.CreateDeployment(ctx, d2))

	list, _ := s.ListDeploymentsByNamespace(ctx, "default")
	assert.Len(t, list, 1)
	assert.Equal(t, "d1", list[0].ID)

	list, _ = s.ListDeploymentsByNamespace(ctx, "production")
	assert.Len(t, list, 1)
	assert.Equal(t, "d2", list[0].ID)
}

func TestPodCRUDAndTransition(t *testing.T) {
	s := NewMemoryStore()
	p := newTestPod("p1", "d1")
	require.NoError(t, s.CreatePod(ctx, p))
	require.ErrorIs(t, s.CreatePod(ctx, p), ErrAlreadyExists)

	require.NoError(t, s.TransitionPod(ctx, "p1", model.PodScheduled, ""))
	got, err := s.GetPod(ctx, "p1")
	require.NoError(t, err)
	require.NotNil(t, got.ScheduledAt)

	require.NoError(t, s.TransitionPod(ctx, "p1", model.PodRunning, ""))
	got, _ = s.GetPod(ctx, "p1")
	require.NotNil(t, got.StartedAt)

	require.ErrorIs(t, s.TransitionPod(ctx, "p1", model.PodDeadLetter, ""), ErrInvalidTransition)

	require.NoError(t, s.TransitionPod(ctx, "p1", model.PodFailed, "boom"))
	got, _ = s.GetPod(ctx, "p1")
	assert.Equal(t, "boom", got.Error)
	assert.Nil(t, got.FinishedAt, "FAILED is not terminal -- reconciler still decides retry vs dead-letter")

	require.NoError(t, s.TransitionPod(ctx, "p1", model.PodDeadLetter, ""))
	got, _ = s.GetPod(ctx, "p1")
	require.NotNil(t, got.FinishedAt)

	require.ErrorIs(t, s.TransitionPod(ctx, "missing", model.PodPending, ""), ErrNotFound)
}

func TestPodListByDeploymentStatusNode(t *testing.T) {
	s := NewMemoryStore()
	for i := 0; i < 3; i++ {
		p := newTestPod(fmt.Sprintf("p%d", i), "d1")
		p.NodeID = "node_x"
		require.NoError(t, s.CreatePod(ctx, p))
	}
	other := newTestPod("p_other", "d2")
	require.NoError(t, s.CreatePod(ctx, other))
	require.NoError(t, s.TransitionPod(ctx, "p_other", model.PodScheduled, ""))

	byDeployment, _ := s.ListPodsByDeployment(ctx, "d1")
	assert.Len(t, byDeployment, 3)

	byStatus, _ := s.ListPodsByStatus(ctx, model.PodPending)
	assert.Len(t, byStatus, 3)

	byNode, _ := s.ListPodsByNode(ctx, "node_x")
	assert.Len(t, byNode, 3)
}

func TestPodsByLabels(t *testing.T) {
	s := NewMemoryStore()

	p1 := newTestPod("p1", "d1")
	p1.Namespace = "default"
	p1.Labels = map[string]string{"app": "web", "env": "prod"}

	p2 := newTestPod("p2", "d1")
	p2.Namespace = "default"
	p2.Labels = map[string]string{"app": "worker"}

	p3 := newTestPod("p3", "d2")
	p3.Namespace = "staging"
	p3.Labels = map[string]string{"app": "web", "env": "staging"}

	require.NoError(t, s.CreatePod(ctx, p1))
	require.NoError(t, s.CreatePod(ctx, p2))
	require.NoError(t, s.CreatePod(ctx, p3))

	// empty selector matches all in namespace
	all, _ := s.ListPodsByLabels(ctx, "default", nil)
	assert.Len(t, all, 2)

	// label selector with namespace
	webProd, _ := s.ListPodsByLabels(ctx, "default", map[string]string{"app": "web"})
	assert.Len(t, webProd, 1)
	assert.Equal(t, "p1", webProd[0].ID)

	// multi-key AND
	both, _ := s.ListPodsByLabels(ctx, "default", map[string]string{"app": "web", "env": "prod"})
	assert.Len(t, both, 1)

	// namespace="" matches all namespaces
	crossNS, _ := s.ListPodsByLabels(ctx, "", map[string]string{"app": "web"})
	assert.Len(t, crossNS, 2)

	// no match
	none, _ := s.ListPodsByLabels(ctx, "default", map[string]string{"app": "db"})
	assert.Len(t, none, 0)
}

func TestNodesByLabels(t *testing.T) {
	s := NewMemoryStore()

	n1 := newTestNode("n1")
	n1.Labels = map[string]string{"role": "worker", "zone": "us-east-1a"}
	n2 := newTestNode("n2")
	n2.Labels = map[string]string{"role": "worker", "zone": "us-east-1b"}
	n3 := newTestNode("n3")
	n3.Labels = map[string]string{"role": "control-plane"}

	require.NoError(t, s.RegisterNode(ctx, n1))
	require.NoError(t, s.RegisterNode(ctx, n2))
	require.NoError(t, s.RegisterNode(ctx, n3))

	workers, _ := s.ListNodesByLabels(ctx, map[string]string{"role": "worker"})
	assert.Len(t, workers, 2)

	zoneA, _ := s.ListNodesByLabels(ctx, map[string]string{"role": "worker", "zone": "us-east-1a"})
	assert.Len(t, zoneA, 1)
	assert.Equal(t, "n1", zoneA[0].ID)

	// empty selector matches all
	all, _ := s.ListNodesByLabels(ctx, nil)
	assert.Len(t, all, 3)

	// no match
	none, _ := s.ListNodesByLabels(ctx, map[string]string{"role": "gpu"})
	assert.Len(t, none, 0)
}

func TestNodeRegisterAndTransition(t *testing.T) {
	s := NewMemoryStore()
	n := newTestNode("n1")
	require.NoError(t, s.RegisterNode(ctx, n))
	require.ErrorIs(t, s.RegisterNode(ctx, n), ErrAlreadyExists)

	require.NoError(t, s.TransitionNode(ctx, "n1", model.NodeHealthy))
	got, err := s.GetNode(ctx, "n1")
	require.NoError(t, err)
	assert.Equal(t, model.NodeHealthy, got.Status)

	require.ErrorIs(t, s.TransitionNode(ctx, "n1", model.NodeRemoved), ErrInvalidTransition)

	list, _ := s.ListNodes(ctx)
	assert.Len(t, list, 1)
}

func TestServiceCRUD(t *testing.T) {
	s := NewMemoryStore()
	svc := &model.Service{ID: "svc1", Name: "test", Namespace: "default", PathPrefix: "/x", Selector: map[string]string{"app": "web"}}
	require.NoError(t, s.UpsertService(ctx, svc))

	got, err := s.GetService(ctx, "svc1")
	require.NoError(t, err)
	assert.Equal(t, "/x", got.PathPrefix)
	assert.Equal(t, map[string]string{"app": "web"}, got.Selector)

	got.PathPrefix = "/y"
	require.NoError(t, s.UpsertService(ctx, got))
	got2, _ := s.GetService(ctx, "svc1")
	assert.Equal(t, "/y", got2.PathPrefix)

	list, _ := s.ListServices(ctx)
	assert.Len(t, list, 1)

	require.NoError(t, s.DeleteService(ctx, "svc1"))
	require.ErrorIs(t, s.DeleteService(ctx, "svc1"), ErrNotFound)
}

// TestStoreConcurrentAccess exercises Create/Get/Transition from many
// goroutines simultaneously against disjoint and shared IDs. Run with
// `go test -race` to confirm no data races.
func TestStoreConcurrentAccess(t *testing.T) {
	s := NewMemoryStore()
	const numGoroutines = 20
	const numPodsEach = 25

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numPodsEach; i++ {
				id := fmt.Sprintf("g%d_pod%d", g, i)
				pod := newTestPod(id, "d1")
				if err := s.CreatePod(ctx, pod); err != nil {
					t.Errorf("CreatePod(%s): %v", id, err)
					continue
				}
				if err := s.TransitionPod(ctx, id, model.PodScheduled, ""); err != nil {
					t.Errorf("TransitionPod(%s): %v", id, err)
				}
			}
		}()
	}
	wg.Wait()

	pods, _ := s.ListPodsByStatus(ctx, model.PodScheduled)
	assert.Len(t, pods, numGoroutines*numPodsEach)

	// Hammer a single shared pod concurrently to confirm Transition serializes.
	require.NoError(t, s.CreatePod(ctx, newTestPod("shared", "d1")))
	require.NoError(t, s.TransitionPod(ctx, "shared", model.PodScheduled, ""))

	var wg2 sync.WaitGroup
	successes := make([]bool, numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		g := g
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			err := s.TransitionPod(ctx, "shared", model.PodRunning, "")
			successes[g] = err == nil
		}()
	}
	wg2.Wait()

	successCount := 0
	for _, ok := range successes {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one goroutine should win the SCHEDULED->RUNNING transition")
}
