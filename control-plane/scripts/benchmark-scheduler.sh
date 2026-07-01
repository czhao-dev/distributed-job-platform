#!/usr/bin/env bash
# Submits N short-lived deployments and times how long it takes for every pod
# they create to reach SUCCEEDED, polling /api/v1/scheduler/stats. A simple
# wall-clock benchmark, not a Go benchmark -- keeps it dependency-free and
# easy to run against a live (Docker or local) cluster.
# Run ./run-local-cluster.sh first.
set -euo pipefail

cd "$(dirname "$0")/.."

N="${1:-10}"
REPLICAS_PER_DEPLOYMENT="${2:-5}"

INFRACTL_BIN="$(mktemp -d)/infractl"
go build -o "$INFRACTL_BIN" ./cmd/infractl
export INFRACTL_SERVER="${INFRACTL_SERVER:-http://localhost:7070}"
SERVER="$INFRACTL_SERVER"

echo "Submitting $N deployments x $REPLICAS_PER_DEPLOYMENT pod instances each..."
TMP_DEPLOYMENT="$(mktemp)"
cat > "$TMP_DEPLOYMENT" <<EOF
name: benchmark
namespace: default
type: batch
command: "true"
replicas: $REPLICAS_PER_DEPLOYMENT
max_retries: 1
resources:
  cpu: 0.05
  memory_mb: 16
EOF

TOTAL_PODS=$((N * REPLICAS_PER_DEPLOYMENT))
START=$(date +%s.%N)

for i in $(seq 1 "$N"); do
  "$INFRACTL_BIN" deployment submit "$TMP_DEPLOYMENT" >/dev/null
done

echo "Submitted $TOTAL_PODS total pod instances. Waiting for completion..."
while true; do
  PENDING=$(curl -s "$SERVER/api/v1/scheduler/queue" | python3 -c 'import json,sys; print(json.load(sys.stdin)["depth"])' 2>/dev/null || echo "?")
  echo "  pending: $PENDING"
  if [ "$PENDING" = "0" ]; then
    break
  fi
  sleep 1
done

END=$(date +%s.%N)
ELAPSED=$(python3 -c "print(f'{$END - $START:.2f}')")
THROUGHPUT=$(python3 -c "print(f'{$TOTAL_PODS / ($END - $START):.1f}')")

echo
echo "Submitted and scheduled $TOTAL_PODS pods in ${ELAPSED}s (${THROUGHPUT} pods/sec scheduling throughput)."
echo "Note: this measures submission-to-scheduled latency; pods may still be running/finishing on nodes."
