// infractl is a stdlib-net/http-only CLI client for the control-plane REST
// API. Two-level noun/verb command tree:
// deployment / node / service / cluster / scheduler.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/czhao-dev/control-plane/internal/model"
	"gopkg.in/yaml.v3"
)

func serverURL() string {
	if v := os.Getenv("INFRACTL_SERVER"); v != "" {
		return v
	}
	return "http://localhost:7070"
}

func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(1)
	}
	noun, verb, rest := os.Args[1], os.Args[2], os.Args[3:]

	switch noun {
	case "deployment":
		dispatchDeployment(verb, rest)
	case "node":
		dispatchNode(verb, rest)
	case "service":
		dispatchService(verb, rest)
	case "cluster":
		dispatchCluster(verb, rest)
	case "scheduler":
		dispatchScheduler(verb, rest)
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `infractl <noun> <verb> [flags] [args]

  deployment submit <file.yaml>
  deployment list [--namespace <ns>] [--label key=value ...]
  deployment status <id>
  deployment cancel <id>

  node list [--label key=value ...]
  node status <id>
  node drain <id>

  service list [--namespace <ns>]
  service add <file.yaml>
  service status <id>
  service backends <id>

  cluster status
  scheduler stats

Flags:
  --namespace <ns>      Filter by namespace (deployment list, service list)
  --label key=value     Filter by label (repeatable; deployment list, node list)

Set INFRACTL_SERVER (default http://localhost:7070) to point at a different control plane.`)
}

// cliFlags holds optional flag values parsed from trailing args.
type cliFlags struct {
	namespace string
	labels    map[string]string
	positional []string
}

// parseFlags extracts --namespace and --label flags from args, returning the
// remainder as positional args.
func parseFlags(args []string) cliFlags {
	f := cliFlags{labels: make(map[string]string)}
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--namespace", "-n":
			i++
			if i < len(args) {
				f.namespace = args[i]
			}
		case "--label", "-l":
			i++
			if i < len(args) {
				k, v, ok := strings.Cut(args[i], "=")
				if ok {
					f.labels[k] = v
				}
			}
		default:
			if strings.HasPrefix(args[i], "--namespace=") {
				f.namespace = strings.TrimPrefix(args[i], "--namespace=")
			} else if strings.HasPrefix(args[i], "--label=") {
				kv := strings.TrimPrefix(args[i], "--label=")
				k, v, ok := strings.Cut(kv, "=")
				if ok {
					f.labels[k] = v
				}
			} else {
				f.positional = append(f.positional, args[i])
			}
		}
		i++
	}
	return f
}

