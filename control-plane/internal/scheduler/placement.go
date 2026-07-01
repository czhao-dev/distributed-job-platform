package scheduler

import (
	"errors"

	"github.com/czhao-dev/control-plane/internal/model"
)

// ErrNoCapacity is returned when no healthy node has enough available
// capacity to take on a pod.
var ErrNoCapacity = errors.New("no node with sufficient capacity")

// SelectNode picks the best healthy node for a pod requiring `required`
// resources among `candidates`, or returns ErrNoCapacity if none qualify.
//
// Policy: filter to HEALTHY nodes with enough available capacity and
// concurrency headroom, then pick the least-loaded one (lowest RunningJobs),
// breaking ties by earliest RegisteredAt for determinism. Pending pods are
// already processed in FIFO (CreatedAt) order by the caller, so this one
// function satisfies both "FIFO" and "resource-aware" scheduling requirements.
func SelectNode(candidates []*model.Node, required model.ResourceRequest) (*model.Node, error) {
	var best *model.Node
	for _, n := range candidates {
		if n.Status != model.NodeHealthy {
			continue
		}
		if !n.HasCapacityFor(required) {
			continue
		}
		if best == nil ||
			n.RunningJobs < best.RunningJobs ||
			(n.RunningJobs == best.RunningJobs && n.RegisteredAt.Before(best.RegisteredAt)) {
			best = n
		}
	}
	if best == nil {
		return nil, ErrNoCapacity
	}
	return best, nil
}
