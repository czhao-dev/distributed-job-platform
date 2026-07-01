package scheduler

import (
	"testing"
	"time"

	"github.com/czhao-dev/control-plane/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectNode_NoCandidates(t *testing.T) {
	_, err := SelectNode(nil, model.ResourceRequest{})
	assert.ErrorIs(t, err, ErrNoCapacity)
}

func TestSelectNode_AllUnhealthy(t *testing.T) {
	candidates := []*model.Node{
		{ID: "n1", Status: model.NodeUnhealthy, Available: model.ResourceCapacity{CPU: 10, MemoryMB: 10000}},
		{ID: "n2", Status: model.NodeDraining, Available: model.ResourceCapacity{CPU: 10, MemoryMB: 10000}},
	}
	_, err := SelectNode(candidates, model.ResourceRequest{CPU: 1, MemoryMB: 1})
	assert.ErrorIs(t, err, ErrNoCapacity)
}

func TestSelectNode_AllOverCapacity(t *testing.T) {
	candidates := []*model.Node{
		{ID: "n1", Status: model.NodeHealthy, Available: model.ResourceCapacity{CPU: 0.1, MemoryMB: 10}},
	}
	_, err := SelectNode(candidates, model.ResourceRequest{CPU: 1, MemoryMB: 100})
	assert.ErrorIs(t, err, ErrNoCapacity)
}

func TestSelectNode_ExactFit(t *testing.T) {
	candidates := []*model.Node{
		{ID: "n1", Status: model.NodeHealthy, Available: model.ResourceCapacity{CPU: 1, MemoryMB: 512}},
	}
	n, err := SelectNode(candidates, model.ResourceRequest{CPU: 1, MemoryMB: 512})
	require.NoError(t, err)
	assert.Equal(t, "n1", n.ID)
}

func TestSelectNode_PicksLeastLoaded(t *testing.T) {
	candidates := []*model.Node{
		{ID: "busy", Status: model.NodeHealthy, RunningJobs: 5, Available: model.ResourceCapacity{CPU: 10, MemoryMB: 10000}},
		{ID: "idle", Status: model.NodeHealthy, RunningJobs: 1, Available: model.ResourceCapacity{CPU: 10, MemoryMB: 10000}},
	}
	n, err := SelectNode(candidates, model.ResourceRequest{})
	require.NoError(t, err)
	assert.Equal(t, "idle", n.ID)
}

func TestSelectNode_TieBreaksByRegisteredAt(t *testing.T) {
	now := time.Now()
	candidates := []*model.Node{
		{ID: "newer", Status: model.NodeHealthy, RunningJobs: 1, RegisteredAt: now.Add(time.Minute), Available: model.ResourceCapacity{CPU: 10, MemoryMB: 10000}},
		{ID: "older", Status: model.NodeHealthy, RunningJobs: 1, RegisteredAt: now, Available: model.ResourceCapacity{CPU: 10, MemoryMB: 10000}},
	}
	n, err := SelectNode(candidates, model.ResourceRequest{})
	require.NoError(t, err)
	assert.Equal(t, "older", n.ID)
}

func TestSelectNode_SkipsMaxConcurrency(t *testing.T) {
	candidates := []*model.Node{
		{ID: "full", Status: model.NodeHealthy, RunningJobs: 2, MaxConcurrent: 2, Available: model.ResourceCapacity{CPU: 10, MemoryMB: 10000}},
		{ID: "ok", Status: model.NodeHealthy, RunningJobs: 1, MaxConcurrent: 2, Available: model.ResourceCapacity{CPU: 10, MemoryMB: 10000}},
	}
	n, err := SelectNode(candidates, model.ResourceRequest{})
	require.NoError(t, err)
	assert.Equal(t, "ok", n.ID)
}
