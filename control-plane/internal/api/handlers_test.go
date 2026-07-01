package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/czhao-dev/control-plane/internal/model"
	"github.com/czhao-dev/control-plane/internal/scheduler"
	"github.com/czhao-dev/control-plane/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer() *httptest.Server {
	st := state.NewMemoryStore()
	sched := scheduler.New(st, time.Hour, nil)
	h := NewHandlers(st, sched)
	return httptest.NewServer(NewRouter(h))
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	return resp
}

func TestDeploymentLifecycle(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/api/v1/deployments", map[string]any{
		"name": "demo", "command": "echo", "replicas": 2,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var created model.Deployment
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	assert.Equal(t, model.DeploymentPending, created.Status)
	assert.Equal(t, "default", created.Namespace, "namespace should default to 'default'")

	getResp, err := http.Get(srv.URL + "/api/v1/deployments/" + created.ID)
	require.NoError(t, err)
	defer getResp.Body.Close()
	assert.Equal(t, http.StatusOK, getResp.StatusCode)

	listResp, err := http.Get(srv.URL + "/api/v1/deployments")
	require.NoError(t, err)
	defer listResp.Body.Close()
	var list struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&list))
	assert.Equal(t, 1, list.Total)

	nsResp, err := http.Get(srv.URL + "/api/v1/deployments?namespace=default")
	require.NoError(t, err)
	defer nsResp.Body.Close()
	var nsList struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(nsResp.Body).Decode(&nsList))
	assert.Equal(t, 1, nsList.Total)

	otherNsResp, err := http.Get(srv.URL + "/api/v1/deployments?namespace=production")
	require.NoError(t, err)
	defer otherNsResp.Body.Close()
	var otherList struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(otherNsResp.Body).Decode(&otherList))
	assert.Equal(t, 0, otherList.Total)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/deployments/"+created.ID, nil)
	delResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer delResp.Body.Close()
	assert.Equal(t, http.StatusOK, delResp.StatusCode)

	notFoundResp, err := http.Get(srv.URL + "/api/v1/deployments/does-not-exist")
	require.NoError(t, err)
	defer notFoundResp.Body.Close()
	assert.Equal(t, http.StatusNotFound, notFoundResp.StatusCode)
}

func TestNodeRegisterHeartbeatPollAndPodStatus(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	regResp := postJSON(t, srv.URL+"/api/v1/nodes/register", map[string]any{
		"hostname": "h1", "address": "http://h1:9100",
		"capacity": map[string]any{"cpu": 2, "memory_mb": 1024}, "max_concurrent_jobs": 2,
	})
	require.Equal(t, http.StatusCreated, regResp.StatusCode)
	var node model.Node
	require.NoError(t, json.NewDecoder(regResp.Body).Decode(&node))
	regResp.Body.Close()
	assert.Equal(t, model.NodeHealthy, node.Status)

	hbResp := postJSON(t, srv.URL+"/api/v1/nodes/"+node.ID+"/heartbeat", map[string]any{"running_jobs": 0})
	defer hbResp.Body.Close()
	assert.Equal(t, http.StatusOK, hbResp.StatusCode)

	pollResp, err := http.Get(srv.URL + "/api/v1/nodes/" + node.ID + "/pods/poll")
	require.NoError(t, err)
	defer pollResp.Body.Close()
	var pollOut struct {
		Pod *model.Pod `json:"pod"`
	}
	require.NoError(t, json.NewDecoder(pollResp.Body).Decode(&pollOut))
	assert.Nil(t, pollOut.Pod, "no pods scheduled yet")

	listResp, err := http.Get(srv.URL + "/api/v1/nodes")
	require.NoError(t, err)
	defer listResp.Body.Close()
	var list struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&list))
	assert.Equal(t, 1, list.Total)

	drainResp := postJSON(t, srv.URL+"/api/v1/nodes/"+node.ID+"/drain", nil)
	defer drainResp.Body.Close()
	assert.Equal(t, http.StatusOK, drainResp.StatusCode)
}

func TestNodeLabelFiltering(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	regResp := postJSON(t, srv.URL+"/api/v1/nodes/register", map[string]any{
		"address": "http://worker1:9100",
	})
	var node model.Node
	json.NewDecoder(regResp.Body).Decode(&node)
	regResp.Body.Close()

	// label filter on a node with no labels → no match
	resp, err := http.Get(srv.URL + "/api/v1/nodes?label=role=gpu")
	require.NoError(t, err)
	defer resp.Body.Close()
	var out struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, 0, out.Total)

	// no filter → returns the node
	resp2, err := http.Get(srv.URL + "/api/v1/nodes")
	require.NoError(t, err)
	defer resp2.Body.Close()
	var out2 struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&out2))
	assert.Equal(t, 1, out2.Total)
}

func TestProxyBackendsReflectsOnlyHealthyNodes(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	healthyResp := postJSON(t, srv.URL+"/api/v1/nodes/register", map[string]any{"address": "http://healthy:9100"})
	var healthy model.Node
	json.NewDecoder(healthyResp.Body).Decode(&healthy)
	healthyResp.Body.Close()

	resp, err := http.Get(srv.URL + "/api/v1/proxy/backends")
	require.NoError(t, err)
	defer resp.Body.Close()
	var out struct {
		Backends []model.BackendSpec `json:"backends"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Backends, 1)
	assert.Equal(t, "http://healthy:9100", out.Backends[0].URL)
}

func TestServiceCRUDOverHTTP(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/api/v1/services", map[string]any{
		"name": "worker-api", "path_prefix": "/workers", "strategy": "least_conn",
		"selector": map[string]string{"role": "worker"},
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var svc model.Service
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&svc))
	resp.Body.Close()
	assert.Equal(t, "default", svc.Namespace)
	assert.Equal(t, map[string]string{"role": "worker"}, svc.Selector)

	backendsResp, err := http.Get(srv.URL + "/api/v1/services/" + svc.ID + "/backends")
	require.NoError(t, err)
	defer backendsResp.Body.Close()
	var backendsOut struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(backendsResp.Body).Decode(&backendsOut))
	assert.Equal(t, 0, backendsOut.Total, "no nodes registered yet")

	delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/services/"+svc.ID, nil)
	delResp, err := http.DefaultClient.Do(delReq)
	require.NoError(t, err)
	defer delResp.Body.Close()
	assert.Equal(t, http.StatusNoContent, delResp.StatusCode)
}

func TestHealthzAndReadyz(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(srv.URL + path)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()
	}
}
