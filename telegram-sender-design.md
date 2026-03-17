# Telegram Bot Sender 设计说明

## 背景

当前目标不是继续排查 OpenClaw 内部 `sessions_send -> announce -> Telegram` 的交付链路，而是直接实现一个**外部 Go 发信层**，让各个伙伴 bot 可以稳定地给指定用户发 Telegram 私聊消息。

已确认事实：

- Telegram 官方 Bot API 可以主动给已建立私聊关系的用户发消息。
- 通过 Go 直调 `sendMessage` 已验证成功。
- 说明问题不在 Telegram 平台能力，而在 OpenClaw 内部链路。

因此，后续方案重点应放在：

1. 如何安全地让 OpenClaw 驱动这个发信层，而**不暴露 bot token**。
2. 是否需要引入队列来减轻主 agent 负担，并提供超时、重试、失败通知能力。
3. 消费者是否需要额外起一个 OpenClaw 或 Claude Code CLI。

---

## 总体结论

### 推荐原则

1. **不要让 OpenClaw 直接持有多个 bot token。**
2. **不要让 OpenClaw 直接执行包含 token 的裸 Go 代码。**
3. **实现一个独立的 Go sender service / worker。**
4. **OpenClaw 只负责“投递消息任务”，不直接负责“调用 Telegram API 发消息”。**
5. **需要队列，但优先从轻量可靠方案开始，不一定一开始就上 beanstalk。**
6. **消费者不需要再起一个 OpenClaw，也不需要 Claude Code CLI。**

---

# 1. 安全设计：如何避免暴露 bot token

## 目标

让 OpenClaw 能驱动发信，但：

- token 不出现在 prompt 中
- token 不写入 memory/workspace 文档
- token 不出现在 shell history / transcript / logs
- token 不交给 agent 推理过程可见

## 错误做法（不要这样）

- 在 prompt 或 MEMORY.md 里写 bot token
- 在 workspace 里保存 token
- 用 `exec` 直接拼接：

```bash
TG_BOT_TOKEN=xxx go run send.go
```

- 让 token 出现在日志、命令行参数、终端历史里

## 正确做法：拆两层

### 层 1：OpenClaw / 主 agent

只负责生成消息任务，例如：

- 发给哪个 bot（逻辑名）
- 发给哪个 chat_id
- 文本是什么
- 可选优先级、超时、重试参数

OpenClaw 不应该知道真正 token。

### 层 2：Go sender service / worker

负责：

- 从安全配置读取 token
- 调 Telegram Bot API
- 管理队列、重试、超时、幂等
- 记录结果

只有这个 Go 服务接触 token。

---

## Token 存储建议

### 方案 A：环境变量（推荐起步）

服务启动时注入：

```bash
BOT_TOKEN_PLANNER=...
BOT_TOKEN_ENGINEER=...
BOT_TOKEN_RESEARCHER=...
BOT_TOKEN_ARCHIVIST=...
BOT_TOKEN_GUARDIAN=...
BOT_TOKEN_CLOSER=...
```

优点：

- 简单
- 不进代码库
- OpenClaw 无需知道 token

适合先做 MVP。

### 方案 B：独立 secrets 文件

例如：

```json
{
  "planner": "xxx",
  "engineer": "yyy"
}
```

要求：

- 文件权限 600
- 不进 git
- 不放在 OpenClaw workspace
- 仅 sender service 进程可读

### 方案 C：系统密钥管理（长期更优）

可选：

- macOS Keychain
- 1Password CLI
- Vault
- 云上 Secret Manager

适合后期升级。

---

# 2. 是否需要队列

## 结论

**需要。**

因为这已经不是“发一条消息”这么简单，而是要处理：

- 主 agent 不被阻塞
- 超时检测
- 自动重试
- 失败回告
- 幂等
- 多 bot 并发
- 服务重启后的恢复

如果把这些逻辑都塞进主 agent，会让：

- 会话污染严重
- 主 agent 被拖慢
- 出错恢复困难
- 行为不可观测

---

## 推荐队列方案

### 最推荐：SQLite + Go worker

原因：

- 单机部署简单
- 易恢复
- 易查状态
- 不额外引入服务
- 足够支撑当前消息规模

### 可选：Redis

如果已经有 Redis，可以考虑。

### 可选：Beanstalk

可以用，但不是首选。

优点：

- 轻量任务队列
- reserve/release/bury 语义清晰

缺点：

- 额外服务
- 单机小规模时不如 SQLite 简洁

## 当前建议

**先用 SQLite。**

---

# 3. 队列中每条任务应包含的字段

建议至少包含：

- `job_id`
- `bot_name`
- `chat_id`
- `text`
- `status`
  - `pending`
  - `sending`
  - `sent`
  - `retry`
  - `failed`
- `attempt_count`
- `max_attempts`
- `next_retry_at`
- `last_error`
- `created_at`
- `updated_at`
- `sent_at`
- `idempotency_key`
- `reply_to_message_id`（可选）
- `disable_notification`（可选）

