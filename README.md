# OCS Bank

基于 **Go + chi + SQLite** 的 OCS 题库服务。支持 Chat Completions、Anthropic Messages 和 Responses 三种上游协议；先查本地缓存，未命中再调用模型。AI 生成内容不代表已经人工验证，缓存记录保留来源并支持失效。

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

| 配置 | 默认值 | 含义 |
|---|---|---|
| `listen` | `127.0.0.1:5000` | HTTP 监听地址 |
| `database` | `question_bank.db` | SQLite 文件路径 |
| `access_token_env` | 空 | 服务访问令牌变量名，优先从 `.env` 读取，未定义时兼容进程环境 |
| `timeout_seconds` | 60 | 查询与上游重试的总预算，1–600 秒 |
| `max_concurrent` | 4 | 最大同时模型调用数 |
| `max_pending` | 64 | 最大等待调用者数量，包含共享请求 |
| `max_retries` | 2 | 首次调用后的重试次数，0–5 |
| `max_tokens` | 1024 | 对应协议的输出 token 上限 |
| `requests_per_minute` | 120 | 所有已认证查询共享的固定窗口限流 |
| `cache_ttl_hours` | 720 | 缓存有效期，1–87600 小时 |

`model` 和 `base_url` 必填，模型名称由所选服务商决定。切换模型、端点或协议会使用新的缓存命名空间。题型采用 OCS 的 `single`、`multiple`、`judgement`、`completion`；未知题型可以传递给模型，缓存仍按类型隔离。

## 接入 OCS

将 [examples/ocs.json](examples/ocs.json) 的整个 JSON 数组填入 OCS 自定义题库配置。修改 `url` 为浏览器能访问的地址。使用 `GM_xmlhttpRequest`；按脚本管理器要求允许相应 `@connect` 域名。

若启用访问令牌，在题库配置对象增加：

```json
"headers": {"Authorization": "Bearer YOUR_SERVICE_ACCESS_TOKEN"}
```

这里是本服务的访问令牌，不是上游模型密钥。配置示例的 handler 按 `res.data.code` 读取业务状态，成功返回 `[undefined, 答案]`，未找到返回 `undefined`。参考 [OCS 官方接口文档](https://docs.ocsjs.com/docs/other/api)。

成功响应：

```json
{"success":true,"data":{"code":1,"data":"2","msg":"AI回答","source":"ai"}}
```

缓存命中时 `source` 为 `public`。未找到或模型拒答返回 HTTP 200 和 `data.code=0, data.data=null`。参数错误为 400，访问令牌错误为 401，本地限流为 429，上游失败为 502，容量不足或上游限流为 503，超时为 504。错误正文不包含上游密钥或内部错误信息。

## 数据和部署

SQLite 自动应用版本化迁移，使用 WAL、5 秒锁等待和单连接池；面向单实例和本地持久磁盘。缓存键包括题目、原始选项、规范题型、协议/端点/模型和提示词版本。过期记录不会命中；删除、修正或失效前备份数据库。

将单条缓存失效：`UPDATE answers SET invalidated=1 WHERE cache_key=?`（使用 SQLite 客户端绑定参数）。失效后下一次查询会重新生成。过期记录不会自动删除，可在维护窗口清理。

容器部署：

```bash
cp examples/container.json config.json
cp .env.example .env
# 修改模型等配置，在 .env 填写 AI_API_KEY 与 OCS_ACCESS_TOKEN。
docker compose up -d --build
```

Compose 将 `config.json` 和 `.env` 只读挂载到同一个 `/config` 目录，只发布本机端口，使用持久卷保存 SQLite。容器内以非 root 用户运行；对公网提供服务时使用 HTTPS 反向代理，保留访问令牌，不暴露数据库。代理超时应大于查询总超时。服务响应 SIGINT/SIGTERM，取消模型调用并进行最长 10 秒的 HTTP 退出等待。

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
