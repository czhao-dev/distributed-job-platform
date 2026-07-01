package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/czhao-dev/control-plane/internal/model"
	bolt "go.etcd.io/bbolt"
)

// BoltStore is a persistent Store backed by an embedded BoltDB database.
// It implements the same Store interface as MemoryStore and is a drop-in
// replacement that survives process restarts.
//
// Design notes:
//   - One BoltDB bucket per entity type (deployments, pods, nodes, services).
//   - Entities are JSON-serialized, consistent with the HTTP API wire format.
//   - State-machine transitions are applied atomically within a single db.Update
//     transaction (read → validate → write), matching MemoryStore semantics.
//   - This is an embedded single-node store — no Raft, no distributed consensus.
//     It is an intentional simplification of etcd for demo/learning purposes.
type BoltStore struct {
	db *bolt.DB
}

var (
	bucketDeployments = []byte("deployments")
	bucketPods        = []byte("pods")
	bucketNodes       = []byte("nodes")
	bucketServices    = []byte("services")
)

// NewBoltStore opens (or creates) a BoltDB database at path and initialises
// the four entity buckets. The caller is responsible for closing the DB when
// the process exits (typically via a deferred db.Close() in main).
func NewBoltStore(path string) (*BoltStore, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bolt db %s: %w", path, err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketDeployments, bucketPods, bucketNodes, bucketServices} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init bolt buckets: %w", err)
	}
	return &BoltStore{db: db}, nil
}

// Close releases the BoltDB file lock. Call this on process shutdown.
func (s *BoltStore) Close() error { return s.db.Close() }

var _ Store = (*BoltStore)(nil)

// --- helpers ---

func bget[T any](tx *bolt.Tx, bucket []byte, id string) (T, error) {
	var zero T
	b := tx.Bucket(bucket)
	raw := b.Get([]byte(id))
	if raw == nil {
		return zero, ErrNotFound
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return zero, err
	}
	return v, nil
}

func bput(tx *bolt.Tx, bucket []byte, id string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return tx.Bucket(bucket).Put([]byte(id), raw)
}

func bdelete(tx *bolt.Tx, bucket []byte, id string) error {
	b := tx.Bucket(bucket)
	if b.Get([]byte(id)) == nil {
		return ErrNotFound
	}
	return b.Delete([]byte(id))
}

// --- Deployments ---

func (s *BoltStore) CreateDeployment(_ context.Context, d *model.Deployment) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketDeployments).Get([]byte(d.ID)) != nil {
			return ErrAlreadyExists
		}
		return bput(tx, bucketDeployments, d.ID, d.Clone())
	})
}

func (s *BoltStore) GetDeployment(_ context.Context, id string) (*model.Deployment, error) {
	var out model.Deployment
	err := s.db.View(func(tx *bolt.Tx) error {
		v, err := bget[model.Deployment](tx, bucketDeployments, id)
		if err != nil {
			return err
		}
		out = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *BoltStore) ListDeployments(_ context.Context) ([]*model.Deployment, error) {
	var out []*model.Deployment
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketDeployments).ForEach(func(_, v []byte) error {
			var d model.Deployment
			if err := json.Unmarshal(v, &d); err != nil {
				return err
			}
			out = append(out, &d)
			return nil
		})
	})
	return out, err
}

func (s *BoltStore) ListDeploymentsByNamespace(_ context.Context, namespace string) ([]*model.Deployment, error) {
	var out []*model.Deployment
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketDeployments).ForEach(func(_, v []byte) error {
			var d model.Deployment
			if err := json.Unmarshal(v, &d); err != nil {
				return err
			}
			if d.Namespace == namespace {
				out = append(out, &d)
			}
			return nil
		})
	})
	return out, err
}

func (s *BoltStore) UpdateDeployment(_ context.Context, d *model.Deployment) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketDeployments).Get([]byte(d.ID)) == nil {
			return ErrNotFound
		}
		d.UpdatedAt = time.Now()
		return bput(tx, bucketDeployments, d.ID, d.Clone())
	})
}

func (s *BoltStore) TransitionDeployment(_ context.Context, id string, to model.DeploymentStatus) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		d, err := bget[model.Deployment](tx, bucketDeployments, id)
		if err != nil {
			return err
		}
		if !model.TransitionDeployment(d.Status, to) {
			return ErrInvalidTransition
		}
		d.Status = to
		d.UpdatedAt = time.Now()
		return bput(tx, bucketDeployments, id, d)
	})
}

func (s *BoltStore) DeleteDeployment(_ context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return bdelete(tx, bucketDeployments, id)
	})
}

// --- Pods ---

func (s *BoltStore) CreatePod(_ context.Context, p *model.Pod) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketPods).Get([]byte(p.ID)) != nil {
			return ErrAlreadyExists
		}
		return bput(tx, bucketPods, p.ID, p.Clone())
	})
}

