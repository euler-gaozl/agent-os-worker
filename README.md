# Agent OS Worker

独立的任务调度、模型传输和 DSH 子进程宿主。执行持久化与迁移属于 Runtime，Worker 消费其固定 `runtime-execution-v1` 接口（模块 `v0.1.0-rc.3`），不导入 Runtime 路由、工具效果或迁移实现。

## 执行与多实例

默认每 300 ms 领取任务，租约 20 秒。多实例需使用不同 `INSTANCE_ID`，通过 Runtime 事务和 fencing 竞争。数据库就绪检查仅覆盖 Worker 所需的六张执行表，不证明 Registry、Storage、ToolHub 或模型可用。

每个任务拥有 Node 子进程。服务身份、供应商 Key 和数据库连接留在 Go 宿主与 Registry，Node 仅收到少量白名单环境。外部调用先登记意图，保存回执；未知效果按原协议处理，不回退到其他域 SQL 或宿主 Shell。

支持三种历史引擎 `legacy-go`、`dsh-0.2.0-rc.2`、`dsh-0.2.0-rc.2-evidence-v1`，固定 runner 与依赖组合不变。源码引擎是另外的明确选择，不能改变未完成任务的引擎。

## 固定源码 Harness

`DSH_SOURCE_RUNNER` 启用 state-v1；`DSH_RESIDENT_RUNNER` 单独启用 resident-v2。各自的产物清单 pin 编译进 Worker，领取前和每次 Node 启动前核验内核、runner、许可证及引擎身份。缺失、替换或不匹配会拒绝执行，不回退版本。产物目录应只读，哈希不是抵御恶意 OS 用户竞态的隔离措施。

源码状态经 Worker 身份调用 Runtime `POST /v1/source-state`，不用通用 ToolHub 写接口。未知传输、5xx 或异常 JSON 按原操作最多尝试 3 次，每次 10 秒并有界退避；仍无法取得回执则进入 `waiting_unknown`，不能根据缺回复伪造成功或失败。管理恢复需匹配的 Runtime `/resume-source-state`，不能用普通失败重试替代。

最终模型请求在 Runtime 加入当前约束/子任务结果、应用运维输出上限后检查。现有 UTF-16 字符/4 + 512 封装 token + 预留输出属于启发式，可能低估非英文；不能声称等价于模型 tokenizer。真实研究已发现固定 runner 输出上限、上下文错误恢复及家族预算问题，修复候选尚未完成，不继续靠加预算宣称验收。

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

包使用 hoisted 依赖，拒绝绝对/目录链接，Unix 命令文件链接需在包内且进入清单。`Test-RelocatedPackage.ps1 -Package <目录>` 真正移动包，确认原路径不存在并运行固定内核；这是同 OS/架构搬迁测试，不使 Windows 二进制在 Linux 运行。Docker 使用自身固定依赖和 UID 10001，默认不带可选源码产物。

## 配置与权限兼容

把 [配置示例](deploy/service.example.json) 保存到仓库外，提供 **Worker 执行域应用账号**，不能使用管理员。`--config` 指向配置，`AGENTOS_NODE` 与 `DSH_RUNNER` 指向包内 Node 和历史 runner；容器/多机显式配置所有服务地址。

`worker --execution-access-profile` 在读取配置或启动 Node 前返回 `worker-minimal-v1`。Windows 维护器核对实际程序、Runtime/Worker 数据库绑定和有效权限；声明不等于已授权或已就绪。

Runtime 独占迁移和授权。旧单仓 Worker readiness 读取更广的表，回滚窗口内必须保留旧 SELECT；最小权限新账号不证明旧程序可回滚。一次更新一个已停止 Worker，把程序、执行模块、runner、锁文件作为固定整体保留。

## 验证状态

真实 Windows Worker/Node、受限 PostgreSQL 和 Runtime HTTP 已验证状态提交后强杀、丢回复、人工恢复及旧/新 Worker 接力；模型、ToolHub 和部分存储为明确夹具。resident-v2 仍为未激活候选，不能把这些测试说成付费模型、跨引擎迁移或完整内存验收。

`AGENTOS_REQUIRE_DSH=1` 使依赖缺失直接失败，不能静默跳过。Actions 日常关闭。参见 [历史适配器](harness/README.md)、[许可证说明](licenses/README.md)、[详细历史证据](docs/history/service-notes-20261014.en.md) 和 [平台交付计划](https://github.com/euler-gaozl/agent-os/blob/main/docs/windows-single-machine-plan.md)。
