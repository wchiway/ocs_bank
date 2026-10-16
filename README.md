# OCS Bank

基于 **Go + chi + SQLite** 的 OCS 题库服务。支持 Chat Completions、Anthropic Messages 和 Responses 三种上游协议；先查人工确认题库，再查 AI 缓存，未命中再调用模型。AI 生成内容不代表已经人工验证，缓存记录保留来源并支持失效。

## 桌面端

桌面端使用 MyGo，提供模型设置、服务启停、运行状态和 OCS 配置导出。首次打开填写模型与 API Key，后续打开自动启动；关闭窗口停止服务。配置与数据库保存在用户目录，默认仅允许本机访问。

```bash
CGO_ENABLED=0 go tool mygo build
```

需要 `go.mod` 指定的 Go 版本；Windows/Linux 还需要系统 WebView 组件。三平台打包、数据目录及 Actions 测试包说明见 [桌面端文档](docs/desktop.md)。CLI 与 Docker 继续使用下面的入口。

## 快速运行

需要 `go.mod` 指定的 Go 版本或兼容更新版本（当前 1.27.1）。

```bash
cp config.example.json config.json
cp .env.example .env
# 编辑 config.json，设置模型、协议和 API 根地址。
# 编辑 .env，填写 AI_API_KEY。不要提交 .env。
go run ./cmd/server -config config.json
```

API 密钥固定从 **配置文件同目录的 `.env`** 读取，格式为 `AI_API_KEY=你的密钥`。例如 `-config /etc/ocs/config.json` 会读取 `/etc/ocs/.env`，与启动目录无关。支持注释和引号；`.env` 缺失、格式错误或密钥为空时启动失败，不回退到进程中的 `AI_API_KEY`。

`config.json` 不再接受 `api_key_env` 或内联 API 密钥，升级时请删除旧的 `api_key_env` 字段。`.env` 已被 Git 和 Docker 构建上下文忽略，仅提交无密钥的 `.env.example`。

默认监听 `127.0.0.1:5000`。配置或密钥缺失时启动失败。运行目录决定相对数据库路径；生产环境建议使用绝对路径。

```bash
xh -I http://127.0.0.1:5000/healthz
xh -I http://127.0.0.1:5000/api/query title=='1+1=?' options=='A. 1 B. 2 C. 3' type==single
```

构建不依赖 CGO 的二进制：

```bash
CGO_ENABLED=0 go build -trimpath -o bin/ocs-bank ./cmd/server
go build -o bin/ocs-migrate ./cmd/migrate
```

## 三种上游协议

| protocol | 默认资源路径 | 示例配置 |
|---|---|---|
| `chat` / `chat_completions` | `/chat/completions` | [config.example.json](config.example.json) |
| `message` / `messages` | `/messages` | [examples/messages.json](examples/messages.json) |
| `response` / `responses` | `/responses` | [examples/responses.json](examples/responses.json) |

`base_url` 是包含版本前缀的 API 根地址，例如 `https://api.example.com/v1`。只追加上表资源路径，不重复添加 `/v1`。兼容网关的子路径也会保留。`endpoint_path` 可覆盖整个 URL 路径，例如 `/custom/v2/generate`；不能包含主机、查询参数或片段。非本机上游必须使用 HTTPS。

三个协议独立构造请求、鉴权和解析响应。第一版只支持非流式文本问答，不执行工具或解析多模态内容。模型必须支持文本 JSON 答案；服务不强制发送厂商特定的结构化输出参数。Chat 默认发送 `max_tokens`，需要新字段的模型可配置 `"chat_token_parameter": "max_completion_tokens"`。

