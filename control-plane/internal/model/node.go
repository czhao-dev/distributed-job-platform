package model

import "time"

// NodeStatus is the lifecycle state of a registered execution node.
type NodeStatus string

const (
	NodeRegistering NodeStatus = "REGISTERING"
	NodeHealthy     NodeStatus = "HEALTHY"
	NodeDraining    NodeStatus = "DRAINING"
	NodeUnhealthy   NodeStatus = "UNHEALTHY"
	NodeRemoved     NodeStatus = "REMOVED"
)

var allowedNodeTransitions = map[NodeStatus][]NodeStatus{
	NodeRegistering: {NodeHealthy, NodeUnhealthy},
	NodeHealthy:     {NodeDraining, NodeUnhealthy},
	NodeDraining:    {NodeRemoved, NodeUnhealthy},
	// A node recovers (resumes heartbeating) before the reconciler removes it.
	NodeUnhealthy: {NodeHealthy, NodeRemoved},
	NodeRemoved:   {},
}

// TransitionNode reports whether moving a node from `from` to `to` is legal.
func TransitionNode(from, to NodeStatus) bool {
	for _, s := range allowedNodeTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// ResourceCapacity describes a node's total or currently-available resources.
type ResourceCapacity struct {
	CPU      float64 `json:"cpu"`
	MemoryMB int     `json:"memory_mb"`
}

// Node represents a registered execution node (a running node agent process).
// Nodes are cluster-scoped (no Namespace field); only Deployments, Pods, and Services
// are namespaced.
type Node struct {
	ID              string            `json:"id"`
	Hostname        string            `json:"hostname"`
	Address         string            `json:"address"`
	Labels          map[string]string `json:"labels,omitempty"`
	Status          NodeStatus        `json:"status"`
	Capacity        ResourceCapacity  `json:"capacity"`
	Available       ResourceCapacity  `json:"available"`
	RunningJobs     int               `json:"running_jobs"`
	MaxConcurrent   int               `json:"max_concurrent_jobs"`
	LastHeartbeatAt time.Time         `json:"last_heartbeat_at"`
	RegisteredAt    time.Time         `json:"registered_at"`
}

// Clone returns a deep copy of the Node.
func (n Node) Clone() Node {
	c := n
	if n.Labels != nil {
		c.Labels = make(map[string]string, len(n.Labels))
		for k, v := range n.Labels {
			c.Labels[k] = v
		}
	}
	return c
}

// HasCapacityFor reports whether the node currently has enough available
// resources and concurrency headroom to take on a pod requiring `req`.
func (n Node) HasCapacityFor(req ResourceRequest) bool {
	if n.MaxConcurrent > 0 && n.RunningJobs >= n.MaxConcurrent {
		return false
	}
	return n.Available.CPU >= req.CPU && n.Available.MemoryMB >= req.MemoryMB
}
