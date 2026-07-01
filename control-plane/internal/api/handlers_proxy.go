package api

import (
	"encoding/json"
	"net/http"

	"github.com/czhao-dev/control-plane/internal/model"
	"github.com/czhao-dev/control-plane/internal/state"
)

// ListServices handles GET /api/v1/services.
// Supports optional ?namespace=<ns> query param.
func (h *Handlers) ListServices(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	services, err := h.store.ListServices(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ns != "" {
		filtered := services[:0]
		for _, svc := range services {
			if svc.Namespace == ns {
				filtered = append(filtered, svc)
			}
		}
		services = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": services, "total": len(services)})
}

// CreateService handles POST /api/v1/services.
func (h *Handlers) CreateService(w http.ResponseWriter, r *http.Request) {
	var svc model.Service
	if err := json.NewDecoder(r.Body).Decode(&svc); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if svc.PathPrefix == "" {
		writeJSONError(w, http.StatusBadRequest, "path_prefix is required")
		return
	}
	if svc.ID == "" {
		genID, err := generateID("svc")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to generate service id")
			return
		}
		svc.ID = genID
	}
	if svc.Strategy == "" {
		svc.Strategy = model.StrategyRoundRobin
	}
	if svc.Namespace == "" {
		svc.Namespace = "default"
	}
	if err := h.store.UpsertService(r.Context(), &svc); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	created, _ := h.store.GetService(r.Context(), svc.ID)
	w.Header().Set("Location", "/api/v1/services/"+svc.ID)
	writeJSON(w, http.StatusCreated, created)
}

// GetService handles GET /api/v1/services/{id}.
func (h *Handlers) GetService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, err := h.store.GetService(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "service not found")
		return
	}
	writeJSON(w, http.StatusOK, svc)
}

// UpdateService handles PUT /api/v1/services/{id}.
func (h *Handlers) UpdateService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.store.GetService(r.Context(), id); err != nil {
		writeJSONError(w, http.StatusNotFound, "service not found")
		return
	}
	var svc model.Service
	if err := json.NewDecoder(r.Body).Decode(&svc); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	svc.ID = id
	if err := h.store.UpsertService(r.Context(), &svc); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := h.store.GetService(r.Context(), id)
	writeJSON(w, http.StatusOK, updated)
}

// DeleteService handles DELETE /api/v1/services/{id}.
func (h *Handlers) DeleteService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteService(r.Context(), id); err != nil {
		if err == state.ErrNotFound {
			writeJSONError(w, http.StatusNotFound, "service not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetServiceBackends handles GET /api/v1/services/{id}/backends.
// Returns the set of healthy nodes matched by the service's selector.
func (h *Handlers) GetServiceBackends(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, err := h.store.GetService(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "service not found")
		return
	}
	backends, err := h.resolveServiceBackends(r.Context(), svc)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backends": backends, "total": len(backends)})
}

// ProxyConfig handles GET /api/v1/proxy/config -- the full set of services
// the reverse proxy should be aware of.
func (h *Handlers) ProxyConfig(w http.ResponseWriter, r *http.Request) {
	services, err := h.store.ListServices(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": services})
}

// ProxyBackends handles GET /api/v1/proxy/backends. Synthesizes the dynamic
// proxy's backend list from currently HEALTHY nodes. Accepts an optional
// ?service={id} param: if given, resolves backends via that service's
// selector rather than returning all healthy nodes.
func (h *Handlers) ProxyBackends(w http.ResponseWriter, r *http.Request) {
	if svcID := r.URL.Query().Get("service"); svcID != "" {
		svc, err := h.store.GetService(r.Context(), svcID)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "service not found")
			return
		}
		backends, err := h.resolveServiceBackends(r.Context(), svc)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"backends": backends})
		return
	}

	nodes, err := h.store.ListNodes(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	backends := make([]model.BackendSpec, 0, len(nodes))
	for _, n := range nodes {
		if n.Status != model.NodeHealthy {
			continue
		}
		backends = append(backends, model.BackendSpec{Name: n.ID, URL: n.Address, Weight: 1})
	}
	writeJSON(w, http.StatusOK, map[string]any{"backends": backends})
}
