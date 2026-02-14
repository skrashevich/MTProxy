# Phase 8 Runbook: Cutover and Rollback (C <-> Go)

Date: 2026-02-14  
Repository: `/Users/svk/Documents/Projects.nosync/MTProxy`  
Branch: `golang`

## Goal
Provide an operationally safe switch path from C binary to Go binary, with a validated rollback window and reproducible drill.

## Preconditions
1. Phase 7 dual-run acceptance is `PASS`.
2. Linux amd64 validation is available (native or Docker emulation).
3. Runtime config/flags remain contract-compatible:
`docker/run.sh` argument shape, CLI flags, exit semantics.

## Artifacts
1. Go binary: `/Users/svk/Documents/Projects.nosync/MTProxy/objs/bin/mtproto-proxy-go`
2. C binary: `/Users/svk/Documents/Projects.nosync/MTProxy/objs/bin/mtproto-proxy`
3. Phase 8 drill report JSON:
`/Users/svk/Documents/Projects.nosync/MTProxy/artifacts/phase8/phase8-cutover-report.json`
4. Phase 8 drill verbose log:
`/Users/svk/Documents/Projects.nosync/MTProxy/artifacts/phase8/go-phase8.log`

## Cutover Procedure (planned production shape)
1. Freeze config inputs:
- keep the same backend config and secret sources used by C runtime.

2. Pre-warm Go binary in same environment:
- run Go with production-equivalent flags in staging/canary slot.
- confirm health endpoint (`/stats`) and process signal behavior.

3. Switch active runtime to Go:
- update service binary path or image tag to Go build.
- restart with unchanged CLI/env contract.

4. Observe within rollback window:
- check startup and steady-state stats,
- verify error counters and acceptance rates remain within SLO limits.

## Rollback Procedure
1. Trigger condition examples:
- startup failure,
- SLO regression,
- protocol/runtime errors not seen in C baseline.

2. Immediate action:
- restore C binary path/image,
- restart service with unchanged config and flags.

3. Post-rollback checks:
- `/stats` availability,
- baseline request handling restored,
- incident note with captured logs/artifacts.

## Automated Validation Drill
Use the built-in integration drill to validate procedure mechanics:
```bash
make go-phase8-drill
```

Linux-in-Docker reproducible drill:
```bash
DOCKER_PLATFORM=linux/amd64 docker run --rm --platform linux/amd64 -v "$PWD":/work -w /work golang:bookworm bash -lc 'set -euo pipefail; export PATH=/usr/local/go/bin:$PATH; apt-get update >/dev/null; apt-get install -y build-essential libssl-dev zlib1g-dev >/dev/null; make go-phase8-drill'
```

The drill executes `C -> Go -> C` switch sequence on one listener profile and enforces startup/shutdown envelope guards.
