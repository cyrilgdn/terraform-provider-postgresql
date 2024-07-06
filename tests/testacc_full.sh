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
  go test -count=1 ./postgresql -v -timeout 120m

  # keep the return value for the scripts to fail and clean properly
  return $?
}

cleanup() {
    "$(pwd)"/tests/testacc_cleanup.sh
}

run_suite() {
    suite=${1?}
    log "setup ($1)" && setup
    source "./tests/switch_$suite.sh"
    log "run ($1)" && run || (log "cleanup" && cleanup && exit 1)
    log "cleanup ($1)" && cleanup
}

run_suite "superuser"
run_suite "proxy"
run_suite "rds"
