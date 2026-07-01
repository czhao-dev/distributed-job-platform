package model

import "time"

// LoadBalanceStrategy names one of the reverse proxy's balancing strategies.
type LoadBalanceStrategy string

const (
	StrategyRoundRobin LoadBalanceStrategy = "round_robin"
	StrategyLeastConn  LoadBalanceStrategy = "least_conn"
	StrategyWeighted   LoadBalanceStrategy = "weighted_round_robin"
)

// RetryPolicy is the proxy's per-service retry behavior.
type RetryPolicy struct {
	MaxAttempts     int `json:"max_attempts"`
	PerTryTimeoutMS int `json:"per_try_timeout_ms"`
}

// HealthCheckConfig is the proxy's per-service active health-check behavior.
type HealthCheckConfig struct {
	Path            string `json:"path"`
	IntervalSeconds int    `json:"interval_seconds"`
}

// BackendSpec is a flat, JSON-serializable backend descriptor returned to the
// reverse proxy via GET /api/v1/proxy/backends. It is intentionally distinct
// from reverse-proxy-load-balancer's own backend.Backend runtime type (which
// tracks atomic health/connection-count state) — this is just a DTO.
type BackendSpec struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Weight int    `json:"weight"`
}

// Service describes how the reverse proxy should route traffic for a path
// prefix. The Selector field resolves backends dynamically by matching against
// Node labels at request time, rather than a static backend list.
//
// Design note: Service.Selector matches Node labels (not Pod labels) because
// Pods in this system run as subprocesses with no independent network address —
// the only routable entities are Nodes. Production orchestrators instead route
// to Pods directly, but that requires a container networking layer this project
// does not implement.
type Service struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Namespace   string              `json:"namespace"`
	PathPrefix  string              `json:"path_prefix"`
	Strategy    LoadBalanceStrategy `json:"strategy"`
	Selector    map[string]string   `json:"selector,omitempty"`
	RetryPolicy RetryPolicy         `json:"retry_policy"`
	HealthCheck HealthCheckConfig   `json:"health_check"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

// Clone returns a deep copy of the Service.
func (s Service) Clone() Service {
	c := s
	if s.Selector != nil {
		c.Selector = make(map[string]string, len(s.Selector))
		for k, v := range s.Selector {
			c.Selector[k] = v
		}
	}
	return c
}
