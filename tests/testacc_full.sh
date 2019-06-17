#!/bin/bash
set -e

log() {
  echo "####################"
  echo "## ->  $1 "
  echo "####################"
}

setup() {
    "$(pwd)"/tests/testacc_setup.sh
}

run() {
  go clean -testcache
  source "$(pwd)"/tests/env.sh
  TF_ACC=1 go test ./postgresql -v -timeout 120m
  
  # for a single test comment the previous line and uncomment the next line
  #TF_LOG=INFO TF_ACC=1 go test -v ./postgresql -run ^TestAccPostgresqlRole_Basic$ -timeout 360s
  
  # keep the return value for the scripts to fail and clean properly
  return $?
}

cleanup() {
    "$(pwd)"/tests/testacc_cleanup.sh
}

## main
log "setup" && setup 
log "run" && run || (log "cleanup" && cleanup && exit 1)
log "cleanup" && cleanup
