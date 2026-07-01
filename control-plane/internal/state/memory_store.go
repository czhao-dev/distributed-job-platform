package state

import (
	"context"
	"sync"
	"time"

	"github.com/czhao-dev/control-plane/internal/model"
)

// MemoryStore is a concurrent, in-memory Store backed by sync.Map, one per
// entity type. A single coarse mutex guards all write paths: this is a
// low-throughput control plane (not a hot data path), and a shared mutex
// keeps cross-entity invariants simple to reason about. Reads are lock-free.
type MemoryStore struct {
	mu          sync.Mutex
	deployments sync.Map // id -> model.Deployment
	pods        sync.Map // id -> model.Pod
	nodes       sync.Map // id -> model.Node
	services    sync.Map // id -> model.Service
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

var _ Store = (*MemoryStore)(nil)

// --- Deployments ---

func (s *MemoryStore) CreateDeployment(_ context.Context, d *model.Deployment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.deployments.Load(d.ID); exists {
		return ErrAlreadyExists
	}
	s.deployments.Store(d.ID, d.Clone())
	return nil
}

func (s *MemoryStore) GetDeployment(_ context.Context, id string) (*model.Deployment, error) {
	v, ok := s.deployments.Load(id)
	if !ok {
		return nil, ErrNotFound
	}
	d := v.(model.Deployment).Clone()
	return &d, nil
}

func (s *MemoryStore) ListDeployments(_ context.Context) ([]*model.Deployment, error) {
	var out []*model.Deployment
	s.deployments.Range(func(_, v any) bool {
		d := v.(model.Deployment).Clone()
		out = append(out, &d)
		return true
	})
	return out, nil
}

func (s *MemoryStore) ListDeploymentsByNamespace(_ context.Context, namespace string) ([]*model.Deployment, error) {
	var out []*model.Deployment
	s.deployments.Range(func(_, v any) bool {
		d := v.(model.Deployment)
		if d.Namespace == namespace {
			c := d.Clone()
			out = append(out, &c)
		}
		return true
	})
	return out, nil
}

func (s *MemoryStore) UpdateDeployment(_ context.Context, d *model.Deployment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.deployments.Load(d.ID); !exists {
		return ErrNotFound
	}
	d.UpdatedAt = time.Now()
	s.deployments.Store(d.ID, d.Clone())
	return nil
}

func (s *MemoryStore) TransitionDeployment(_ context.Context, id string, to model.DeploymentStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.deployments.Load(id)
	if !ok {
		return ErrNotFound
	}
	d := v.(model.Deployment)
	if !model.TransitionDeployment(d.Status, to) {
		return ErrInvalidTransition
	}
	d.Status = to
	d.UpdatedAt = time.Now()
	s.deployments.Store(id, d)
	return nil
}

func (s *MemoryStore) DeleteDeployment(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.deployments.Load(id); !exists {
		return ErrNotFound
	}
	s.deployments.Delete(id)
	return nil
}

// --- Pods ---

func (s *MemoryStore) CreatePod(_ context.Context, p *model.Pod) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.pods.Load(p.ID); exists {
		return ErrAlreadyExists
	}
	s.pods.Store(p.ID, p.Clone())
	return nil
}

func (s *MemoryStore) GetPod(_ context.Context, id string) (*model.Pod, error) {
	v, ok := s.pods.Load(id)
	if !ok {
		return nil, ErrNotFound
	}
	p := v.(model.Pod).Clone()
	return &p, nil
}

func (s *MemoryStore) ListPodsByDeployment(_ context.Context, deploymentID string) ([]*model.Pod, error) {
	var out []*model.Pod
	s.pods.Range(func(_, v any) bool {
		p := v.(model.Pod)
		if p.DeploymentID == deploymentID {
			c := p.Clone()
			out = append(out, &c)
		}
		return true
	})
	return out, nil
}

func (s *MemoryStore) ListPodsByStatus(_ context.Context, status model.PodStatus) ([]*model.Pod, error) {
	var out []*model.Pod
	s.pods.Range(func(_, v any) bool {
		p := v.(model.Pod)
		if p.Status == status {
			c := p.Clone()
			out = append(out, &c)
		}
		return true
	})
	return out, nil
}

func (s *MemoryStore) ListPodsByNode(_ context.Context, nodeID string) ([]*model.Pod, error) {
	var out []*model.Pod
	s.pods.Range(func(_, v any) bool {
		p := v.(model.Pod)
		if p.NodeID == nodeID {
			c := p.Clone()
			out = append(out, &c)
		}
		return true
	})
	return out, nil
}