---

# 4. 超时、重试、最大重试次数

## 单次发送超时

Telegram `sendMessage` 一般很快，建议：

- HTTP 超时：10–15 秒
- 单次发送 attempt 超时：15 秒

超过即判失败。

## 错误分类

### 可重试错误

例如：

- 网络超时
- 临时连接失败
- 5xx
- 429 rate limit
- 上游暂时 unavailable

### 不可重试错误

例如：

- 401 Unauthorized
- 403 bot can't initiate conversation
- 400 chat not found
- 400 malformed request

这些直接标记失败，不要盲重试。

## 最大重试次数

建议默认：

- **3 次**

特殊情况下可扩展到：

- **5 次**

## 退避策略

建议：

- 第 1 次：立即发送
- 第 2 次：10 秒后
- 第 3 次：60 秒后
- 第 4 次（可选）：5 分钟后
- 第 5 次（可选）：15 分钟后

## 429 处理

如果 Telegram 返回 `retry_after`：

- 必须尊重 Telegram 的 `retry_after`
- 使用它覆盖默认退避时间

---

# 5. 幂等设计

必须做幂等，否则重试会导致重复发消息。

## 建议

使用 `idempotency_key`，可由以下信息拼出：

- partner / bot name
- chat_id
- 原始任务 id
- 文本 hash

worker 发送前先查：

- 该 key 是否已经成功发送过

如果已发送，则直接跳过。

---

# 6. 失败后的处理方式

## 推荐流程

如果最终失败：

1. worker 将任务标记为 `failed`
2. 记录 `last_error`
3. 将失败事件暴露给主 agent / 监控任务
4. 由主 agent 决定是否告知用户

例如主 agent 可对用户说：

- 哪个伙伴发信失败
- 已重试几次
- 是否由主 agent 临时代答/转述

这比让 worker 直接对用户发失败说明更合理。

---

# 7. 消费者要不要再起一个 OpenClaw 或 Claude Code CLI

## 结论

**不要。**

消费者本质只是一个普通程序，职责是：

- 读取队列
- 调 Telegram HTTP API
- 记录结果
- 管理重试

它不是 agent，不需要：

- OpenClaw runtime
- Claude Code CLI
- 任何 LLM

## 正确形态

就是一个普通 Go 守护进程 / service。

---

# 8. 推荐的最终架构

## 组件

### 1）OpenClaw / 主 agent

负责：

- 生成消息任务
- 调用 sender service API 或写入任务入口

### 2）Go sender service

负责：

- 接收消息任务
- 校验参数
- 写入 SQLite 队列
- 返回 `job_id`

### 3）Go worker

负责：

- 消费队列
- 调 Telegram API
- 处理重试 / 失败 / 成功回写

---

## 推荐调用方式

OpenClaw 不直接发 Telegram，而是向本地服务发请求：

```http
POST http://127.0.0.1:8787/send
Content-Type: application/json

{
  "bot": "guardian",
  "chat_id": 7098285098,
  "text": "听风，这是一条伙伴直说测试消息。",
  "max_attempts": 3
}
```

sender service 内部把：

- `bot=guardian`

映射为：

- `BOT_TOKEN_GUARDIAN`

然后由 worker 实际发送。

---

# 9. OpenClaw 与 sender service 的边界

## OpenClaw 应该知道的

- bot 的逻辑名
- 用户 chat_id
- 文本内容
- 重试策略（可选）

## OpenClaw 不应该知道的

- 真正 bot token
- Telegram API 调用细节
- 重试实现细节
- 队列消费实现细节

---

# 10. 速率控制建议

即使当前消息量不大，也建议做每 bot 限流。

## 建议

- 每个 bot 使用独立速率限制器
- 先保守按每秒 1–5 条控制
- 不要让多个 worker 同时无脑冲同一个 bot

---

# 11. 最小可用版本（MVP）建议

## 第一阶段

实现：

- Go sender service
- SQLite 队列
- 单 worker
- 3 次重试
- 失败状态记录
- bot token 通过环境变量读取

## 第二阶段

补充：

- 每 bot 限流
- 失败回调给主 agent
- 幂等 key
- 更细粒度错误分类
- 管理接口（查看 pending/retry/failed）

## 第三阶段

可选升级：

- Keychain / Secret Manager
- 多 worker
- Web dashboard
- 失败告警通知

---

# 12. 一句话总结

**最终推荐方案是：实现一个独立的 Go sender 服务，OpenClaw 只投递消息任务，不接触 token；使用 SQLite 队列 + 3 次重试 + 失败后由主 agent 兜底通知。**

---

# 13. 后续可直接交给 Claude 继续做的内容

下一步可以让 Claude 基于本设计直接实现：

1. 目录结构
2. SQLite schema
3. `/send` HTTP API
4. worker 消费循环
5. Telegram `sendMessage` 调用封装
6. 重试策略实现
7. 环境变量读取与 bot name -> token 映射
8. 简单日志与状态查询接口
