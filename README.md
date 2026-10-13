# Agent OS Worker

Independent scheduler, model transport and DSH subprocess host. Runtime owns execution persistence and migrations; the Worker consumes the fixed `runtime-execution-v1` facade from Runtime `v0.1.0-rc.3`. It does not import Runtime routes, tool effects or migrations. Extraction is based on integration commit `752c9081f05733365f46594bd7581ec55b4ad875`.

## Resident-v2 candidate (not activated)

`DSH_SOURCE_RUNNER` still enables only the original source state-v1 artifact.
`DSH_RESIDENT_RUNNER` separately enables resident-v2. Each has an independent
compiled manifest pin, verified before enabling claims and again before every
Node launch. Verified handles carry their engine identity; a missing artifact,
swapped handle, mismatched pin or changed file is rejected without version
fallback. Both variants use the final provider-body budget check and the same
Runtime-owned archive/state/receipt protocol. Existing tasks keep their engine.

`Package-Worker.ps1 -SourceArtifact <v1-root> -ResidentArtifact <v2-root>` includes
both verified artifacts under separate `source-harness` and `resident-harness`
directories. Either option can be omitted; omitted versions must not be enabled
in deployment configuration. Resident-v2 requires a Runtime that explicitly
lists that version, plus matching integration release and rollback guards.
The source-state HTTP name alone is insufficient to prove v2 support.

Local development evidence: 53 Worker test records passed with zero skips;
seven actual Windows Worker/Node tests using restricted PostgreSQL and Runtime
HTTP passed, including committed-state interruption, lost HTTP replies and safe
operator recovery for both source versions. A v1 task started by the old
`70d45cd` Worker was interrupted after commit and completed by the new Worker
without repeating committed model/tool/state effects. Provider, ToolHub routing
and immutable Storage are fixtures, not paid provider/durability acceptance.
Evidence is `p25-worker-resident-unit-b847ca3ea20c4d378c241b948badb6e2` and
`p25-worker-resident-6a61fe5d1e7f4e9ebf2c71de2f2eaf36`. Package relocation and
production activation are separate gates; this candidate has not changed the
live deployment. GitHub Actions remain disabled.

## Build and verify

`worker --execution-access-profile` reports the compiled `worker-minimal-v1`
SQL requirement and exits before loading configuration, opening connections or
starting Node. Only that exact single argument is handled. The release contract
records the same profile; this is a compatibility claim, not a database grant or
proof that a deployment is ready. The Windows coordinator checks the actual
binary, bound Runtime/Worker database and effective login permissions before
startup, update, rollback and pending recovery. Missing metadata is treated as a
legacy requirement; malformed metadata or a different binary report is refused.
The existing engines, pinned Harness assets and execution module are unchanged.

Local candidate evidence `execution-access-build-1e387f7fc44c40e094e2ce0491f0ada7`
records both entrypoint tests, vet and executable build; the actual binary report
was exercised by 44 PostgreSQL/release checks in
`execution-access-release-fdce72a63e154c74a34c70f8caf37a2a`.
Formal activation and permission contraction are separate steps; no model or CI
calls were made for this addition.

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

Default claims remain `legacy-go`, `dsh-0.2.0-rc.2`, and `dsh-0.2.0-rc.2-evidence-v1`. Their DSH runner and package lock remain unchanged. Source-engine support is an explicit candidate opt-in described below; upstream Session history is not yet bounded. See [historical Harness adapter](harness/README.md).

## Source-engine candidate

The separate private `agent-os-harness` repository builds DSH from fixed official source. `worker/source-harness-lock.json` is embedded in the Worker binary and pins its manifest, upstream commit and new engine `dsh-source-639ed015-state-v1`. Set `DSH_SOURCE_RUNNER` to the absolute `source-runner.mjs` from that artifact to enable claims. Startup verifies the compiled manifest pin plus runner/kernel/license digests; each source run rechecks before launching. Missing or changed artifacts fail closed. An editable adjacent manifest cannot authorize another kernel. Artifact directories must be deployed read-only: digest checks are integrity checks, not protection from a malicious OS user racing file reads.

`Package-Worker.ps1 -SourceArtifact <absolute-artifact-root>` adds only the four verified source runtime files and source pin under `source-harness/`. Existing npm engines retain their separate `harness/` directory. Source Node starts with the artifact working directory and `--no-global-search-paths`; it receives the existing small environment allowlist. Runtime/model/service credentials stay in Go. The default Docker image and packages without `-SourceArtifact` carry historical engines only; do not advertise source readiness for them.

`POST /v1/source-state` goes directly to Runtime under the Worker identity. It never accesses state tables or generic ToolHub mutation routes. Runtime freezes each read, rejection and success using the staged operation ID. On uncertain transport, 5xx or malformed JSON the host retries the identical invocation up to three attempts (10 seconds per request with bounded delays). Fence/intent/auth rejections preserve their semantics. If a receipt remains unreachable, the task enters `waiting_unknown`, never a false completed/failed state based solely on missing delivery. The matching Runtime candidate adds control-only `POST /v1/runs/{id}/resume-source-state` after checking that the latest exchange is acknowledged source state and no calls remain unresolved. Its cancellation path can settle this state without replaying the update. Runtime `v0.1.0-rc.2` alone does **not** include that management recovery/cancellation addition; deploy the matching later Runtime candidate recorded by integration. The generic failed-run resume/reconcile buttons cannot substitute for it. Retries are not unbounded.

The final model request is checked **after** Runtime has appended current constraints and child results and applied the operator output cap. It uses the pinned DSH UTF-16 characters/4 heuristic across the serialized provider body, plus 512 framing tokens and reserved output. This closes the invisible-payload accounting gap, but is not the model's exact tokenizer and can underestimate non-English input. Accurate tokenizer/usage calibration remains open; no exact context-fit claim is made. Original user rules, working-set text and tool schemas are never silently truncated.

Local acceptance `p25-worker-source-bf435a1e014f4070a6cdf656445adb22` used a real Windows Worker executable, source DSH, restricted PostgreSQL and Runtime HTTP. After an actual automatic Compact deliberately omitted a dated fact, the next request still had the full working set. Killing Worker after state revision 2 committed but before its reply required fenced replacement; a separate test dropped the HTTP connection while keeping Worker alive. Both finished without repeated model/tool/state effects. Provider, ToolHub routing and immutable Storage were fixtures, not paid/provider or Storage-durability tests. Public submission, deployed activation, native Session restoration and long-history memory plateau are separate remaining gates.

## Migration, upgrade and rollback

Runtime alone creates the execution tables and grants both runtime/worker roles. Worker has no migration executable and cannot access a raw database/transaction through its facade. Only the Runtime `execution` and `internal/runstate` packages may enter the dependency closure. The SDK contains contracts and transport, not this service's SQL.

Runtime's migration gives new Worker roles minimum execution permissions. **The historical monorepo Worker's readiness probe also reads other Runtime tables.** Keep existing legacy read grants during the old/new rollback window. A newly provisioned minimum-privilege role is valid for the new Worker, but is not proof that the old binary can roll back. No legacy grants are revoked by this extraction.

Upgrade one stopped Worker at a time after compatibility verification; pin the executable, Runtime module, runner and lockfile as one release. Do not swap replay-visible engine behavior for unfinished runs. Checkpoint/archive acknowledgement must precede new effects, and uncertain writes require reconciliation. Exact source/module identifiers are in [provenance](source-provenance.json), deployment contracts in [release contract](deploy/release-contract.json). Live activation and cross-version recovery status are recorded by the integration repository, not inferred from successful standalone CI.
