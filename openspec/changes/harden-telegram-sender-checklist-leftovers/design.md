## Context

当前 sender 已经具备 `config.toml` 配置流、`POST /send` 入队、SQLite 队列、worker 重试与最小恢复逻辑，但 checklist 里剩余的高优先级收尾项还未落地：本地鉴权、幂等、增强版 `sending` 恢复、最小状态查询 API，以及纯文本长度边界。现有实现是单二进制 Go 服务，核心改动会同时触及配置加载、HTTP handler、存储模型、worker 恢复规则和测试套件，因此属于跨模块的加固变更。

## Goals / Non-Goals

**Goals:**
- 为 `POST /send` 增加独立于 Telegram bot token 的本地 sender 鉴权。
- 为发送请求增加入队幂等能力，防止相同业务请求重复创建 job。
- 把 `sending` 恢复从“重启即 failed”提升为“可明确恢复、可继续处理”的语义。
- 提供 `GET /jobs/{job_id}` 与 `GET /healthz` 作为最小状态查询闭环。
- 为纯文本 MVP 增加文本长度上限，并在入队前拒绝超长文本。

**Non-Goals:**
- 不引入 Unix socket。
- 不做富文本、媒体发送、批量 jobs 列表 API。
- 不在这一轮里做 OpenClaw skill 化。
- 不重写当前 sender 的整体架构或替换 SQLite。

## Decisions

### 1. 使用本地 API key 作为 `/send` 鉴权方案
- 在 `config.toml` 顶层新增独立 sender key 配置，由服务启动时加载。
- `POST /send` 必须校验 `Authorization: Bearer <key>`，缺失或错误直接返回 401/403。
- 不复用 Telegram bot token，避免上游拿到发送 token。

**为什么这样做：**
- 与当前 loopback HTTP 形态兼容，改动最小。
- 比改成 Unix socket 更快落地，符合当前 checklist 收尾目标。

**备选方案：**
- Unix socket：安全边界更强，但会扩大改动范围，不适合作为当前第一轮收尾。
- 自定义 header：可行，但 `Authorization: Bearer` 更常见，也更利于后续调用方复用。

### 2. 幂等先只做入队层，不在这一轮补发送后去重表
- 在请求体增加 `idempotency_key`。
- `jobs` 表新增 `idempotency_key` 字段，并为其建立唯一索引。
- handler 入队前按 key 查询已有 job，存在时直接返回已有 job。

**为什么这样做：**
- checklist 里最直接的风险是“同一业务请求重复入队”。入队幂等能先挡住主要重复来源。
- 发送幂等若继续做，需要额外发送记录或更复杂状态模型，会明显扩大本轮范围。

**备选方案：**
- 同时做发送幂等：更完整，但超出这一轮的最小改动目标。
- 不落库、仅内存去重：进程重启后失效，不满足 sender 的可靠性目标。

### 3. `sending` 恢复增强采用 lease 过期回退到 `retry`
- 在 `jobs` 表新增 `lease_expires_at`。
- worker claim job 时写入 `status = sending` 和 `lease_expires_at = now + sendTimeout + buffer`。
- 启动恢复时，仅回收已过期的 `sending` 任务，并将其转回 `retry`，同时记录恢复错误信息。

**为什么这样做：**
- 比“启动即 failed”更接近 checklist 里要求的可恢复语义。
- 不需要分布式锁模型，仍能维持单进程 SQLite 架构。

**备选方案：**
- `locked_at`：也可行，但需要额外推导过期时间；`lease_expires_at` 更直接。
- 继续标记为 `failed`：简单，但无法体现“可自动恢复重试”，与 checklist 收尾目标不匹配。

### 4. 最小状态 API 只提供 job by id 和 health check
- `GET /jobs/{job_id}` 返回 job 关键信息：`job_id`、`status`、`attempt_count`、`last_error`、`created_at`、`updated_at`、`sent_at`。
- `GET /healthz` 返回固定健康响应。
- 不做 jobs 列表/筛选 API。

**为什么这样做：**
- 足够支撑主 agent、人工排障和最小联调闭环。
- 避免把状态查询面做太大。

### 5. 纯文本长度限制在 HTTP handler 前置校验
- 在 handler 中对 `text` 做最大长度检查，超出 Telegram 单条文本上限则返回 400。
- 不做自动分片或富文本兼容。

**为什么这样做：**
- 最符合 checklist 的“第一版直接拒绝，不做复杂分片”。
- 错误尽早发生，避免无意义入队和后续 worker 重试。

## Risks / Trade-offs

- [恢复语义升级会引入 schema 迁移] → 使用 `CREATE TABLE IF NOT EXISTS` 后补 `ALTER TABLE` 风格迁移，保持对已有本地库可升级。
- [入队幂等只解决重复入队，不完全覆盖发送重复] → 在 proposal 范围内接受这一 trade-off，并在 README /后续 proposal 中明确发送幂等仍可后续补充。
- [新增 sender key 配置会影响 config_builder 与 config.toml 契约] → 通过固定字段和 README 更新保持配置入口单一。
- [状态 API 暴露更多内部状态] → 仍保持只读最小字段，不暴露 bot token、sender key 或内部敏感配置。