Responses 可选配置 `reasoning_effort`，服务将其发送为 `reasoning: {"effort": "…"}`。允许值为 `none`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max`；具体模型支持哪些值由上游决定。省略时不发送该字段，保持原有行为和上游默认值。此配置仅用于 `response` / `responses`，其他协议配置非空值会导致启动校验失败。

| 配置 | 默认值 | 含义 |
|---|---|---|
| `listen` | `127.0.0.1:5000` | HTTP 监听地址 |
| `database` | `question_bank.db` | SQLite 文件路径 |
| `access_token_env` | 空 | 服务访问令牌变量名，优先从 `.env` 读取，未定义时兼容进程环境 |
| `timeout_seconds` | 60 | 查询与上游重试的总预算，1–600 秒 |
| `max_concurrent` | 4 | 最大同时模型调用数 |
| `max_pending` | 64 | 最大等待调用者数量，包含共享请求 |
| `max_retries` | 2 | 首次调用后的重试次数，0–5 |
| `max_tokens` | 1024 | 对应协议的输出 token 上限；Responses 映射为 `max_output_tokens`，可包含推理与最终答案 |
| `reasoning_effort` | 省略 | 仅 Responses；显式指定模型的推理强度 |
| `requests_per_minute` | 120 | 所有已认证查询共享的固定窗口限流 |
| `cache_ttl_hours` | 720 | 缓存有效期，1–87600 小时 |

`model` 和 `base_url` 必填，模型名称由所选服务商决定。切换模型、端点、协议或显式 `reasoning_effort` 会使用新的 AI 缓存命名空间；人工确认题库不受影响。题型采用 OCS 的 `single`、`multiple`、`judgement`、`completion`；未知题型可以传递给模型，缓存仍按类型隔离。

使用 Qwen Responses 时，可参考 [examples/qwen-responses.json](examples/qwen-responses.json)，其中 `qwen3.8-flash` 使用 `reasoning_effort: "low"`、`max_tokens: 4096`、60 秒总预算。示例中的 `https://api.example.com/v1` 是占位地址，必须按服务商或网关提供的 Responses 端点替换 `base_url`，必要时设置 `endpoint_path`；不能直接假定 Chat Completions 根地址支持 Responses。[阿里云 Responses 文档](https://www.alibabacloud.com/help/zh/model-studio/qwen-api-via-openai-responses) 说明 Qwen3.8 默认推理强度为 `xhigh`，且 `max_output_tokens` 同时限制推理和最终答案。输出预算过小可能在生成完整 JSON 前耗尽，服务会拒绝截断答案。配置与部署步骤见[维护文档](docs/maintenance.md#ocs-连接失败与模型输出排障)。

## 接入 OCS

将 [examples/ocs.json](examples/ocs.json) 的整个 JSON 数组填入 OCS 自定义题库配置。修改 `url` 为浏览器能访问的 `/ocs/query` 地址。使用 `GM_xmlhttpRequest`；按脚本管理器要求允许相应 `@connect` 域名。

