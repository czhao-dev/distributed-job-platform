package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTransitionNode(t *testing.T) {
	allStatuses := []NodeStatus{
		NodeRegistering, NodeHealthy, NodeDraining, NodeUnhealthy, NodeRemoved,
	}

	valid := map[NodeStatus]map[NodeStatus]bool{
		NodeRegistering: {NodeHealthy: true, NodeUnhealthy: true},
		NodeHealthy:     {NodeDraining: true, NodeUnhealthy: true},
		NodeDraining:    {NodeRemoved: true, NodeUnhealthy: true},
		NodeUnhealthy:   {NodeHealthy: true, NodeRemoved: true},
		NodeRemoved:     {},
	}

	for _, from := range allStatuses {
		for _, to := range allStatuses {
			from, to := from, to
			want := valid[from][to]
			t.Run(fmt.Sprintf("%s_to_%s", from, to), func(t *testing.T) {
				assert.Equal(t, want, TransitionNode(from, to))
			})
		}
	}
}

func TestNodeHasCapacityFor(t *testing.T) {
	n := Node{
		Available:     ResourceCapacity{CPU: 1, MemoryMB: 512},
		RunningJobs:   2,
		MaxConcurrent: 2,
	}
	assert.False(t, n.HasCapacityFor(ResourceRequest{CPU: 0.1, MemoryMB: 1}), "at max concurrency, no capacity regardless of resources")

	n.RunningJobs = 1
	assert.True(t, n.HasCapacityFor(ResourceRequest{CPU: 1, MemoryMB: 512}))
	assert.False(t, n.HasCapacityFor(ResourceRequest{CPU: 1.1, MemoryMB: 1}))
	assert.False(t, n.HasCapacityFor(ResourceRequest{CPU: 0.1, MemoryMB: 513}))

	n.MaxConcurrent = 0 // unlimited concurrency
	assert.True(t, n.HasCapacityFor(ResourceRequest{CPU: 1, MemoryMB: 512}))
}

func TestNodeClone(t *testing.T) {
	n := Node{ID: "n1", Labels: map[string]string{"role": "worker"}}
	clone := n.Clone()
	clone.Labels["role"] = "mutated"
	assert.Equal(t, "worker", n.Labels["role"], "mutating clone Labels must not affect original")
}
