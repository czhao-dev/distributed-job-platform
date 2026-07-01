#!/usr/bin/env bash
# Submits the example batch deployment (20 pod instances) to a running
# control plane and shows how the scheduler distributes them across nodes.
# Run ./run-local-cluster.sh first.
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT_DIR="$(pwd)"

INFRACTL_BIN="$(mktemp -d)/infractl"
go build -o "$INFRACTL_BIN" ./cmd/infractl

export INFRACTL_SERVER="${INFRACTL_SERVER:-http://localhost:7070}"

echo "Submitting examples/batch-job.yaml (20 pod instances) to $INFRACTL_SERVER..."
"$INFRACTL_BIN" deployment submit examples/batch-job.yaml

DEPLOYMENT_ID=$("$INFRACTL_BIN" deployment list | awk 'NR==2{print $1}')
echo "Deployment ID: $DEPLOYMENT_ID"

for i in 1 2 3 4 5; do
  echo
  echo "--- after ${i}s ---"
  "$INFRACTL_BIN" node list
  echo
  "$INFRACTL_BIN" deployment status "$DEPLOYMENT_ID" | tail -n +6
  sleep 1
done

echo
echo "Done. Re-run \`infractl deployment status $DEPLOYMENT_ID\` to keep watching."
