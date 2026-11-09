# OCS Bank

基于 **Go + chi + 纯 Go SQLite** 的 OCS 题库与 AI 答案缓存服务，提供无界面的命令行和 Docker 部署方式。支持文字题、图片题，以及 Chat Completions、Anthropic Messages、OpenAI Responses 三种上游协议。

文字题先查人工确认题库，再查有效 AI 缓存，未命中才调用模型。图片题使用原生多模态输入，并按图片内容隔离缓存。**AI 答案及其缓存不等同于人工验证，题型校验也不保证事实正确。**

- [快速开始](#快速开始)
- [接入 OCS 与图片题](#接入-ocs-与图片题)
- [HTTP 接口](#http-接口)
- [模型与服务配置](#模型与服务配置)
- [数据与维护](#数据与维护)
- [升级与版本发布](#升级与版本发布)
- [开发与验证](#开发与验证)

## 主要能力

- **缓存与并发控制**：SQLite 持久缓存、共享在途请求、限流、排队上限、超时及取消传播。
- **多协议图片输入**：PNG、JPEG、GIF、WebP 内联图片，独立图片内容块，不把 Base64 当作文字 prompt。
- **安全错误返回**：失败、截断和无效答案不缓存；OCS handler 根据固定错误码显示提示，不暴露上游响应或密钥。
- **知识与审计分离**：人工答案、AI 缓存和每次上游调用记录独立保存；缓存命中不产生新的模型调用记录。
- **离线维护工具**：数据库统计、清理预览、一致性备份、恢复到新文件及旧 Python 数据迁移。
- **无 CGO 构建**：服务和维护工具不依赖 Node.js、前端构建工具或系统图形组件。

## 快速开始

### 本机运行

需要 [go.mod](go.mod) 指定的 Go 版本或兼容更新版本（当前 **1.27.1**）。以下复制命令仅用于首次配置；已有部署不要覆盖自己的配置和密钥文件。

```bash
cp config.example.json config.json
cp .env.example .env
# 编辑 config.json：填写模型、协议和 API 根地址。
# 编辑 .env：填写 AI_API_KEY；不要提交该文件。
go run ./cmd/server -config config.json
```

默认监听 `127.0.0.1:5000`，数据库为 `question_bank.db`。相对数据库路径基于运行目录；生产环境建议使用绝对路径。

API 密钥固定从**配置文件同目录的 `.env`** 读取，格式为 `AI_API_KEY=你的上游密钥`。例如 `-config /etc/ocs/config.json` 会读取 `/etc/ocs/.env`，与启动目录无关。支持注释和引号；文件缺失、格式错误或 `AI_API_KEY` 为空时启动失败，不回退到进程中的同名变量。

`config.json` 不接受内联 API 密钥或旧的 `api_key_env` 字段。应用设置和秘密必须分开；真实 `.env`、运行配置和数据库不得提交。

检查连接和文字查询：

```bash
xh -I http://127.0.0.1:5000/healthz
xh -I POST http://127.0.0.1:5000/api/query \
  title='1+1=?' options=$'A. 1\nB. 2\nC. 3' type=single
```

这是免令牌的默认本机配置。启用服务访问令牌后，实际查询需携带 `Authorization: Bearer …`；存活检查仍公开。

构建三个无 CGO 工具：

```bash
CGO_ENABLED=0 go build -trimpath -o bin/ocs-bank ./cmd/server
CGO_ENABLED=0 go build -trimpath -o bin/ocs-maintain ./cmd/maintain
CGO_ENABLED=0 go build -trimpath -o bin/ocs-migrate ./cmd/migrate
```

### Docker 部署

[docker-compose.yml](docker-compose.yml) 使用 `ghcr.io/wchiway/ocs_bank:latest`，将配置、密钥只读挂载到 `/config`，将数据库目录挂载到 `/data`。

```bash
cp examples/container.json config.json
cp .env.example .env
# 编辑模型配置，在 .env 填写 AI_API_KEY 与 OCS_ACCESS_TOKEN。
mkdir -p data
# Linux 宿主机：确保镜像用户 UID/GID 65532 可写入该目录。
sudo chown 65532:65532 data
docker compose pull
docker compose up -d
```

三个挂载路径必须提前存在，缺失时直接报错。容器配置应保留：

- `listen`: `0.0.0.0:5000`
- `database`: `/data/question_bank.db`
- `access_token_env`: `OCS_ACCESS_TOKEN`，并在 `.env` 中设置非空的对应令牌。

宿主机默认发布 `0.0.0.0:5000`，可用 `OCS_BANK_PORT` 修改端口。本机浏览器使用 `http://127.0.0.1:5000`，其它设备使用宿主机可达地址。非回环监听必须启用服务令牌；上游密钥 `AI_API_KEY` 不能代替它。

镜像以非 root 用户运行。私密 package 需先通过有读取权限的 GitHub 身份执行 `docker login ghcr.io`。公网部署应使用 HTTPS 反向代理，保留访问令牌，不暴露数据库；代理超时应大于查询总预算，图片请求还需核对代理的请求体大小限制。

**`latest` 只随成功的正式版本发布更新，合并 `main` 不会更新已部署镜像。** 使用图片题前，确认运行版本已包含该功能；尚无对应正式镜像时，可从当前源码构建服务。

## 接入 OCS 与图片题

1. 先启动支持 POST 图片接口的服务。旧的 GET-only 服务直接使用新配置会返回 405。
2. 将 [examples/ocs.json](examples/ocs.json) 的**整个 JSON 数组**填入 OCS 自定义题库配置，修改 `url` 为浏览器可达的 `/ocs/query` 地址。
3. 使用 `GM_xmlhttpRequest`，按脚本管理器提示允许相应 `@connect` 域名。
4. 图片题使用 OCS 5.4.0 或兼容更新版本，在“通用 → 全局设置 → 高级设置”开启“图片题优化”。

新版配置使用 **POST JSON**，通过字段解析器将 `env.images` 作为数组传递，同时发送原始 `title`、`options`、`type` 和带 `[图片N]` 标记的 `suggestion_title`、`suggestion_options`。**不要写成 `"images": "${images}"`，也不要把 Base64 拼进题干或 URL。**

启用服务访问令牌时，合并以下请求头，并保留示例中的其它配置：

```json
{
  "Content-Type": "application/json",
  "Authorization": "Bearer YOUR_SERVICE_ACCESS_TOKEN"
}
```

该对象是题库配置的 `headers` 字段，令牌属于本服务，**不是上游 AI 密钥**。

### 图片能力与限制

模型和兼容网关必须支持所选协议的原生图片输入；文字题成功或配置提醒消失，都不能证明模型已看到图片。

| 项目 | 限制 |
| --- | --- |
| 格式 | PNG、JPEG、GIF、WebP 的 Base64 Data URL |
| 数量 | 最多 6 张，顺序对应 `[图片1]`、`[图片2]` 等标记 |
| 单张大小 | 解码后最多 3 MiB |
| 合计大小 | 解码后最多 12 MiB |
| 尺寸 | 单张最多 2000 万像素，每条边最多 8192 像素；从图片头读取 |
| POST 请求体 | 最多 17 MiB，包含 Base64、题目原文和 JSON 开销 |
| 请求体读取 | 最多 10 秒，且不超过查询总预算 |

服务不下载客户端提供的图片 URL。GET 或空图片数组不能携带内联图片源；原文中的内联 Base64 必须配有图片数组和建议文本，保存与发送文字时使用占位文本替代。图片数组不写入数据库或日志。

连续标记且无歧义的图片选项会还原后校验；无明确结构或多行选项不做猜测。答案中的 `[图片N]` 可由 OCS 按本题图片顺序还原；直接使用 API 的调用者也需处理这种标记。

完整字段、MIME 修正、图片格式要求及排障见[图片题接入文档](docs/images.md)；自定义题库规范参考 [OCS 官方开发文档](https://docs.ocsjs.com/docs/other/api)。

## HTTP 接口

| 方法与路径 | 用途 | 访问控制 |
| --- | --- | --- |
| `POST /ocs/query` | OCS 文字/图片查询，业务错误也返回 HTTP 200 | 服务令牌与共享限流 |
| `POST /api/query` | 文字/图片 JSON 查询，保留业务 HTTP 状态码 | 服务令牌与共享限流 |
| `GET /ocs/query`、`GET /api/query` | 兼容旧的文字查询，不接收图片字段 | 服务令牌与共享限流 |
| `HEAD /ocs/query` | OCS 连接探测，不调用数据库或模型 | 公开，不消耗查询额度 |
| `GET /healthz` | 进程存活检查 | 公开 |
| `GET /readyz` | 数据库就绪检查 | 公开 |
| `GET /api/stats` | 本次运行的指标，不是历史账单 | 服务令牌规则 |

未配置令牌时，默认本机服务免认证。**HEAD 探测通过仅说明端点可达，不验证访问令牌、图片、数据库或模型调用。**

查询成功：

```json
{"success":true,"data":{"code":1,"data":"2","msg":"AI回答","source":"ai"}}
```

缓存或人工题库命中时 `source` 为 `public`。未找到或模型拒答仍返回 HTTP 200，但 `data.code=0`、`data.data=null`。

查询失败：

```json
{"success":false,"error_code":"image_invalid","error":"invalid image data or image limits exceeded"}
```

OCS 的 GM 请求可能不会将非 200 响应交给自定义 handler，因此 `/ocs/query` 用 HTTP 200 承载安全业务错误。`/api/query` 保留状态码：参数/图片错误 400、认证错误 401、过大请求体 413、本地限流 429、上游失败 502、容量不足/上游限流/存储故障 503、上游超时 504、请求取消或请求体读取超时 408。

`image_invalid`、`request_too_large`、`output_limit`、`answer_format`、`answer_invalid` 等错误不带答案、不缓存。示例 handler 只按固定错误码显示中文提示，未知错误使用通用消息，不显示上游内部信息。详见[维护与排障文档](docs/maintenance.md#ocs-连接失败与模型输出排障)。

## 模型与服务配置

| `protocol` | 默认追加资源路径 | 示例配置 |
| --- | --- | --- |
| `chat` / `chat_completions` | `/chat/completions` | [config.example.json](config.example.json) |
| `message` / `messages` | `/messages` | [examples/messages.json](examples/messages.json) |
| `response` / `responses` | `/responses` | [examples/responses.json](examples/responses.json) |

`model` 和 `base_url` 必填，示例模型与地址应按服务商实际要求修改。`base_url` 是 API 根地址，例如 `https://api.example.com/v1`；服务只追加上表资源路径，不额外重复 `/v1`。`endpoint_path` 可覆盖整个路径，但不能包含主机、查询参数或片段。非本机上游必须使用 HTTPS。

三个协议分别构造请求、鉴权和响应解析，使用非流式文本 JSON 答案，不执行工具。文字题保持纯文本请求；图片题按协议生成原生图片内容块。Chat 默认发送 `max_tokens`，需要新字段的模型可设置 `chat_token_parameter: "max_completion_tokens"`。

| 配置 | 默认值 | 含义 |
| --- | --- | --- |
| `listen` | `127.0.0.1:5000` | HTTP 监听地址 |
| `database` | `question_bank.db` | SQLite 文件路径 |
| `access_token_env` | 空 | 服务令牌变量名；优先从 `.env` 读取，未定义时兼容进程环境 |
| `timeout_seconds` | 60 | 查询与上游重试总预算，1–600 秒 |
| `max_concurrent` | 4 | 同时模型调用上限 |
| `max_pending` | 64 | 等待调用者上限，包含共享请求 |
| `max_retries` | 2 | 首次调用后重试次数，0–5 |
| `max_tokens` | 1024 | 协议对应的输出 token 上限；Responses 可包含推理与最终答案 |
| `reasoning_effort` | 省略 | 仅 Responses 的显式推理强度 |
| `requests_per_minute` | 120 | 所有已认证查询共享的固定窗口限流 |
| `cache_ttl_hours` | 720 | AI 缓存有效期，1–87600 小时 |

Responses 的 `reasoning_effort` 允许 `none`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max`，实际支持范围由模型决定。省略时保留上游默认行为；其他协议不能配置非空值。输出预算不足可能在生成完整 JSON 前耗尽，截断答案会被拒绝。

[Qwen Responses 示例](examples/qwen-responses.json) 演示 `reasoning_effort: "low"`、`max_tokens: 4096` 和 60 秒预算，**不代表该模型或网关必然支持图片**。根地址及输出设置必须按上游文档核对，处理方式见[模型输出排障](docs/maintenance.md#ocs-连接失败与模型输出排障)。

## 数据与维护

SQLite 使用 WAL、5 秒锁等待和单连接池，面向单实例及本地持久磁盘。数据库目录与 WAL/SHM 文件需一起保存；不要直接复制或替换活跃数据库主文件。

- **文字人工题库**：`reviewed_answers` 优先，不受模型切换或 AI 缓存 TTL 影响。旧人工表没有图片身份，图片请求不复用这些记录，但不会删除或覆盖它们。
- **AI 缓存**：按题目、原始选项、规范题型、协议/端点/模型、显式推理强度及提示词版本隔离；图片题另加入有序图片摘要和建议文本。旧文字题缓存键保持不变，过期或失效记录不命中。
- **调用审计**：`upstream_calls` 记录每次 HTTP 尝试（含重试/失败）与可获得的 token 用量，不记录题目、答案、响应体、端点或密钥。缺失用量不能当作零费用。

题型采用 `single`、`multiple`、`judgement`、`completion`。连续标记的单选/多选校验选项归属；多选去重，填空保留顺序和重复答案，判断题规范为对/错。无明确选项结构时保留兼容性；这些检查不保证答案的事实正确性。

维护工具不加载服务配置、dotenv 或 AI：

```bash
bin/ocs-maintain -database /srv/ocs/data/question_bank.db -action stats
bin/ocs-maintain -database /srv/ocs/data/question_bank.db -action prune -model your-model
bin/ocs-maintain -database /srv/ocs/data/question_bank.db -action backup -output /srv/ocs/backups/bank-backup.db
```

清理默认只预览，实际删除需显式 `-apply`；备份/恢复输出目录须提前存在，已有目标文件不覆盖。完整规则见[维护文档](docs/maintenance.md)，旧 Python 数据迁移与回退见[迁移文档](docs/migration.md)。

## 升级与版本发布

1. 停服或使用一致性备份保存现有数据，核对示例差异，不覆盖自己的模型配置和秘密。
2. 更新服务二进制或拉取已包含所需功能的正式镜像；Compose 更新可使用 `docker compose pull && docker compose up -d --force-recreate bank`。
3. 修改过配置或密钥后重启服务，刷新可能被编辑器替换过的只读单文件挂载。
4. 重新导入完整 OCS 示例，使 URL、POST 图片字段和错误 handler 同步更新；先做连接探测，再验证少量实际查询。

图片题功能不需要新增数据库 schema；旧数据继续保留。服务自动应用已有版本化迁移；回退旧二进制需要使用与其兼容的数据库备份，不能直接假定新 schema 可由旧程序打开。

正式版本 tag 创建 Release 并发布完整版本 GHCR 镜像，再按构建 digest 安全提升 `latest`；预发布 tag 创建 Pre-release 和完整预发布镜像，不更新 `latest`。**普通提交和 PR 合并不会创建 Release 或更新镜像。** Release 下载仅提供 GitHub 自动生成的源码包，命令行二进制需自行编译。

发布时的 DeepSeek 中文总结与镜像任务相互独立，需由管理员配置 Actions Secret `DEEPSEEK_API_KEY`。发布、通道、重跑和回退规则见[版本发布文档](docs/releases.md)。

## 开发与验证

所有修改，包括文档，都遵循 **worktree → 任务分支 → PR → merge**，禁止直接开发或提交到 `main`。详见 [AGENTS.md](AGENTS.md) 和 [CONTRIBUTING.md](CONTRIBUTING.md)。不要使用 `git push --all` 或 `--mirror`，本地 `legacy/python` 含历史凭据，必须保持本地。

```bash
gofmt -w cmd internal migrations tests
go vet ./...
go test ./... -timeout 90s
go test -race ./... -timeout 90s
CGO_ENABLED=0 go build ./...
node --test tests/*.test.mjs
```

Node.js 22+ 仅用于 OCS、无界面分发约束及发布脚本测试，不是服务运行依赖；无需 npm 包或前端构建。测试使用合成图片、临时 SQLite、本机 mock 上游及隔离服务进程，不需要真实 AI 凭据。

自动化代理在开发与诊断时不得读取、上传真实 dotenv，也不得通过运行程序间接加载它；服务正常运行时读取自己的密钥文件不受此限制。模型与 OCS 的现场联调由部署者在自己的配置环境中完成，验收步骤见[验证文档](docs/validation.md)。
