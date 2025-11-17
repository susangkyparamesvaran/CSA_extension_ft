#!/usr/bin/env bash
set -euo pipefail

#############################
# CONFIG
#############################

# List of *worker* EC2 instance IDs (NOT the broker)
WORKERS=(
  i-0a18a4837b2302e9b   # Testing
  i-0957de6dfce2abc3b   # Testing2
)

# How long to wait before killing one (in seconds)
MIN_DELAY=10       # earliest kill
MAX_DELAY=60       # latest kill

# Your Go test command
TEST_CMD=("go" "test" "-count=1" "./tests" "-v" "-run" "TestGol/-1")

#############################
# SCRIPT
#############################

echo "Starting Go tests: ${TEST_CMD[*]}"
"${TEST_CMD[@]}" &
TEST_PID=$!

echo "Go test running with PID $TEST_PID"

# Random delay between MIN_DELAY and MAX_DELAY
RANGE=$((MAX_DELAY - MIN_DELAY + 1))
DELAY=$((RANDOM % RANGE + MIN_DELAY))
echo "Will stop a random worker in ${DELAY}s..."
sleep "$DELAY"

# Pick a random worker
NUM_WORKERS=${#WORKERS[@]}
IDX=$((RANDOM % NUM_WORKERS))
VICTIM_ID=${WORKERS[$IDX]}

echo "Stopping worker instance: $VICTIM_ID"
aws ec2 stop-instances --instance-ids "$VICTIM_ID" >/dev/null

echo "Sent stop command to $VICTIM_ID. Waiting for tests to finish..."
wait "$TEST_PID" || {
  echo "go test exited with non-zero status (expected if the system doesn't handle failure well yet)"
  exit 1
}

echo "Chaos test finished."
