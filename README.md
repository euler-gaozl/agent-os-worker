# Agent OS Worker

独立的任务调度、模型传输和 DSH 子进程宿主。执行持久化与迁移属于 Runtime，Worker 消费其固定 `runtime-execution-v1` 接口（模块 `v0.1.0-rc.5.0.20261015080014-1f6c4c29c62b`），不导入 Runtime 路由、工具效果或迁移实现。

## 执行与多实例

默认每 300 ms 领取任务，租约 20 秒。多实例需使用不同 `INSTANCE_ID`，通过 Runtime 事务和 fencing 竞争。数据库就绪检查仅覆盖 Worker 所需的六张执行表，不证明 Registry、Storage、ToolHub 或模型可用。

每个任务拥有 Node 子进程。服务身份、供应商 Key 和数据库连接留在 Go 宿主与 Registry，Node 仅收到少量白名单环境。外部调用先登记意图，保存回执；未知效果按原协议处理，不回退到其他域 SQL 或宿主 Shell。

支持三种历史引擎 `legacy-go`、`dsh-0.2.0-rc.2`、`dsh-0.2.0-rc.2-evidence-v1`，固定 runner 与依赖组合不变。源码引擎是另外的明确选择，不能改变未完成任务的引擎。

## 固定源码 Harness

`DSH_SOURCE_RUNNER` 启用 state-v1；`DSH_RESIDENT_RUNNER` 单独启用 resident-v2。各自的产物清单 pin 编译进 Worker，领取前和每次 Node 启动前核验内核、runner、许可证及引擎身份。缺失、替换或不匹配会拒绝执行，不回退版本。产物目录应只读，哈希不是抵御恶意 OS 用户竞态的隔离措施。

`DSH_BUDGET_RUNNER` 是独立 budget-v3 候选，传递任务快照的有效输出上限。
上下文预检拒绝不登记模型调用，交由 Runtime 在原 fence 下冻结；成功结果及 usage
从已提交原回执派生。最终 provider body 与原意图/快照的模型、工具和参数完整绑定。
网络、401、预算、取消、存储或截断仍明确失败，不伪装成可压缩的上下文拒绝。
候选单元/真实 Node 协议检查已通过；完整发布包、联合进程恢复和正式安装仍需另验。

`DSH_NATIVE_RUNNER` 是独立 native-v4 候选，传递服务端发现并冻结的模型能力，
生成和压缩均用模型支持的最大输出，取消旧字符估算预发送拒绝和家族 token 配额。
传输保留请求/响应各 32 MiB 字节保护、2 分钟发送/首响应保护及独立响应读取空闲保护；
没有 90 秒整次生成截止。只有完整、可识别的 HTTP 400 上下文错误可进入压缩恢复，
真实发送的拒绝回执也持久化并计次。网络中断、部分回包和截断不伪装为未发送。
native 的任务状态 HTTP 允许 64 MiB 响应，普通工具及历史引擎仍为 4 MiB。
Node 堆配置 512 MiB、帧 128 MiB，仅作用于 native；这不是进程 RSS 或平台内存验收结论。

源码状态经 Worker 身份调用 Runtime `POST /v1/source-state`，不用通用 ToolHub 写接口。未知传输、5xx 或异常 JSON 按原操作最多尝试 3 次，每次 10 秒并有界退避；仍无法取得回执则进入 `waiting_unknown`，不能根据缺回复伪造成功或失败。管理恢复需匹配的 Runtime `/resume-source-state`，不能用普通失败重试替代。

最终模型请求在 Runtime 加入当前约束/子任务结果、应用运维输出上限后检查。现有 UTF-16 字符/4 + 512 封装 token + 预留输出属于启发式，可能低估非英文；不能声称等价于模型 tokenizer。真实研究已发现的执行问题，仍须用 v3 正式安装后的有界模型与业务成稿验收确认。

## 构建与完整发布包

需要 Go 1.27.2、PowerShell 7、Node 24、pnpm 11.25.0。固定 Go 模块已随 `vendor` 提交，Go 构建不需邻近仓库或凭证。

```powershell
$env:GOWORK='off'; $env:GOPROXY='off'
pnpm --dir harness install --frozen-lockfile --ignore-scripts --prod
$env:AGENTOS_REQUIRE_DSH='1'
node --test harness/session-seams.test.mjs
go test -mod=vendor -count=1 ./...
go vet -mod=vendor ./...
./scripts/Verify-Ownership.ps1
./scripts/Build.ps1
./scripts/Package-Worker.ps1 -Destination <新的绝对发布目录>
```

`Build.ps1` 只生成 Worker/healthcheck。`Package-Worker.ps1` 要求干净提交与新目录，加入 Node、固定 Harness 依赖、发布契约、来源和完整 SHA-256 清单。`-Offline -StoreDirectory <保留的pnpm-store>` 可离线重建。需要源码引擎时加 `-SourceArtifact <v1目录>` / `-ResidentArtifact <v2目录>`，未打包的版本不得在配置中启用。

