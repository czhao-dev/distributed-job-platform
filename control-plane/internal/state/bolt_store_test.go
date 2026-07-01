package state

import (
	"path/filepath"
	"testing"

	"github.com/czhao-dev/control-plane/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBoltStore(t *testing.T) *BoltStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := NewBoltStore(path)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestBoltDeploymentCRUD(t *testing.T) {
	s := newBoltStore(t)
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

func TestBoltPodCRUDAndTransition(t *testing.T) {
	s := newBoltStore(t)
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
}

func TestBoltNodeRegisterAndTransition(t *testing.T) {
	s := newBoltStore(t)
	n := newTestNode("n1")
	require.NoError(t, s.RegisterNode(ctx, n))
	require.ErrorIs(t, s.RegisterNode(ctx, n), ErrAlreadyExists)

	require.NoError(t, s.TransitionNode(ctx, "n1", model.NodeHealthy))
	got, err := s.GetNode(ctx, "n1")
	require.NoError(t, err)
	assert.Equal(t, model.NodeHealthy, got.Status)

	require.ErrorIs(t, s.TransitionNode(ctx, "n1", model.NodeRemoved), ErrInvalidTransition)
}

func TestBoltNodesByLabels(t *testing.T) {
	s := newBoltStore(t)
	n1 := newTestNode("n1")
	n1.Labels = map[string]string{"role": "worker"}
	n2 := newTestNode("n2")
	n2.Labels = map[string]string{"role": "control-plane"}
	require.NoError(t, s.RegisterNode(ctx, n1))
	require.NoError(t, s.RegisterNode(ctx, n2))

	workers, _ := s.ListNodesByLabels(ctx, map[string]string{"role": "worker"})
	assert.Len(t, workers, 1)
	assert.Equal(t, "n1", workers[0].ID)
}

func TestBoltServiceCRUD(t *testing.T) {
	s := newBoltStore(t)
	svc := &model.Service{ID: "svc1", Name: "test", Namespace: "default", PathPrefix: "/x", Selector: map[string]string{"app": "web"}}
	require.NoError(t, s.UpsertService(ctx, svc))

	got, err := s.GetService(ctx, "svc1")
	require.NoError(t, err)
	assert.Equal(t, "/x", got.PathPrefix)
	assert.Equal(t, map[string]string{"app": "web"}, got.Selector)

	require.NoError(t, s.DeleteService(ctx, "svc1"))
	require.ErrorIs(t, s.DeleteService(ctx, "svc1"), ErrNotFound)
}

// TestBoltPersistence verifies that state survives closing and reopening the DB.
func TestBoltPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persist.db")

	// Write state
	s1, err := NewBoltStore(path)
	require.NoError(t, err)
	require.NoError(t, s1.CreateDeployment(ctx, newTestDeployment("d1")))
	require.NoError(t, s1.Close())

	// Reopen and verify
	s2, err := NewBoltStore(path)
	require.NoError(t, err)
	defer s2.Close()

	got, err := s2.GetDeployment(ctx, "d1")
	require.NoError(t, err)
	assert.Equal(t, "d1", got.ID)
	assert.Equal(t, "test", got.Name)
}