OCS 5.2.1 的 [GM 请求实现](https://github.com/ocsjs/ocsjs/blob/8b25a3f44523a0d4b7373878488056f4aa478cf6/packages/core/src/core/utils/request.ts#L48) 只将 HTTP 200 的响应交给 handler；非 200 会先被归为连接失败。`GET /ocs/query` 使用与 `GET /api/query` 相同的认证、限流和查询服务，将业务错误以 HTTP 200 的 JSON 返回，让 handler 能显示具体原因。OCS 的连接测试使用公开的 `HEAD /ocs/query`，无需参数或令牌，不调用模型、不占查询限流额度；测试通过只代表此探测端点可达，不能验证访问令牌、数据库或模型调用。

若启用访问令牌，在题库配置对象增加：

```json
"headers": {"Authorization": "Bearer YOUR_SERVICE_ACCESS_TOKEN"}
```

这里是本服务的访问令牌，不是上游模型密钥。配置示例的 handler 对 `success:false` 按固定 `error_code` 映射抛出中文错误，不显示上游错误文本；成功按 `res.data.code` 返回 `[undefined, 答案]`，未找到返回 `undefined`。升级后请重新导入完整示例，使 URL 与 handler 同时更新。参考 [OCS 官方接口文档](https://docs.ocsjs.com/docs/other/api)。

成功响应：

```json
{"success":true,"data":{"code":1,"data":"2","msg":"AI回答","source":"ai"}}
```

本地缓存或人工题库命中时 `source` 为 `public`。两个查询端点的成功结构相同；未找到或模型拒答返回 HTTP 200 和 `data.code=0, data.data=null`。

`/api/query` 保留 HTTP 状态码：参数错误为 400，访问令牌错误为 401，本地限流为 429，上游失败为 502，容量不足、上游限流或数据库故障为 503，超时为 504，请求取消为 408。`/ocs/query` 的这些业务错误均返回 HTTP 200，正文为 `success:false`、稳定的 `error_code` 和安全的 `error`，不含答案字段或上游内部信息。例如输出达到上限时：

```json
{"success":false,"error_code":"output_limit","error":"output token limit reached; increase max_tokens or reduce reasoning_effort"}
```

新增分类 `output_limit` 表示上游明确报告输出达到上限，`answer_format` 表示答案不是约定的 JSON，`answer_invalid` 表示答案未通过题型校验；其他不兼容响应仍归为 `protocol`。这些错误都不会作为答案返回或缓存。分类与排障步骤见[维护文档](docs/maintenance.md#ocs-连接失败与模型输出排障)。

## 数据和部署

SQLite 自动应用版本化迁移，使用 WAL、5 秒锁等待和单连接池；面向单实例和本地持久磁盘。缓存键包括题目、原始选项、规范题型、协议/端点/模型、显式 `reasoning_effort` 和提示词版本。过期记录不会命中；删除、修正或失效前备份数据库。

人工导入保存到独立的 `reviewed_answers`，不受模型切换或缓存 TTL 影响。schema v2 自动迁移旧 `legacy-reviewed` 中未显式失效且答案无冲突的记录（包括已过期记录）；冲突记录留在原表供复核。普通 AI 缓存不会自动变成人工答案。旧 AI 缓存策略 `answer-v1` 不再命中，避免沿用未做题型校验的答案。升级前备份；回退旧二进制需要恢复旧 schema 的备份。

单选/多选在选项为连续 `A. …`、`B. …` 等逐行标记且内容无歧义时校验选项归属，不猜测字母答案；无明确选项结构时保留兼容性。多选去重，填空保留重复答案和顺序，空分段拒绝；判断题规范为对/错。校验不代表事实正确，OCS 返回结构保持不变。

`upstream_calls` 独立记录每次 HTTP 尝试（含重试、失败）的时间、模型、协议、结果与可获得的 token 用量。缓存命中不增加调用；覆盖答案不会覆盖历史调用。用量缺失记为 NULL，不能当作零费用；记录不包含题目、答案、响应体、端点或密钥。数据库写入失败时保留有效答案并输出脱敏错误，统计不是服务商账单。

将单条 AI 缓存失效：`UPDATE answers SET invalidated=1 WHERE cache_key=?`（使用 SQLite 客户端绑定参数）。失效后下一次查询优先查询人工题库，再按需生成。过期记录不会自动删除；独立 `ocs-maintain` 支持统计、清理预览、分批删除、一致性备份和恢复到新文件，不加载服务配置或密钥。`/api/stats` 提供进程运行指标，`/readyz` 提供数据库就绪检查。使用方法见 [维护文档](docs/maintenance.md)。

容器部署使用 [docker-compose.yml](docker-compose.yml)，直接拉取 GitHub Packages 的 `ghcr.io/wchiway/ocs_bank:latest` 镜像，无需本地构建或设置版本变量。[版本发布流程](docs/releases.md) 在正式发布时同时推送完整版本 tag 和 `latest`；预发布只推送完整预发布 tag，不更新 `latest`。首次使用前需成功发布一个包含此流程的正式版本。

```bash
cp examples/container.json config.json
cp .env.example .env
# 修改模型等配置，在 .env 填写 AI_API_KEY 与 OCS_ACCESS_TOKEN。
mkdir -p data
# Linux 宿主机：镜像以 UID/GID 65532 运行，需要能写入数据目录。
sudo chown 65532:65532 data
docker compose pull
docker compose up -d
```

私密 package 需要先用具备读取权限的 GitHub 身份执行 `docker login ghcr.io`。后续更新时执行 `docker compose pull && docker compose up -d`，拉取并使用更新后的 `latest` 镜像。

Compose 将 `config.json` 和 `.env` 只读挂载到同一个 `/config` 目录。容器配置必须保留 `"listen": "0.0.0.0:5000"`、`"database": "/data/question_bank.db"` 和 `"access_token_env": "OCS_ACCESS_TOKEN"`，不要用本地运行示例中的回环监听地址。宿主机默认发布 `0.0.0.0:5000`，可通过 `OCS_BANK_PORT` 修改宿主机端口；宿主机通过 `http://127.0.0.1:5000` 访问，其他设备使用宿主机 IP。非回环监听要求 `.env` 中配置非空的 `OCS_ACCESS_TOKEN`，OCS 请求需携带对应 Bearer 令牌。

若启动时报 `non-loopback listen requires access_token_env with a nonempty secret`，检查 `config.json` 是否设置了 `"access_token_env": "OCS_ACCESS_TOKEN"`，再确认其同目录 `.env` 包含非空的 `OCS_ACCESS_TOKEN=你自己设置的服务访问令牌`。`access_token_env` 填的是变量名；它与上游密钥 `AI_API_KEY` 是两项配置。只改 `listen` 或只往 `.env` 添加令牌都不够。该错误发生在 `.env` 解析和 `AI_API_KEY` 校验通过之后，并不代表整个 `.env` 未读取。修改文件后执行 `docker compose up -d --force-recreate bank`，让进程重新加载配置，同时刷新可能被编辑器替换过的单文件挂载。

SQLite 目录显式绑定为 `./data:/data`，宿主机文件为 `./data/question_bank.db`，WAL/SHM 文件也保存在同一目录；重建容器后数据仍在。三个挂载路径必须提前准备好，缺失时直接报错。旧 `compose.yaml` 已由 `docker-compose.yml` 替代；从旧部署升级前，先停止旧容器并备份、导出原 `bank-data` 命名卷中的数据到 `./data`，再调整目录及已有数据库文件的属主为 `65532:65532`，避免新容器使用空数据库。

容器内以非 root 用户运行；对公网提供服务时使用 HTTPS 反向代理，保留访问令牌，不暴露数据库。代理超时应大于查询总超时。服务响应 SIGINT/SIGTERM，取消模型调用并进行最长 10 秒的 HTTP 退出等待。

旧 Python 数据迁移与回退见 [docs/migration.md](docs/migration.md)。旧代码只保存在本地 `legacy/python` 和原 upstream，历史包含凭据，禁止推送到新仓库。

## 开发和验证

所有修改遵循 **worktree → 任务分支 → PR → merge**，禁止直接开发或提交到 `main`。详见 [AGENTS.md](AGENTS.md) 和 [CONTRIBUTING.md](CONTRIBUTING.md)。

```bash
gofmt -w cmd internal migrations tests
go vet ./...
go test ./... -timeout 90s
go test -race ./... -timeout 90s
go build ./...
node --test tests/ocs.test.mjs
```

Node.js 仅用于验证 OCS 的 JavaScript handler，不是服务运行依赖。Go 测试覆盖三协议模拟上游、SQLite、并发取消、迁移，以及真实服务进程的认证/缓存重启/优雅退出。CI 同时构建无 CGO 二进制和容器镜像。

测试不使用真实 AI 密钥。真实服务商与浏览器 OCS 的现场联调需在部署环境完成，见 [docs/validation.md](docs/validation.md)。

## 版本发布

推送正式版本 tag（如 `v0.1.0`）自动创建 Release；推送预发布 tag（如 `v0.1.0-rc.1`）自动创建 Pre-release。Release 附件仅使用 GitHub 自动生成的 Source code（zip/tar.gz）；创建 Release 后，并行发布 `ghcr.io/wchiway/ocs_bank:<tag>` 多架构镜像到 GitHub Packages，以及通过 DeepSeek Responses API 生成中文版本总结。需配置 Actions Secret `DEEPSEEK_API_KEY`，详见 [docs/releases.md](docs/releases.md)。
