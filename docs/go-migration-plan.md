# Canonical Plan: C -> Go Migration for MTProxy with Multi-Agent Execution

## Summary
The plan is fixed as a single source of truth in the repository and is executed by three independent agents in one shared branch with strict path ownership.
Goal: `pure Go`, full behavioral parity with the current C version, and staged `dual-run` rollout.

## Plan Storage (what "save" means)
1. Official plan file: `/Users/svk/Documents/Projects.nosync/MTProxy/docs/go-migration-plan.md`.
2. Current version: `v1` (this specification).
3. Any plan change is made only as a separate commit with prefix `plan:` and a changelog block at the end of the file.
4. Additional coordination file: `/Users/svk/Documents/Projects.nosync/MTProxy/docs/go-migration-coordination.md`.
5. Shared execution branch: `golang`.

## Public API / Interface Commitments
1. External contract remains 1:1:
`mtproto-proxy` CLI, flags, exit codes, `/Users/svk/Documents/Projects.nosync/MTProxy/docker/run.sh` behavior, env vars, and tg/t.me link format.
2. Backend config format and runtime reload semantics are preserved.
3. New contracts are allowed only as internal Go interfaces:
`internal/engine.Runner`, `internal/crypto.CipherSuite`, `internal/proxy.Session`, `internal/worker.Supervisor`.
4. Any breaking external behavior change is forbidden until migration completion and separate approval.

## Ownership Model for 3 Independent Agents
1. `codex` owner:
`/Users/svk/Documents/Projects.nosync/MTProxy/cmd/mtproto-proxy`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/cli`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/config`,
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/cli`,
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/docker`.
2. `claude code` owner:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/netx`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/engine`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/worker`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/stats`,
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/load`.
3. `z.ai glm coder` owner:
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/crypto`,
`/Users/svk/Documents/Projects.nosync/MTProxy/internal/proxy`,
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/protocol`,
`/Users/svk/Documents/Projects.nosync/MTProxy/integration/crypto`.
4. Cross-cutting files are editable only by the integrator:
`/Users/svk/Documents/Projects.nosync/MTProxy/go.mod`,
`/Users/svk/Documents/Projects.nosync/MTProxy/.github/workflows/*`,
`/Users/svk/Documents/Projects.nosync/MTProxy/Dockerfile`,
`/Users/svk/Documents/Projects.nosync/MTProxy/README.md`.

## Implementation Phases (decision-complete)
1. Phase 0 Baseline:
Inventory C functions/global state and create golden baseline for help/exit codes/config/stats/docker behavior.
2. Phase 1 Go Skeleton:
`go.mod`, empty runnable binary, CI for `go build` plus smoke parity.
3. Phase 2 Control Plane Parity:
CLI/options/env/config bootstrap/signals/logging/graceful shutdown.
4. Phase 3 Config and Targets:
Backend config parser and target management parity.
5. Phase 4 Crypto and Protocol:
AES/DH/SHA/CRC plus MTProto framing/state parity on vectors.
6. Phase 5 Data Plane:
TCP flow, buffers, timers, connection limits, stats endpoint parity.
7. Phase 6 Worker Semantics:
`-M` mode via supervisor+worker subprocess model with signal semantics.
8. Phase 7 Dual-run Rollout:
Canary traffic and SLO comparison against C.
9. Phase 8 Cutover:
Production switch, rollback window, deprecate C pipeline.

## Integration Rules in One Common Branch
1. Every commit must include agent tag:
`[agent:codex]`, `[agent:claude]`, `[agent:glm]`.
2. Commits are allowed only in owned paths; violations block integration.
3. Rebase is required before push to `golang`.
4. Integrator runs daily conflict scan and parity status report in
`/Users/svk/Documents/Projects.nosync/MTProxy/docs/go-migration-coordination.md`.
5. Any cross-cutting change is done only as an integrator "integration patch".

## Test Cases and Scenarios (mandatory)
1. CLI parity:
`--help`, invalid/valid `-S/-P`, ports, required args, exit codes.
2. Docker parity:
Env scenarios from `/Users/svk/Documents/Projects.nosync/MTProxy/docker/run.sh`.
3. Config parity:
Valid/invalid backend configs and reload behavior.
4. Crypto vectors:
SHA1/SHA256/HMAC/CRC/AES/DH byte parity.
5. Protocol parity:
Handshake and packet exchange, C vs Go trace comparison.
6. Worker parity:
`-M`, `SIGTERM`, `SIGUSR1`, worker crash and supervisor reaction.
7. Stability:
Soak/load/FD leak/memory pressure.
8. CI matrix:
Linux build/test/smoke, macOS build/smoke, Docker multi-arch build.

## Acceptance Gates
1. External 1:1 contract is confirmed by tests.
2. All mandatory tests are green.
3. Dual-run is not worse than C on agreed SLO.
4. C->Go functional coverage is documented.
5. Cutover and rollback procedures are validated.

## Assumptions and Defaults
1. `pure Go only`, no `cgo`.
2. Full parity on first Go production release.
3. Rollout only via `dual-run`.
4. Single shared branch is an explicit organizational choice.
5. Conflict management uses strict path ownership only.
