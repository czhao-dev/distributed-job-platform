#!/usr/bin/env bash
# Demonstrates failure recovery: submits a deployment, hard-kills a node
# container while pods are running on it, and shows the control plane detect
# the missed heartbeat, mark the node unhealthy, and reschedule its pods.
# Run ./run-local-cluster.sh first.
set -euo pipefail

cd "$(dirname "$0")/.."

INFRACTL_BIN="$(mktemp -d)/infractl"
go build -o "$INFRACTL_BIN" ./cmd/infractl
export INFRACTL_SERVER="${INFRACTL_SERVER:-http://localhost:7070}"

echo "1. Submitting a deployment with 12 long-running pod instances..."
TMP_DEPLOYMENT="$(mktemp)"
cat > "$TMP_DEPLOYMENT" <<'EOF'
name: failure-recovery-demo
namespace: default
labels:
  app: failure-recovery-demo
type: batch
command: "sleep"
args: ["8"]
replicas: 12
max_retries: 3
restart_policy: on_failure
resources:
  cpu: 0.1
  memory_mb: 32
EOF
"$INFRACTL_BIN" deployment submit "$TMP_DEPLOYMENT"
DEPLOYMENT_ID=$("$INFRACTL_BIN" deployment list | awk 'NR==2{print $1}')

echo
echo "2. Waiting for pods to be scheduled across nodes..."
sleep 3
"$INFRACTL_BIN" deployment status "$DEPLOYMENT_ID" | tail -n +6

echo
echo "3. Hard-killing worker-2 (simulates a crashed node, not a graceful stop)..."
( cd .. && docker compose kill worker-2 )
KILL_TIME=$(date +%s)

echo
echo "4. Waiting for the control plane's heartbeat timeout to fire (default 15s,
   plus reconcile interval) -- this will take ~17-20s..."
while true; do
  STATUS=$("$INFRACTL_BIN" node list | grep "worker-2:9101" | awk '{print $3}' || true)
  NOW=$(date +%s)
  ELAPSED=$((NOW - KILL_TIME))
  echo "  [+${ELAPSED}s] worker-2 node status: ${STATUS:-unknown}"
  if [ "$STATUS" = "UNHEALTHY" ]; then
    break
  fi
  if [ "$ELAPSED" -gt 30 ]; then
    echo "  timed out waiting for worker-2 to be marked unhealthy" >&2
    break
  fi
  sleep 2
done

echo
echo "5. Node fleet after detection:"
"$INFRACTL_BIN" node list

echo
echo "6. Deployment status -- pods that were running on worker-2 should now be
   PENDING/SCHEDULED again (or with backoff), being picked up by
   the remaining healthy nodes:"
"$INFRACTL_BIN" deployment status "$DEPLOYMENT_ID" | tail -n +6

echo
echo "Done. Restart worker-2 with: docker compose start worker-2"
