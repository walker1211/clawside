## Why

当前 Telegram sender 已经具备最小可运行链路，但按 `docs/telegram-sender-post-implementation.md` 的 checklist
看，仍缺少几项直接影响可信运行的收尾能力：本地 `/send` 鉴权、幂等、`sending` 恢复增强、最小状态查询
API，以及文本长度边界。现在补齐这些能力，可以把服务从“能跑”提升到“可控、可查、可恢复地跑”，也为后续 skill 化提供稳定边界。

## What Changes

- 为 `POST /send` 增加本地鉴权，拒绝未携带或携带错误 sender key 的请求。
- 为入队增加幂等键支持，重复业务请求返回已有 job，而不是创建重复任务。
- 增强 `sending` 状态恢复语义，避免任务在异常退出后永久卡死，并为安全恢复预留明确状态规则。
- 增加最小状态查询接口：`GET /jobs/{job_id}` 与 `GET /healthz`。
- 收紧纯文本 MVP 的输入边界，对超长文本在入队前直接拒绝。
- 保持现有 TOML 配置流、HTTP handler / store / worker 主结构不变，不引入与 checklist 无关的新能力。

## Capabilities

### New Capabilities

- `telegram-sender-hardening`: 覆盖 sender 本地接口鉴权、入队幂等、发送恢复、最小状态查询 API 与纯文本输入边界约束。

### Modified Capabilities

- 无

## Impact

- 影响代码：`config.go`、`main.go`、`http_handler.go`、`store.go`、`worker.go`、对应 Go 测试文件，以及 `README.md`。
- 影响 API：`POST /send` 请求契约会新增鉴权与幂等要求；新增 `GET /jobs/{job_id}`、`GET /healthz`。
- 影响存储：SQLite `jobs` 表预计需要新增幂等与恢复相关字段或索引。
- 影响运行配置：需要在 `config.toml` 中引入 sender 本地鉴权 key 或等效配置项，且不得与 Telegram bot token 混用。
