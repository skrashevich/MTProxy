# Phase 7 Dual-Run Baseline (C vs Go)

## Scope
- Harness: `/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli/dual_run_integration_test.go`
- Test: `TestDualRunDataplaneCanarySLO`
- Environment: Linux `amd64` (Docker, `golang:bookworm`)

## Command
```bash
docker run --rm --platform linux/amd64 -v "$PWD":/work -w /work golang:bookworm bash -lc 'set -euo pipefail; export PATH=/usr/local/go/bin:$PATH; apt-get update >/dev/null; apt-get install -y build-essential libssl-dev zlib1g-dev >/dev/null; MTPROXY_DUAL_RUN=1 go test -v ./integration/cli -run TestDualRunDataplaneCanarySLO -count=1'
```

## Result
- Status: `PASS`
- Dataplane canary metrics:
  - `go_connect success=1.000 p95=3.70125ms avg=781.751us`
  - `c_connect success=1.000 p95=1.296042ms avg=465.461us`
  - `go_stats success=1.000 p95=46.363958ms avg=5.864242ms`
  - `c_stats success=1.000 p95=38.487626ms avg=14.487599ms`
  - `go_shutdown=6.642667ms c_shutdown=5.965333ms`

## SLO Gates in Harness
- Success-rate regression guard: Go cannot be worse than C by more than `0.02` absolute.
- Latency guard (p95):
  - connect: `go_p95 <= c_p95 * 2.0 + 25ms`
  - stats: `go_p95 <= c_p95 * 2.5 + 40ms`
- Shutdown guard: `go_shutdown <= c_shutdown * 2 + 250ms`

## Notes
- Dual-run harness requires `linux/amd64`.
- In test environment both binaries are started with `-u root` to avoid user-switch failures in containerized runs.
