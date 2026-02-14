# Phase 7 Acceptance Report (Dual-Run Rollout)

Date: 2026-02-14  
Repository: `/Users/svk/Documents/Projects.nosync/MTProxy`  
Branch: `golang`

## Scope
Phase 7 target from the canonical plan:
- dual-run comparison of Go vs C runtime behavior,
- canary/load traffic checks with SLO guards,
- evidence artifact suitable for CI and review.

Acceptance Gate in scope:
- Gate 3: `Dual-run is not worse than C on agreed SLO`.

## Source Artifact
- JSON report: `/Users/svk/Documents/Projects.nosync/MTProxy/artifacts/dualrun/phase7-dualrun-report.json`
- Verbose log: `/Users/svk/Documents/Projects.nosync/MTProxy/artifacts/dualrun/go-dualrun.log`
- Generated at (UTC): `2026-02-14T09:34:40Z`
- Environment: `linux/amd64` (Docker)

## Acceptance Command
```bash
DOCKER_PLATFORM=linux/amd64 docker run --rm --platform linux/amd64 -v "$PWD":/work -w /work golang:bookworm bash -lc 'set -euo pipefail; export PATH=/usr/local/go/bin:$PATH; apt-get update >/dev/null; apt-get install -y build-essential libssl-dev zlib1g-dev >/dev/null; make go-dualrun-report'
```

Result: `PASS`

## Executed Dual-Run Tests
1. `TestDualRunControlPlaneSLO`
2. `TestDualRunDataplaneCanarySLO`
3. `TestDualRunDataplaneLoadSLO`

All three tests passed in one run (`ok .../integration/cli 13.311s`).

## SLO Summary (from artifact)
1. Control plane:
- shutdown guard: `go <= c*2 + 200ms`
- observed: `go=4.8315ms`, `c=8.888667ms` -> pass.

2. Dataplane canary:
- success-rate guard: `go + 0.02 >= c` (connect/stats),
- p95/p99 guards on connect/stats vs C baseline,
- shutdown guard: `go <= c*2 + 250ms`.
- observed key metrics:
  - connect: `go p95=12.850166ms`, `c p95=47.404167ms`
  - stats: `go p95=56.7975ms`, `c p95=33.89775ms`
  - both success-rate values: `1.000`
  - all guards passed.

3. Dataplane load:
- higher attempt/concurrency profile,
- success-rate + p95 + p99 + shutdown guards.
- observed key metrics:
  - connect: `go p95=2.4825ms`, `c p95=6.366334ms`
  - stats: `go p95=33.786334ms`, `c p95=81.226292ms`
  - connect p99: `go=4.958583ms`, `c=7.539666ms`
  - stats p99: `go=54.895876ms`, `c=82.297041ms`
  - both success-rate values: `1.000`
  - all guards passed.

## Conclusion
Gate 3 status in this repository branch (`golang`): **PASS**.  
Phase 7 dual-run SLO evidence is present and reproducible via `make go-dualrun-report`.

Open migration work remains in Phase 8:
- cutover procedure validation,
- rollback procedure validation.