// buildQuery assembles query params from a namespace and label map.
func buildQuery(namespace string, labels map[string]string) string {
	q := url.Values{}
	if namespace != "" {
		q.Set("namespace", namespace)
	}
	for k, v := range labels {
		q.Add("label", k+"="+v)
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// --- deployment ---

func dispatchDeployment(verb string, args []string) {
	switch verb {
	case "submit":
		cmdDeploymentSubmit(args)
	case "list":
		cmdDeploymentList(args)
	case "status":
		cmdDeploymentStatus(args)
	case "cancel":
		cmdDeploymentCancel(args)
	default:
		usage()
		os.Exit(1)
	}
}

func cmdDeploymentSubmit(args []string) {
	if len(args) < 1 {
		fatalf("deployment submit: file path required")
	}
	body := readYAMLAsJSON(args[0])

	resp, err := http.Post(serverURL()+"/api/v1/deployments", "application/json", bytes.NewReader(body))
	if err != nil {
		fatalf("submit failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		fatalf("submit failed: %s", readErrorBody(resp))
	}
	var d model.Deployment
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		fatalf("submit: failed to decode response: %v", err)
	}
	fmt.Printf("Submitted %s (%s)\n", d.ID, d.Name)
}

func cmdDeploymentList(args []string) {
	f := parseFlags(args)
	var result struct {
		Deployments []model.Deployment `json:"deployments"`
		Total       int                `json:"total"`
	}
	getJSON("/api/v1/deployments"+buildQuery(f.namespace, f.labels), &result)

	fmt.Printf("%-22s %-16s %-12s %-10s %-10s %-10s\n", "ID", "NAME", "NAMESPACE", "TYPE", "STATUS", "REPLICAS")
	for _, d := range result.Deployments {
		fmt.Printf("%-22s %-16s %-12s %-10s %-10s %-10d\n", d.ID, d.Name, d.Namespace, d.Type, d.Status, d.Replicas)
	}
	fmt.Printf("\n%d total\n", result.Total)
}

func cmdDeploymentStatus(args []string) {
	f := parseFlags(args)
	if len(f.positional) < 1 {
		fatalf("deployment status: id required")
	}
	id := f.positional[0]
	var d model.Deployment
	getJSON("/api/v1/deployments/"+id, &d)

	fmt.Printf("ID:        %s\n", d.ID)
	fmt.Printf("Name:      %s\n", d.Name)
	fmt.Printf("Namespace: %s\n", d.Namespace)
	fmt.Printf("Type:      %s\n", d.Type)
	fmt.Printf("Status:    %s\n", d.Status)
	fmt.Printf("Replicas:  %d\n", d.Replicas)

	var podsResult struct {
		Pods  []model.Pod `json:"pods"`
		Total int         `json:"total"`
	}
	getJSON("/api/v1/deployments/"+id+"/pods", &podsResult)

	fmt.Printf("\nPods (%d):\n", podsResult.Total)
	fmt.Printf("%-18s %-16s %-12s %-8s\n", "ID", "NODE", "STATUS", "ATTEMPT")
	for _, p := range podsResult.Pods {
		fmt.Printf("%-18s %-16s %-12s %-8d\n", p.ID, p.NodeID, p.Status, p.Attempt)
	}
}

func cmdDeploymentCancel(args []string) {
	f := parseFlags(args)
	if len(f.positional) < 1 {
		fatalf("deployment cancel: id required")
	}
	id := f.positional[0]
	doRequest(http.MethodDelete, "/api/v1/deployments/"+id, nil, http.StatusOK)
	fmt.Printf("Cancelled %s\n", id)
}

// --- node ---

func dispatchNode(verb string, args []string) {
	switch verb {
	case "list":
		cmdNodeList(args)
	case "status":
		cmdNodeStatus(args)
	case "drain":
		cmdNodeDrain(args)
	default:
		usage()
		os.Exit(1)
	}
}

func cmdNodeList(args []string) {
	f := parseFlags(args)
	var result struct {
		Nodes []model.Node `json:"nodes"`
		Total int          `json:"total"`
	}
	getJSON("/api/v1/nodes"+buildQuery("", f.labels), &result)

	fmt.Printf("%-18s %-24s %-10s %-8s %-8s\n", "ID", "ADDRESS", "STATUS", "RUNNING", "MAX")
	for _, n := range result.Nodes {
		fmt.Printf("%-18s %-24s %-10s %-8d %-8d\n", n.ID, n.Address, n.Status, n.RunningJobs, n.MaxConcurrent)
	}
	fmt.Printf("\n%d total\n", result.Total)
}

func cmdNodeStatus(args []string) {
	f := parseFlags(args)
	if len(f.positional) < 1 {
		fatalf("node status: id required")
	}
	var n model.Node
	getJSON("/api/v1/nodes/"+f.positional[0], &n)

	fmt.Printf("ID:             %s\n", n.ID)
	fmt.Printf("Hostname:       %s\n", n.Hostname)
	fmt.Printf("Address:        %s\n", n.Address)
	fmt.Printf("Status:         %s\n", n.Status)
	fmt.Printf("Running Pods:   %d / %d\n", n.RunningJobs, n.MaxConcurrent)
	fmt.Printf("Capacity:       cpu=%.2f memory_mb=%d\n", n.Capacity.CPU, n.Capacity.MemoryMB)
	fmt.Printf("Available:      cpu=%.2f memory_mb=%d\n", n.Available.CPU, n.Available.MemoryMB)
	fmt.Printf("Last Heartbeat: %s\n", n.LastHeartbeatAt.Format(time.RFC3339))
}

func cmdNodeDrain(args []string) {
	f := parseFlags(args)
	if len(f.positional) < 1 {
		fatalf("node drain: id required")
	}
	id := f.positional[0]
	doRequest(http.MethodPost, "/api/v1/nodes/"+id+"/drain", nil, http.StatusOK)
	fmt.Printf("Draining %s\n", id)
}

// --- service ---

func dispatchService(verb string, args []string) {
	switch verb {
	case "list":
		cmdServiceList(args)
	case "add":
		cmdServiceAdd(args)
	case "status":
		cmdServiceStatus(args)
	case "backends":
		cmdServiceBackends(args)
	default:
		usage()
		os.Exit(1)
	}
}

func cmdServiceList(args []string) {
	f := parseFlags(args)
	var result struct {
		Services []model.Service `json:"services"`
		Total    int             `json:"total"`
	}
	getJSON("/api/v1/services"+buildQuery(f.namespace, nil), &result)

	fmt.Printf("%-18s %-16s %-12s %-16s %-16s\n", "ID", "NAME", "NAMESPACE", "PATH_PREFIX", "STRATEGY")
	for _, s := range result.Services {
		fmt.Printf("%-18s %-16s %-12s %-16s %-16s\n", s.ID, s.Name, s.Namespace, s.PathPrefix, s.Strategy)
	}
	fmt.Printf("\n%d total\n", result.Total)
}

func cmdServiceAdd(args []string) {
	f := parseFlags(args)
	if len(f.positional) < 1 {
		fatalf("service add: file path required")
	}
	body := readYAMLAsJSON(f.positional[0])
	resp, err := http.Post(serverURL()+"/api/v1/services", "application/json", bytes.NewReader(body))
	if err != nil {
		fatalf("service add failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		fatalf("service add failed: %s", readErrorBody(resp))
	}
	var s model.Service
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		fatalf("service add: failed to decode response: %v", err)
	}
	fmt.Printf("Added service %s (%s)\n", s.ID, s.Name)
}

func cmdServiceStatus(args []string) {
	f := parseFlags(args)
	if len(f.positional) < 1 {
		fatalf("service status: id required")
	}
	var s model.Service
	getJSON("/api/v1/services/"+f.positional[0], &s)
	b, _ := json.MarshalIndent(s, "", "  ")
	fmt.Println(string(b))
}

func cmdServiceBackends(args []string) {
	f := parseFlags(args)
	if len(f.positional) < 1 {
		fatalf("service backends: id required")
	}
	var result struct {
		Backends []model.BackendSpec `json:"backends"`
		Total    int                 `json:"total"`
	}
	getJSON("/api/v1/services/"+f.positional[0]+"/backends", &result)

	fmt.Printf("%-18s %-32s %-8s\n", "NAME", "URL", "WEIGHT")
	for _, b := range result.Backends {
		fmt.Printf("%-18s %-32s %-8d\n", b.Name, b.URL, b.Weight)
	}
	fmt.Printf("\n%d backend(s)\n", result.Total)
}

// --- cluster / scheduler ---

func dispatchCluster(verb string, _ []string) {
	if verb != "status" {
		usage()
		os.Exit(1)
	}
	cmdClusterStatus()
}

func cmdClusterStatus() {
	var deployments struct {
		Total int `json:"total"`
	}
	getJSON("/api/v1/deployments", &deployments)

	var nodes struct {
		Nodes []model.Node `json:"nodes"`
		Total int          `json:"total"`
	}
	getJSON("/api/v1/nodes", &nodes)

	var services struct {
		Total int `json:"total"`
	}
	getJSON("/api/v1/services", &services)

	healthy := 0
	for _, n := range nodes.Nodes {
		if n.Status == model.NodeHealthy {
			healthy++
		}
	}

	fmt.Printf("Control plane: %s\n", serverURL())
	fmt.Printf("Deployments:   %d\n", deployments.Total)
	fmt.Printf("Nodes:         %d (%d healthy)\n", nodes.Total, healthy)
	fmt.Printf("Services:      %d\n", services.Total)
}

func dispatchScheduler(verb string, _ []string) {
	if verb != "stats" {
		usage()
		os.Exit(1)
	}
	var raw map[string]any
	getJSON("/api/v1/scheduler/stats", &raw)
	b, _ := json.MarshalIndent(raw, "", "  ")
	fmt.Println(string(b))
}

// --- shared helpers ---

func readYAMLAsJSON(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		fatalf("failed to read %s: %v", path, err)
	}
	var generic map[string]any
	if err := yaml.Unmarshal(data, &generic); err != nil {
		fatalf("failed to parse YAML %s: %v", path, err)
	}
	body, err := json.Marshal(generic)
	if err != nil {
		fatalf("failed to re-encode %s as JSON: %v", path, err)
	}
	return body
}

func getJSON(path string, out any) {
	resp, err := http.Get(serverURL() + path)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatalf("request failed: %s", readErrorBody(resp))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		fatalf("failed to decode response: %v", err)
	}
}

func doRequest(method, path string, body []byte, wantStatus int) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, serverURL()+path, reader)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		fatalf("request failed: %s", readErrorBody(resp))
	}
}

func readErrorBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	return fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
