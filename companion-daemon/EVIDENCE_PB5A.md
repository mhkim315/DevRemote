# PB.5a evidence

The mux-bound lifecycle tests remain in the repository under the `legacy` build
tag as a migration record; none were deleted. Their V1 replacement coverage is
in `internal/term/pb5b_single_spawn_test.go`:

| Legacy coverage | V1 migration |
| --- | --- |
| create/capture and single-spawn | `TestPB5_V1CreateUsesExactlyOneSpawn` |
| adapter-session cleanup and replacement | `GenerationCleanupCapability` now uses `LaunchIdentity` and `ProcessCleanup` |
| mux managed-process lifecycle tests | native `PTYHandle` typed signal, kill, wait, and cleanup contract |

`go run scripts/archgate.go --task=1` verifies that the production runtime has
one `o.v1Spawn.Spawn` path, has no old launcher field or mux selector, and that
the V1 contract files do not import mux.