func (s *BoltStore) GetPod(_ context.Context, id string) (*model.Pod, error) {
	var out model.Pod
	err := s.db.View(func(tx *bolt.Tx) error {
		v, err := bget[model.Pod](tx, bucketPods, id)
		if err != nil {
			return err
		}
		out = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *BoltStore) ListPodsByDeployment(_ context.Context, deploymentID string) ([]*model.Pod, error) {
	var out []*model.Pod
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPods).ForEach(func(_, v []byte) error {
			var p model.Pod
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			if p.DeploymentID == deploymentID {
				out = append(out, &p)
			}
			return nil
		})
	})
	return out, err
}

func (s *BoltStore) ListPodsByStatus(_ context.Context, status model.PodStatus) ([]*model.Pod, error) {
	var out []*model.Pod
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPods).ForEach(func(_, v []byte) error {
			var p model.Pod
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			if p.Status == status {
				out = append(out, &p)
			}
			return nil
		})
	})
	return out, err
}

func (s *BoltStore) ListPodsByNode(_ context.Context, nodeID string) ([]*model.Pod, error) {
	var out []*model.Pod
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPods).ForEach(func(_, v []byte) error {
			var p model.Pod
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			if p.NodeID == nodeID {
				out = append(out, &p)
			}
			return nil
		})
	})
	return out, err
}

func (s *BoltStore) ListPodsByLabels(_ context.Context, namespace string, selector map[string]string) ([]*model.Pod, error) {
	var out []*model.Pod
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPods).ForEach(func(_, v []byte) error {
			var p model.Pod
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			if (namespace == "" || p.Namespace == namespace) && matchesSelector(p.Labels, selector) {
				out = append(out, &p)
			}
			return nil
		})
	})
	return out, err
}

func (s *BoltStore) UpdatePod(_ context.Context, p *model.Pod) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketPods).Get([]byte(p.ID)) == nil {
			return ErrNotFound
		}
		return bput(tx, bucketPods, p.ID, p.Clone())
	})
}

func (s *BoltStore) TransitionPod(_ context.Context, id string, to model.PodStatus, errMsg string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		p, err := bget[model.Pod](tx, bucketPods, id)
		if err != nil {
			return err
		}
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
		return bput(tx, bucketPods, id, p)
	})
}

// --- Nodes ---

func (s *BoltStore) RegisterNode(_ context.Context, n *model.Node) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketNodes).Get([]byte(n.ID)) != nil {
			return ErrAlreadyExists
		}
		return bput(tx, bucketNodes, n.ID, n.Clone())
	})
}

func (s *BoltStore) GetNode(_ context.Context, id string) (*model.Node, error) {
	var out model.Node
	err := s.db.View(func(tx *bolt.Tx) error {
		v, err := bget[model.Node](tx, bucketNodes, id)
		if err != nil {
			return err
		}
		out = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *BoltStore) ListNodes(_ context.Context) ([]*model.Node, error) {
	var out []*model.Node
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketNodes).ForEach(func(_, v []byte) error {
			var n model.Node
			if err := json.Unmarshal(v, &n); err != nil {
				return err
			}
			out = append(out, &n)
			return nil
		})
	})
	return out, err
}

func (s *BoltStore) ListNodesByLabels(_ context.Context, selector map[string]string) ([]*model.Node, error) {
	var out []*model.Node
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketNodes).ForEach(func(_, v []byte) error {
			var n model.Node
			if err := json.Unmarshal(v, &n); err != nil {
				return err
			}
			if matchesSelector(n.Labels, selector) {
				out = append(out, &n)
			}
			return nil
		})
	})
	return out, err
}

func (s *BoltStore) UpdateNode(_ context.Context, n *model.Node) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketNodes).Get([]byte(n.ID)) == nil {
			return ErrNotFound
		}
		return bput(tx, bucketNodes, n.ID, n.Clone())
	})
}

func (s *BoltStore) TransitionNode(_ context.Context, id string, to model.NodeStatus) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		n, err := bget[model.Node](tx, bucketNodes, id)
		if err != nil {
			return err
		}
		if !model.TransitionNode(n.Status, to) {
			return ErrInvalidTransition
		}
		n.Status = to
		return bput(tx, bucketNodes, id, n)
	})
}

// --- Services ---

func (s *BoltStore) UpsertService(_ context.Context, svc *model.Service) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		svc.UpdatedAt = time.Now()
		return bput(tx, bucketServices, svc.ID, svc.Clone())
	})
}

func (s *BoltStore) GetService(_ context.Context, id string) (*model.Service, error) {
	var out model.Service
	err := s.db.View(func(tx *bolt.Tx) error {
		v, err := bget[model.Service](tx, bucketServices, id)
		if err != nil {
			return err
		}
		out = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *BoltStore) ListServices(_ context.Context) ([]*model.Service, error) {
	var out []*model.Service
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketServices).ForEach(func(_, v []byte) error {
			var svc model.Service
			if err := json.Unmarshal(v, &svc); err != nil {
				return err
			}
			out = append(out, &svc)
			return nil
		})
	})
	return out, err
}

func (s *BoltStore) DeleteService(_ context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return bdelete(tx, bucketServices, id)
	})
}
