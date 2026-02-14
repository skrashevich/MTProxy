# Phase 8 Acceptance Report (Cutover and Rollback)

Date: 2026-02-14  
Repository: `/Users/svk/Documents/Projects.nosync/MTProxy`  
Branch: `golang`

## Scope
Phase 8 target from the canonical plan:
- production switch procedure definition,
- rollback window and rollback mechanics,
- validated drill for cutover/rollback sequence.

Acceptance Gate in scope:
- Gate 5: `Cutover and rollback procedures are validated`.

## Source Artifacts
1. Runbook:
- `/Users/svk/Documents/Projects.nosync/MTProxy/docs/go-phase8-cutover-runbook.md`

2. Drill report JSON:
- `/Users/svk/Documents/Projects.nosync/MTProxy/artifacts/phase8/phase8-cutover-report.json`

3. Drill verbose log:
- `/Users/svk/Documents/Projects.nosync/MTProxy/artifacts/phase8/go-phase8.log`

Generated at (UTC): `2026-02-14T10:00:53Z`

## Acceptance Command
```bash
DOCKER_PLATFORM=linux/amd64 docker run --rm --platform linux/amd64 -v "$PWD":/work -w /work golang:bookworm bash -lc 'set -euo pipefail; export PATH=/usr/local/go/bin:$PATH; apt-get update >/dev/null; apt-get install -y build-essential libssl-dev zlib1g-dev >/dev/null; make go-phase8-drill'
```

Result: `PASS` (`ok .../integration/cli 10.263s`).

## Drill Sequence
`TestPhase8CutoverRollbackDrill` executes:
1. `baseline_c` (start C binary and verify `/stats`)
2. `cutover_go` (switch active symlink to Go binary and verify `/stats`)
3. `rollback_c` (switch active symlink back to C binary and verify `/stats`)

All steps passed in one run.

## Measured Results (from JSON)
1. `baseline_c`
- startup: `279.293 ms`
- shutdown: `5.800 ms`

2. `cutover_go`
- startup: `272.687 ms`
- shutdown: `6.935 ms`

3. `rollback_c`
- startup: `261.654 ms`
- shutdown: `6.157 ms`

Guard thresholds:
- startup: `cutover_go` and `rollback_c` <= `baseline_c*3 + 500ms`
- shutdown: `cutover_go` and `rollback_c` <= `baseline_c*3 + 500ms`

All thresholds passed.

## Conclusion
Gate 5 status in repository drill scope (`golang` branch): **PASS**.  
Cutover and rollback procedures are documented and validated by automated Linux `amd64` drill.
