# Agent OS Worker

Independent scheduler, model transport and DSH subprocess host. Runtime owns execution persistence and migrations; the Worker consumes the fixed `runtime-execution-v1` facade from Runtime `v0.1.0-rc.1`. It does not import Runtime routes, tool effects or migrations. Extraction is based on integration commit `752c9081f05733365f46594bd7581ec55b4ad875`.

## Build and verify

Use Go 1.27.2, PowerShell 7, Node 24 and pnpm 11.25.0. The committed vendor directory contains canonical fixed modules; no neighboring checkout, local `replace`, private credentials or network access is needed for Go builds.

```powershell
$env:GOWORK='off'; $env:GOPROXY='off'
pnpm --dir harness install --frozen-lockfile --ignore-scripts --prod
$env:AGENTOS_REQUIRE_DSH='1'
node --test harness/session-seams.test.mjs
go test -mod=vendor -count=1 ./...
go vet -mod=vendor ./...
./scripts/Verify-Ownership.ps1
./scripts/Build.ps1
# Full independent release, including the selected Node and installed Harness:
./scripts/Package-Worker.ps1 -Destination /absolute/path/to/new-worker-release
```

`Build.ps1` produces only the Worker and healthcheck executables. `Package-Worker.ps1` requires a clean commit and an unused destination; it adds Node, installed locked Harness dependencies, release contract, provenance and a complete SHA-256 inventory. `-Offline -StoreDirectory <retained-pnpm-store>` reconstructs without package downloads. Packages use hoisted dependencies and forbid directory/absolute links so Windows junctions cannot point back to the build directory. Unix command-file links must be relative, resolve inside the package and appear in its manifest. `Test-RelocatedPackage.ps1 -Package <package>` moves the package to a new sibling directory, verifies the original root is absent and hashes/link targets match, then runs the actual kernel through both fixed DSH protocols. This tests portability on the same OS/architecture; it does not make a Windows executable run on Linux. Docker builds its own locked dependencies and runs as UID 10001. Node and pnpm are pinned in CI; packages record the exact selected Node version and binary hash.

The CI matrix tests Windows and Linux, the actual pinned DSH kernel using synthetic model and ToolHub HTTP, provider error/receipt handling, child credential isolation, owner dependency closure, standalone packaging, Linux race checks and image build. These tests do not claim real provider output, durable database recovery or old/new process handoff. Those are separate integration gates. With `AGENTOS_REQUIRE_DSH=1`, missing installed dependencies fail the kernel test rather than silently skip it.

## Run and dependencies

Copy `deploy/service.example.json` outside the repository and supply deployment-specific identities, endpoints and the **Worker execution-domain account**, never an administrator account. Launch the binary with `--config /protected/worker.json`. Set `AGENTOS_NODE` and `DSH_RUNNER` to the packaged Node and `harness/runner.mjs`. Explicit endpoint settings take precedence over local defaults. Service and model credentials stay in the Go supervisor and Registry; the child receives a small environment allowlist and no service tokens, database URL or model key.

Workers poll every 300 ms and use a 20-second lease by default. Deploy multiple instances with unique `INSTANCE_ID` values; they claim through Runtime's transaction/fencing code. Worker HTTP readiness checks its six execution tables. Readiness is not evidence that Registry, Storage, ToolHub or the model provider is available. Dependency failures preserve durable intent and unknown-effect semantics through the Runtime facade; no fallback to another service's SQL or a host shell is permitted.

The current engines remain `legacy-go` for historical tasks, `dsh-0.2.0-rc.2`, and `dsh-0.2.0-rc.2-evidence-v1`. The DSH runner and package lock are unchanged from the integration baseline. P2.5 source-managed Harness and bounded-history changes are **not implemented by this extraction**. See [Harness adapter](harness/README.md).

## Migration, upgrade and rollback

Runtime alone creates the execution tables and grants both runtime/worker roles. Worker has no migration executable and cannot access a raw database/transaction through its facade. Only the Runtime `execution` and `internal/runstate` packages may enter the dependency closure. The SDK contains contracts and transport, not this service's SQL.

Runtime's migration gives new Worker roles minimum execution permissions. **The historical monorepo Worker's readiness probe also reads other Runtime tables.** Keep existing legacy read grants during the old/new rollback window. A newly provisioned minimum-privilege role is valid for the new Worker, but is not proof that the old binary can roll back. No legacy grants are revoked by this extraction.

Upgrade one stopped Worker at a time after compatibility verification; pin the executable, Runtime module, runner and lockfile as one release. Do not swap replay-visible engine behavior for unfinished runs. Checkpoint/archive acknowledgement must precede new effects, and uncertain writes require reconciliation. Exact source/module identifiers are in [provenance](source-provenance.json), deployment contracts in [release contract](deploy/release-contract.json). Live activation and cross-version recovery status are recorded by the integration repository, not inferred from successful standalone CI.
