# Go Functional Coverage Report (C -> Go)

Date: 2026-02-14  
Repository: `/Users/svk/Documents/Projects.nosync/MTProxy`  
Branch: `golang`

## Purpose
This document closes Acceptance Gate 4 from the canonical migration plan:
- `C->Go functional coverage is documented`.

Coverage here is functional/behavioral (external contract and runtime behavior), not line-by-line source translation.

## Evidence Summary
1. Full Go test suite:
```bash
go test ./... -count=1
```
Result: `PASS` (all Go/internal/integration packages green).

2. Linux validation gate:
```bash
DOCKER_PLATFORM=linux/amd64 make go-linux-docker-check
```
Result: `PASS` (`go-stability` + `go-dualrun` + `go-phase8-drill`).

3. Dual-run and cutover artifacts:
- `/Users/svk/Documents/Projects.nosync/MTProxy/artifacts/dualrun/phase7-dualrun-report.json`
- `/Users/svk/Documents/Projects.nosync/MTProxy/artifacts/phase8/phase8-cutover-report.json`

4. Automated test inventory:
- `159` Go tests (`rg '^func Test' ...`).

## Functional Coverage Matrix
1. `CLI contract / options / usage / exit codes`
- C anchors: `mtproto/mtproto-proxy.c` option parsing/usage path.
- Go implementation:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/cli/options.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/cli/usage.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/cmd/mtproto-proxy/main.go`.
- Coverage:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/cli/options_test.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/cli/usage_test.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli/main_integration_test.go`.
- Status: `Covered`.

2. `Docker runtime argument parity`
- C/ops anchor: `/Users/svk/Documents/Projects.nosync/MTProxy/docker/run.sh`.
- Go implementation: same CLI contract via Go binary.
- Coverage:
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/docker/run_args_test.go`.
- Status: `Covered`.

3. `Backend config parsing and reload semantics`
- C anchors: `mtproto/mtproto-config.c`, `common/parse-config.c`.
- Go implementation:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/config/config.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/config/manager.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/load.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/lifecycle.go`.
- Coverage:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/config/config_test.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/config/manager_test.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/load_test.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/lifecycle_test.go`,
integration reload tests in `/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli/main_integration_test.go`.
- Status: `Covered`.

4. `Control-plane lifecycle (signals/log reopen/graceful shutdown)`
- C anchors: `engine/engine-signals.c`, `engine/engine.c`, `mtproto/mtproto-proxy.c`.
- Go implementation:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/runtime.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/cmd/mtproto-proxy/main.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/cmd/mtproto-proxy/log_writer.go`.
- Coverage:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/runtime_test.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/cmd/mtproto-proxy/log_writer_test.go`,
integration signal tests in `/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli/main_integration_test.go`.
- Status: `Covered`.

5. `Worker/supervisor semantics (-M, forwarding, crash handling)`
- C anchor: worker fork/signal behavior in `mtproto/mtproto-proxy.c`.
- Go implementation:
`/Users/svk/Documents/Projects.nosync/MTProxy/cmd/mtproto-proxy/supervisor.go`,
worker helpers in `/Users/svk/Documents/Projects.nosync/MTProxy/cmd/mtproto-proxy/main.go`.
- Coverage:
supervisor-focused scenarios in `/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli/main_integration_test.go`,
unit support tests in `/Users/svk/Documents/Projects.nosync/MTProxy/cmd/mtproto-proxy/main_test.go`.
- Status: `Covered`.

6. `Crypto primitives (hash/CRC/AES/DH)`
- C anchors:
`common/md5.c`, `common/sha1.c`, `common/sha256.c`, `common/crc32*.c`,
`net/net-crypto-aes.c`, `net/net-crypto-dh.c`, `crypto/aesni256.c`.
- Go implementation:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/crypto/hash.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/crypto/crc.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/crypto/aes.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/crypto/dh.go`.
- Coverage:
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/crypto/crypto_vectors_test.go`.
- Status: `Covered`.

7. `MTProto framing/protocol decision logic`
- C anchor: packet checks and forward decision path in `mtproto/mtproto-proxy.c`.
- Go implementation:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/protocol/mtproto.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/protocol/frames.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/protocol/state.go`.
- Coverage:
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/protocol/protocol_vectors_test.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/protocol/forward_trace_parity_test.go`.
- Status: `Covered`.

8. `Routing/target selection/failover behavior`
- C anchors: `net/net-rpc-targets.c`, forwarding path in `mtproto/mtproto-proxy.c`.
- Go implementation:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/router.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/forwarder.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/dataplane.go`.
- Coverage:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/router_test.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/forwarder_test.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/dataplane_test.go`.
- Status: `Covered`.

9. `Ingress/egress data plane (TCP flow, exchange, limits)`
- C anchors:
`net/net-tcp-rpc-ext-server.c`, `net/net-tcp-rpc-client.c`, `net/net-tcp-connections.c`.
- Go implementation:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/ingress.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/outbound.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/rate_limiter.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/dataplane.go`.
- Coverage:
unit tests in ingress/outbound/dataplane/rate_limiter test files,
end-to-end ingress/outbound tests in `/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli/main_integration_test.go`.
- Status: `Covered`.

10. `Stats endpoint and observability`
- C anchors: `mtfront_prepare_stats` and `/stats` path in `mtproto/mtproto-proxy.c`.
- Go implementation:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/stats.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/http_stats.go`.
- Coverage:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/stats_test.go`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy/http_stats_test.go`,
integration assertions in `/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli/main_integration_test.go`.
- Status: `Covered`.

11. `Stability and resource pressure`
- Goal from plan: soak/load, FD growth, memory guard.
- Coverage:
`TestSignalLoopIngressOutboundSoakLoadFDAndMemoryGuards`,
`TestSignalLoopIngressOutboundBurstStability`,
`TestSignalLoopOutboundIdleEvictionMetrics`,
`TestSignalLoopOutboundMaxFrameSizeRejectsOversizedPayload`
in `/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli/main_integration_test.go`,
invoked by `make go-stability`.
- Status: `Covered`.

12. `Dual-run Go vs C SLO parity`
- Coverage:
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli/dual_run_integration_test.go`,
artifact `/Users/svk/Documents/Projects.nosync/MTProxy/artifacts/dualrun/phase7-dualrun-report.json`,
acceptance `/Users/svk/Documents/Projects.nosync/MTProxy/docs/go-phase7-acceptance-report.md`.
- Status: `Covered` (Gate 3 `PASS` in branch scope).

13. `Cutover and rollback validation`
- Coverage:
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli/phase8_cutover_integration_test.go`,
artifact `/Users/svk/Documents/Projects.nosync/MTProxy/artifacts/phase8/phase8-cutover-report.json`,
runbook `/Users/svk/Documents/Projects.nosync/MTProxy/docs/go-phase8-cutover-runbook.md`,
acceptance `/Users/svk/Documents/Projects.nosync/MTProxy/docs/go-phase8-acceptance-report.md`.
- Status: `Covered` (Gate 5 `PASS` in drill scope).

## Coverage Boundaries / Notes
1. This report documents functional parity, not source-level one-to-one implementation of internal C engine internals.
2. C and Go `/stats` payloads are both available and validated for health/metrics semantics; key sets are not required to be text-identical.
3. Linux amd64 is the canonical parity environment for C-vs-Go dual-run and cutover drill.

## Conclusion
Gate 4 (`C->Go functional coverage is documented`) is satisfied in branch scope (`golang`):  
functional domains, their Go implementations, and validating tests/artifacts are explicitly mapped in this report.
