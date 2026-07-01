package api

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRouter wires every control-plane endpoint. Uses stdlib net/http's
// Go-1.22+ method-prefixed patterns and {wildcard} path values rather than
// gorilla/mux -- this is a fresh module on Go 1.26, so stdlib routing covers
// everything gorilla/mux would, consistent with how
// reverse-proxy-load-balancer already uses a plain ServeMux.
func NewRouter(h *Handlers) http.Handler {
	mux := http.NewServeMux()

	// Deployment API (kubectl create/get/delete deployment)
	mux.HandleFunc("POST /api/v1/deployments", h.CreateDeployment)
	mux.HandleFunc("GET /api/v1/deployments", h.ListDeployments)
	mux.HandleFunc("GET /api/v1/deployments/{id}", h.GetDeployment)
	mux.HandleFunc("DELETE /api/v1/deployments/{id}", h.CancelDeployment)
	mux.HandleFunc("GET /api/v1/deployments/{id}/pods", h.ListDeploymentPods)

	// Node API (kubelet register/heartbeat/poll/status)
	mux.HandleFunc("POST /api/v1/nodes/register", h.RegisterNode)
	mux.HandleFunc("POST /api/v1/nodes/{id}/heartbeat", h.Heartbeat)
	mux.HandleFunc("GET /api/v1/nodes/{id}/pods/poll", h.PollPods)
	mux.HandleFunc("POST /api/v1/nodes/{id}/pods/{pod_id}/status", h.UpdatePodStatus)
	mux.HandleFunc("GET /api/v1/nodes", h.ListNodes)
	mux.HandleFunc("GET /api/v1/nodes/{id}", h.GetNode)
	mux.HandleFunc("POST /api/v1/nodes/{id}/drain", h.DrainNode)

	// Scheduler API
	mux.HandleFunc("GET /api/v1/scheduler/queue", h.SchedulerQueue)
	mux.HandleFunc("GET /api/v1/scheduler/stats", h.SchedulerStats)
	mux.HandleFunc("POST /api/v1/scheduler/rebalance", h.SchedulerRebalance)

	// Service API (kubectl create/get/delete service)
	mux.HandleFunc("GET /api/v1/services", h.ListServices)
	mux.HandleFunc("POST /api/v1/services", h.CreateService)
	mux.HandleFunc("GET /api/v1/services/{id}", h.GetService)
	mux.HandleFunc("PUT /api/v1/services/{id}", h.UpdateService)
	mux.HandleFunc("DELETE /api/v1/services/{id}", h.DeleteService)
	mux.HandleFunc("GET /api/v1/services/{id}/backends", h.GetServiceBackends)

	// Proxy discovery API (consumed by reverse-proxy-load-balancer)
	mux.HandleFunc("GET /api/v1/proxy/config", h.ProxyConfig)
	mux.HandleFunc("GET /api/v1/proxy/backends", h.ProxyBackends)

	mux.HandleFunc("GET /healthz", Healthz)
	mux.HandleFunc("GET /readyz", Readyz)
	mux.Handle("GET /metrics", promhttp.Handler())

	return requestIDMiddleware(recoveryMiddleware(loggingMiddleware(mux)))
}

// Healthz handles GET /healthz -- liveness: the HTTP server is up.
func Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz handles GET /readyz -- readiness: always true once the server is
// serving, since the in-memory store has no external dependencies to wait on.
func Readyz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
