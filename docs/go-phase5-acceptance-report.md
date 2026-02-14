# Phase 5 Acceptance Report (Data Plane)

Date: 2026-02-14
Repository: `/Users/svk/Documents/Projects.nosync/MTProxy`

## Scope
Phase 5 target from the canonical plan:
- TCP flow and data plane path,
- buffers/timers/connection limits,
- stats parity and runtime observability,
- stability checks (soak/load, FD leak, memory pressure).

## Implemented Coverage
1. Ingress and outbound runtime path:
- ingress TCP accept/read loop + frame handling,
- outbound TCP transport with pooled connections, reconnect, idle eviction.

2. Runtime guards:
- max connection/session limit,
- accept-rate and DH-accept-rate limits,
- outbound max frame-size guard,
- env-tunable outbound timeouts and idle timeout.

3. Supervisor/runtime behavior:
- default runtime start path,
- single-worker binder policy for stats/ingress/outbound in `-M` mode.

4. Observability:
- `/stats` exports dataplane/ingress/outbound counters,
- explicit metrics for rejected/rate-limited/evicted/error scenarios.

## Acceptance Commands and Results
1. Stability gate:
```bash
make go-stability
```
Result: `PASS` (`ok github.com/TelegramMessenger/MTProxy/integration/cli 10.261s`).

2. Protocol trace parity gate:
```bash
go test ./integration/protocol -run TestForwardMTProtoTraceParityWithCLogic -count=1
```
Result: `PASS`.

3. Full non-cached test suite:
```bash
go test ./... -count=1
```
Result: `PASS`.

## Stability Criteria Covered
1. Burst stability:
- `TestSignalLoopIngressOutboundBurstStability`

2. Idle pool stability:
- `TestSignalLoopOutboundIdleEvictionMetrics`

3. Oversize/memory guard:
- `TestSignalLoopOutboundMaxFrameSizeRejectsOversizedPayload`

4. Soak/load + resource growth guard:
- `TestSignalLoopIngressOutboundSoakLoadFDAndMemoryGuards`
- includes threshold checks:
  - FD growth budget: `+32`,
  - RSS growth budget: `+160 MiB`,
  - zero-error dataplane/ingress counters during replay window.

## Conclusion
Phase 5 acceptance gates in this repository are currently green:
- stability test gate passes,
- trace parity gate passes,
- full suite passes non-cached.

Open migration work remains in later phases (`Phase 7+`): dual-run SLO comparison, production cutover, rollback validation.
