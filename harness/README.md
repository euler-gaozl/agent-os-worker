# DSH 执行适配器（历史引擎）

加载官方 DeepSeek Harness 内核 **0.2.0-rc.2**（Cordis 4.0.4），需要 Node 24。使用 `pnpm --dir harness install --frozen-lockfile` 安装，锁文件属于运行版本的一部分，不使用 Python。

`runner.mjs` 组合 DSH 的 AgentRegistry、AgentLoop、ToolRuntime、SessionStore、SystemPrompt、TokenMeter 和 BasicCompactionEngine。DSH 决定后续模型请求与工具分派，适配器把消息转换为平台留存的 OpenAI 兼容传输；Go 宿主管理租约、密钥、真实模型 HTTP、ToolHub 和持久化回执。这是官方内核的编程接口，不是 SDK 客户端或完整交互 CLI，也没有另写一个决策循环。

每个任务拥有一个 Node 子进程，只注册任务快照里的工具，不挂载宿主 Shell、文件系统、动态插件或 DSH 遥测插件。子 Agent 是使用相同内核的平台子任务，等待时 Worker 可以让出执行。进程隔离不等于不可信代码沙箱；可执行工具仍应经过独立 Sandbox。

换行分隔的桥接消息属于 Agent OS 内部协议。每次 model/tool/finish 交互有递增序号、语义输入哈希和已提交回复；重启通过重放恢复 DSH 会话，出现漂移即停止。没有回执不能证明动作没发生。

evidence-v1 在外部动作前，通过事务暂存、不可变 Storage 片段和链接清单保存新增事件；历史引擎保留原完整前缀协议。不使用私有 DSH JSONL 格式或第二个本地会话写入器。参见[证据与兼容契约](https://github.com/euler-gaozl/agent-os/blob/main/docs/architecture/storage-evidence-v1.md)。

Compact 仍由 DSH 负责，受支持的 `summarize` 钩子使用通用任务连续性提示。原始证据保留在事件产物，当前约束单独加入每次新模型请求。绑定 `run_recall` 的 Agent 可重新发现本任务及后代的工具回执和归档；摘要质量仍需真实模型验收。增量传输不代表 Session 常驻历史有界。

升级 DSH 或改变重放可见行为前，必须规划未完成任务兼容并执行恢复/故障测试，不能静默替换活动任务的固定包。源码构建的新引擎由独立 [Harness 仓库](https://github.com/euler-gaozl/agent-os-harness) 管理。

上游：[官方仓库](https://github.com/deepseek-ai/deepseek-harness)、[架构参考](https://deepseek-harness.github.io/deepseek-harness/reference/)。DSH 依赖保留上游 MIT 许可证。