v3 另外使用 `-BudgetArtifact <v3目录>`，三套固定产物可共存。搬迁检查会执行包内
所有声明的源码引擎，不能用旧 runner 能启动来代替新版本验证。
v4 使用 `-NativeArtifact <v4目录>`；四套源码产物分别固定，旧编译 pin 和历史 runner 不变。

包使用 hoisted 依赖，拒绝绝对/目录链接，Unix 命令文件链接需在包内且进入清单。`Test-RelocatedPackage.ps1 -Package <目录>` 真正移动包，确认原路径不存在并运行固定内核；这是同 OS/架构搬迁测试，不使 Windows 二进制在 Linux 运行。Docker 使用自身固定依赖和 UID 10001，默认不带可选源码产物。

## 配置与权限兼容

把 [配置示例](deploy/service.example.json) 保存到仓库外，提供 **Worker 执行域应用账号**，不能使用管理员。`--config` 指向配置，`AGENTOS_NODE` 与 `DSH_RUNNER` 指向包内 Node 和历史 runner；容器/多机显式配置所有服务地址。

`worker --execution-access-profile` 在读取配置或启动 Node 前返回 `worker-minimal-v1`。Windows 维护器核对实际程序、Runtime/Worker 数据库绑定和有效权限；声明不等于已授权或已就绪。

Runtime 独占迁移和授权。旧单仓 Worker readiness 读取更广的表，回滚窗口内必须保留旧 SELECT；最小权限新账号不证明旧程序可回滚。一次更新一个已停止 Worker，把程序、执行模块、runner、锁文件作为固定整体保留。

## 验证状态

真实 Windows Worker/Node、受限 PostgreSQL 和 Runtime HTTP 已验证状态提交后强杀、丢回复、人工恢复及旧/新 Worker 接力；这些故障中的模型、ToolHub 和部分存储为明确夹具。Windows 已安装支持 resident-v2 的 Worker 并有独立的受控真实供应商验证，默认新根仍为 v1。budget-v3 尚未安装，完整内存与研究验收仍未完成。

`AGENTOS_REQUIRE_DSH=1` 使依赖缺失直接失败，不能静默跳过。Actions 日常关闭。参见 [历史适配器](harness/README.md)、[许可证说明](licenses/README.md)、[详细历史证据](docs/history/service-notes-20261014.en.md) 和 [平台交付计划](https://github.com/euler-gaozl/agent-os/blob/main/docs/windows-single-machine-plan.md)。

native 候选本地全仓 96 条检查通过、零跳过，包含真实 TLS 大消息传输、回执缓存、
请求上传停滞、响应持续进展/停滞/取消、实际固定 Node 读取 4.5 MB HTTP 任务状态并返回
超过 5 MiB 文本、产物篡改拒绝和历史回归。模型/执行账本为明确夹具，不是付费模型、
实际数据库接管或正式安装证明。独立评审发现的旧状态响应容量和上传无截止问题已修复。
原始证据由本机 `ops/native-worker.path` 索引，联合进程故障和发布仍待完成。

## Native 模型尝试的约束修订保护

当前编译依赖固定 Runtime 提交 `1f6c4c29c62bac3ad6d674b7797a7410f373bf00`，对应上述 Go pseudo-version。它在新模型尝试前核对冻结意图与当前约束修订；不一致则按普通失败落盘，不新增供应商请求。已完成回执仍可恢复、重放，旧 PlanRevision 不会被更新。本检查不能撤回已获准的在途调用，其账单与回执仍须保留。该行为由 Worker 内嵌的 execution 执行，单独更新 Runtime HTTP 服务不能替代 Worker 升级。

该版本目前是维护者本地固定源码，尚未发布远程 tag 或模块；`go.sum` 由 Go 对精确 Git 提交的合法模块包生成，完整 vendor 随源码保留，不依赖邻近仓库的 replace。升级同时纳入 rc.5 以来已有的只读 continuation preview 和 native/legacy 提交校验代码；execution facade、官方 Harness、SDK rc.3 和其它第三方依赖版本不变。

Mac 开发安装需逐个验证两个可领取任务的 Worker 的实际二进制与编译 pin；不能把 go.mod 更新视为已经部署。Runtime HTTP 可以继续使用兼容的既有 preview 版本，不需要为此迁移数据库。新 continuation 写动作、UI、真实供应商、业务与 Windows 验收仍按主仓完整计划另行记录。
## Windows 接续发布兼容（2026-10-17）

发布契约新增 `continuation_management` 的 `native-model-revision-guard-v1` 能力。`source-provenance.json` 已修正为实际编译使用的 Runtime execution `v0.1.0-rc.5.0.20261015080014-1f6c4c29c62b`，与 go.mod、go.sum 和 vendor 一致；这次没有再次改变执行依赖。平台在启用接续 Runtime 前检查全部在运行的 Worker，启用后拒绝回滚到缺少守卫的 Worker。历史引擎与回放协议保持原版本。声明与源码检查不是 Windows 安装或真实模型验收，实际部署结果由集成仓记录。
