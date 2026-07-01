package state

import (
	"context"
	"errors"

	"github.com/czhao-dev/control-plane/internal/model"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrAlreadyExists     = errors.New("already exists")
	ErrInvalidTransition = errors.New("invalid state transition")
)

// Store is the control plane's view of desired and observed cluster state:
// deployments (desired state), pods (execution units), nodes (execution
// nodes), and services (proxy routing config).
type Store interface {
	CreateDeployment(ctx context.Context, d *model.Deployment) error
	GetDeployment(ctx context.Context, id string) (*model.Deployment, error)
	ListDeployments(ctx context.Context) ([]*model.Deployment, error)
	ListDeploymentsByNamespace(ctx context.Context, namespace string) ([]*model.Deployment, error)
	UpdateDeployment(ctx context.Context, d *model.Deployment) error
	TransitionDeployment(ctx context.Context, id string, to model.DeploymentStatus) error
	DeleteDeployment(ctx context.Context, id string) error

	CreatePod(ctx context.Context, p *model.Pod) error
	GetPod(ctx context.Context, id string) (*model.Pod, error)
	ListPodsByDeployment(ctx context.Context, deploymentID string) ([]*model.Pod, error)
	ListPodsByStatus(ctx context.Context, status model.PodStatus) ([]*model.Pod, error)
	ListPodsByNode(ctx context.Context, nodeID string) ([]*model.Pod, error)
	ListPodsByLabels(ctx context.Context, namespace string, selector map[string]string) ([]*model.Pod, error)
	UpdatePod(ctx context.Context, p *model.Pod) error
	TransitionPod(ctx context.Context, id string, to model.PodStatus, errMsg string) error

	RegisterNode(ctx context.Context, n *model.Node) error
	GetNode(ctx context.Context, id string) (*model.Node, error)
	ListNodes(ctx context.Context) ([]*model.Node, error)
	ListNodesByLabels(ctx context.Context, selector map[string]string) ([]*model.Node, error)
	UpdateNode(ctx context.Context, n *model.Node) error
	TransitionNode(ctx context.Context, id string, to model.NodeStatus) error

	UpsertService(ctx context.Context, s *model.Service) error
	GetService(ctx context.Context, id string) (*model.Service, error)
	ListServices(ctx context.Context) ([]*model.Service, error)
	DeleteService(ctx context.Context, id string) error
}
