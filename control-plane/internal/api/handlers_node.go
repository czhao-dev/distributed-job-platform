package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/czhao-dev/control-plane/internal/metrics"
	"github.com/czhao-dev/control-plane/internal/model"
	"github.com/czhao-dev/control-plane/internal/state"
)

type registerNodeRequest struct {
	Hostname      string                 `json:"hostname"`
	Address       string                 `json:"address"`
	Capacity      model.ResourceCapacity `json:"capacity"`
	MaxConcurrent int                    `json:"max_concurrent_jobs"`
}

// RegisterNode handles POST /api/v1/nodes/register. A node is considered
// healthy the instant it successfully registers, since calling this endpoint
// is itself proof of life.
func (h *Handlers) RegisterNode(w http.ResponseWriter, r *http.Request) {
	var req registerNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Address == "" {
		writeJSONError(w, http.StatusBadRequest, "address is required")
		return
	}

	const maxIDAttempts = 5
	var id string
	for attempt := 0; attempt < maxIDAttempts; attempt++ {
		genID, err := generateID("node")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to generate node id")
			return
		}
		now := time.Now()
		node := &model.Node{
			ID:              genID,
			Hostname:        req.Hostname,
			Address:         req.Address,
			Status:          model.NodeRegistering,
			Capacity:        req.Capacity,
			Available:       req.Capacity,
			MaxConcurrent:   req.MaxConcurrent,
			LastHeartbeatAt: now,
			RegisteredAt:    now,
		}
		if err := h.store.RegisterNode(r.Context(), node); err == nil {
			id = genID
			break
		}
	}
	if id == "" {
		writeJSONError(w, http.StatusInternalServerError, "failed to allocate a unique node id")
		return
	}

	if err := h.store.TransitionNode(r.Context(), id, model.NodeHealthy); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to mark node healthy")
		return
	}

	node, err := h.store.GetNode(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "node vanished after registration")
		return
	}
	writeJSON(w, http.StatusCreated, node)
}

type nodeHeartbeatRequest struct {
	RunningJobs int                    `json:"running_jobs"`
	Available   model.ResourceCapacity `json:"available"`
}

// Heartbeat handles POST /api/v1/nodes/{id}/heartbeat. Receiving a heartbeat
// proves the node is alive, so an UNHEALTHY node recovers here — the
// reconciler only ever owns the HEALTHY→UNHEALTHY direction. A DRAINING node
// stays DRAINING (operator decommission is not undone by the node still being
// alive).
func (h *Handlers) Heartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, err := h.store.GetNode(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}

	var req nodeHeartbeatRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body is optional

	node.LastHeartbeatAt = time.Now()
	node.RunningJobs = req.RunningJobs
	if req.Available != (model.ResourceCapacity{}) {
		node.Available = req.Available
	}
	if err := h.store.UpdateNode(r.Context(), node); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if node.Status == model.NodeUnhealthy {
		if err := h.store.TransitionNode(r.Context(), id, model.NodeHealthy); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	metrics.WorkerHeartbeats.Inc()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// PollPods handles GET /api/v1/nodes/{id}/pods/poll. Returns at most one pod
// currently SCHEDULED onto this node (awaiting pickup), or an empty response
// if there is none.
func (h *Handlers) PollPods(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.store.GetNode(r.Context(), id); err != nil {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}

	pods, err := h.store.ListPodsByNode(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, p := range pods {
		if p.Status == model.PodScheduled {
			writeJSON(w, http.StatusOK, map[string]any{"pod": p})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pod": nil})
}

type podStatusRequest struct {
	Status   model.PodStatus `json:"status"`
	ExitCode *int            `json:"exit_code,omitempty"`
	Error    string          `json:"error,omitempty"`
	Output   string          `json:"output,omitempty"`
}

// UpdatePodStatus handles POST /api/v1/nodes/{id}/pods/{pod_id}/status.
// When a pod leaves RUNNING into a terminal state, the node's reserved
// capacity for it is released here.
func (h *Handlers) UpdatePodStatus(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	podID := r.PathValue("pod_id")

	var req podStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	pod, err := h.store.GetPod(r.Context(), podID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "pod not found")
		return
	}
	wasRunning := pod.Status == model.PodRunning

	pod.ExitCode = req.ExitCode
	pod.Output = req.Output
	if err := h.store.UpdatePod(r.Context(), pod); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.TransitionPod(r.Context(), podID, req.Status, req.Error); err != nil {
		if err == state.ErrInvalidTransition {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if wasRunning && isPodTerminalish(req.Status) {
		h.releaseNodeCapacity(r.Context(), nodeID, pod.DeploymentID)
		switch req.Status {
		case model.PodSucceeded:
			metrics.JobsSucceeded.Inc()
		case model.PodFailed:
			metrics.JobsFailed.Inc()
		}
	}

	updated, _ := h.store.GetPod(r.Context(), podID)
	writeJSON(w, http.StatusOK, updated)
}

func isPodTerminalish(s model.PodStatus) bool {
	switch s {
	case model.PodSucceeded, model.PodFailed, model.PodCancelled:
		return true
	default:
		return false
	}
}

func (h *Handlers) releaseNodeCapacity(ctx context.Context, nodeID, deploymentID string) {
	node, err := h.store.GetNode(ctx, nodeID)
	if err != nil {
		return
	}
	if node.RunningJobs > 0 {
		node.RunningJobs--
	}
	if deployment, err := h.store.GetDeployment(ctx, deploymentID); err == nil {
		node.Available.CPU += deployment.Resources.CPU
		node.Available.MemoryMB += deployment.Resources.MemoryMB
		if node.Available.CPU > node.Capacity.CPU {
			node.Available.CPU = node.Capacity.CPU
		}
		if node.Available.MemoryMB > node.Capacity.MemoryMB {
			node.Available.MemoryMB = node.Capacity.MemoryMB
		}
	}
	_ = h.store.UpdateNode(ctx, node)
}

// ListNodes handles GET /api/v1/nodes.
// Supports optional ?label=key=value (repeatable) for label filtering.
func (h *Handlers) ListNodes(w http.ResponseWriter, r *http.Request) {
	labelVals := r.URL.Query()["label"]
	var nodes []*model.Node
	var err error
	if len(labelVals) > 0 {
		nodes, err = h.store.ListNodesByLabels(r.Context(), parseLabels(labelVals))
	} else {
		nodes, err = h.store.ListNodes(r.Context())
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "total": len(nodes)})
}

// GetNode handles GET /api/v1/nodes/{id}.
func (h *Handlers) GetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, err := h.store.GetNode(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	writeJSON(w, http.StatusOK, node)
}

// DrainNode handles POST /api/v1/nodes/{id}/drain.
func (h *Handlers) DrainNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.store.GetNode(r.Context(), id); err != nil {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	if err := h.store.TransitionNode(r.Context(), id, model.NodeDraining); err != nil {
		if err == state.ErrInvalidTransition {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	node, _ := h.store.GetNode(r.Context(), id)
	writeJSON(w, http.StatusOK, node)
}
