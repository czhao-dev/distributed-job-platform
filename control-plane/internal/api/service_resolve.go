package api

import (
	"context"

	"github.com/czhao-dev/control-plane/internal/model"
)

// resolveServiceBackends returns the set of healthy nodes whose labels match
// the service's selector. An empty selector matches all healthy nodes.
func (h *Handlers) resolveServiceBackends(ctx context.Context, svc *model.Service) ([]model.BackendSpec, error) {
	nodes, err := h.store.ListNodesByLabels(ctx, svc.Selector)
	if err != nil {
		return nil, err
	}
	backends := make([]model.BackendSpec, 0, len(nodes))
	for _, n := range nodes {
		if n.Status != model.NodeHealthy {
			continue
		}
		backends = append(backends, model.BackendSpec{Name: n.ID, URL: n.Address, Weight: 1})
	}
	return backends, nil
}