func (s *MemoryStore) ListPodsByLabels(_ context.Context, namespace string, selector map[string]string) ([]*model.Pod, error) {
	var out []*model.Pod
	s.pods.Range(func(_, v any) bool {
		p := v.(model.Pod)
		if (namespace == "" || p.Namespace == namespace) && matchesSelector(p.Labels, selector) {
			c := p.Clone()
			out = append(out, &c)
		}
		return true
	})
	return out, nil
}

// UpdatePod overwrites the stored pod wholesale. Callers own validating any
// state-transition rules before calling Update (TransitionPod is the
// validated alternative for state changes).
func (s *MemoryStore) UpdatePod(_ context.Context, p *model.Pod) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.pods.Load(p.ID); !exists {
		return ErrNotFound
	}
	s.pods.Store(p.ID, p.Clone())
	return nil
}

// TransitionPod validates and applies a status transition for pod id,
// updating ScheduledAt/StartedAt/FinishedAt and Error as part of the same
// atomic update.
func (s *MemoryStore) TransitionPod(_ context.Context, id string, to model.PodStatus, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.pods.Load(id)
	if !ok {
		return ErrNotFound
	}
	p := v.(model.Pod)

	if !model.TransitionPod(p.Status, to) {
		return ErrInvalidTransition
	}

	p.Status = to
	if errMsg != "" {
		p.Error = errMsg
	}

	now := time.Now()
	switch to {
	case model.PodScheduled:
		if p.ScheduledAt == nil {
			p.ScheduledAt = &now
		}
	case model.PodRunning:
		if p.StartedAt == nil {
			p.StartedAt = &now
		}
	}
	if isTerminalPodStatus(to) && p.FinishedAt == nil {
		p.FinishedAt = &now
	}

	s.pods.Store(id, p)
	return nil
}

func isTerminalPodStatus(s model.PodStatus) bool {
	switch s {
	case model.PodSucceeded, model.PodDeadLetter, model.PodCancelled:
		return true
	default:
		return false
	}
}

// --- Nodes ---

func (s *MemoryStore) RegisterNode(_ context.Context, n *model.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.nodes.Load(n.ID); exists {
		return ErrAlreadyExists
	}
	s.nodes.Store(n.ID, n.Clone())
	return nil
}

func (s *MemoryStore) GetNode(_ context.Context, id string) (*model.Node, error) {
	v, ok := s.nodes.Load(id)
	if !ok {
		return nil, ErrNotFound
	}
	n := v.(model.Node).Clone()
	return &n, nil
}

func (s *MemoryStore) ListNodes(_ context.Context) ([]*model.Node, error) {
	var out []*model.Node
	s.nodes.Range(func(_, v any) bool {
		n := v.(model.Node).Clone()
		out = append(out, &n)
		return true
	})
	return out, nil
}

func (s *MemoryStore) ListNodesByLabels(_ context.Context, selector map[string]string) ([]*model.Node, error) {
	var out []*model.Node
	s.nodes.Range(func(_, v any) bool {
		n := v.(model.Node)
		if matchesSelector(n.Labels, selector) {
			c := n.Clone()
			out = append(out, &c)
		}
		return true
	})
	return out, nil
}

func (s *MemoryStore) UpdateNode(_ context.Context, n *model.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.nodes.Load(n.ID); !exists {
		return ErrNotFound
	}
	s.nodes.Store(n.ID, n.Clone())
	return nil
}

func (s *MemoryStore) TransitionNode(_ context.Context, id string, to model.NodeStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.nodes.Load(id)
	if !ok {
		return ErrNotFound
	}
	n := v.(model.Node)
	if !model.TransitionNode(n.Status, to) {
		return ErrInvalidTransition
	}
	n.Status = to
	s.nodes.Store(id, n)
	return nil
}

// --- Services ---

func (s *MemoryStore) UpsertService(_ context.Context, svc *model.Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc.UpdatedAt = time.Now()
	s.services.Store(svc.ID, svc.Clone())
	return nil
}

func (s *MemoryStore) GetService(_ context.Context, id string) (*model.Service, error) {
	v, ok := s.services.Load(id)
	if !ok {
		return nil, ErrNotFound
	}
	svc := v.(model.Service).Clone()
	return &svc, nil
}

func (s *MemoryStore) ListServices(_ context.Context) ([]*model.Service, error) {
	var out []*model.Service
	s.services.Range(func(_, v any) bool {
		svc := v.(model.Service).Clone()
		out = append(out, &svc)
		return true
	})
	return out, nil
}

func (s *MemoryStore) DeleteService(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.services.Load(id); !exists {
		return ErrNotFound
	}
	s.services.Delete(id)
	return nil
}
