# 诊断、统计与数据库维护

## 运行状态

- `GET /healthz`：进程存活，不调用模型。
- `HEAD /ocs/query`：OCS 连接探测，公开返回 200；无需题目或令牌，不调用查询服务、数据库或模型，不占查询限流额度。它不能验证实际搜题是否成功。
- `GET /readyz`：检查服务未关闭且数据库能在 1 秒预算内响应；不可用返回 503，不返回内部错误。
- `GET /api/stats`：JSON 进程统计，使用与 `/api/query` 相同的 Bearer 令牌规则；仅在未配置令牌的本机部署中免认证。

`/api/stats` 返回 `scope: "process"` 与 `counters`。计数从本次启动开始，未出现的计数可视为零：

| 字段 | 含义 |
| --- | --- |
| `queries_total` | 所有查询请求，包括认证和参数拒绝 |
| `query_result_*` | 查询结果分类，例如 ok、invalid、unauthorized、timeout、storage、output_limit、answer_format、answer_invalid |
| `query_layer_reviewed/cache/generated/none` | 人工题库、AI 缓存、生成答案或无答案 |
| `shared_waiters_total` | 加入已有共享生成的等待者 |
| `pending_generations`、`active_generations` | 尚未结束的生成任务（含排队）、正在调用生成器的任务 |
| `queue_count`、`queue_ms_total` | 排队次数及累计等待毫秒 |
| `generation_count`、`generation_ms_total` | 生成器调用次数及累计耗时（含重试/记录保存） |
| `upstream_calls_total`、`upstream_result_*` | 实际 HTTP 尝试及分类，包含重试 |
| `upstream_count`、`upstream_ms_total` | HTTP 尝试次数及累计耗时，不含重试等待 |
| `input_tokens_known_total`、`output_tokens_known_total` | 上游返回且通过数值检查的已知用量 |
| `upstream_usage_unknown_total` | 用量缺失或无效的尝试，不能当作零费用 |
| `cache_write_failures_total`、`call_record_write_failures_total` | 缓存或调用记录保存失败 |

平均排队时间可由累计毫秒除以次数得到；这些累计计数不提供 P95 分位数。多个请求共享一次生成时，请求数与调用数不同。进程计数重启清零，历史统计使用下面的维护命令。查询和生成日志仅含 ID、时间、命中层级和固定错误类别，不记录题目、答案或密钥。

## OCS 连接失败与模型输出排障

先区分连接探测、实际查询和模型返回。OCS 5.2.1 的 [GM 请求实现](https://github.com/ocsjs/ocsjs/blob/8b25a3f44523a0d4b7373878488056f4aa478cf6/packages/core/src/core/utils/request.ts#L48) 只处理 HTTP 200，非 200 响应不会交给自定义 handler，所以模型返回错误也可能显示为“连接失败”。部署包含 `/ocs/query` 的新版本后，重新导入 [examples/ocs.json](../examples/ocs.json) 并修改浏览器可达地址；要同时更新 URL 和 handler。使用服务访问令牌的部署仍须配置 `Authorization: Bearer …`。连接测试的公开 `HEAD /ocs/query` 不检查该令牌；实际 `GET /ocs/query` 继续执行认证和限流。

`/api/query` 保持原 HTTP 状态码，适合 API 调用和监控；`/ocs/query` 使用相同查询逻辑，将业务错误返回为 HTTP 200 与 `{"success":false,"error_code":"…","error":"…"}`。OCS 示例 handler 仅用固定错误码映射中文提示，未知错误使用通用提示，不拼接上游错误文本。错误响应不带答案，失败结果不会写入答案缓存；未找到或拒答仍是成功响应中的 `data.code=0`。

| `error_code` / 日志分类 | 证据与处理 |
| --- | --- |
| `output_limit` | 上游明确报告达到输出上限：Responses 的 `status=incomplete` 且 `incomplete_details.reason=max_output_tokens`，或 Chat 的 `finish_reason=length`、Messages 的 `stop_reason=max_tokens`。增加 `max_tokens`；Responses 可在模型支持时降低 `reasoning_effort`。截断输出不会被当作完整答案。 |
| `answer_format` | 未返回约定的 JSON 答案。检查模型是否适合按提示输出 JSON，必要时重试或更换模型。它与明确的输出上限分类分开。 |
| `answer_invalid` | JSON 答案未通过题型或选项校验。检查题型和选项是否完整，必要时换模型或人工复核。 |
| `protocol` | 其他响应结构或协议不兼容。核对模型支持的协议、API 根地址和 `endpoint_path`；不能仅凭此分类认定输出已达到上限。 |
| `timeout` | 请求总预算已用尽。结合队列与上游耗时检查；降低 OCS 并发或调整上游和超时预算。 |
| `unauthorized` / `auth` | 前者是本服务访问令牌错误，后者是上游模型鉴权失败。分别由部署者在本机检查对应配置，不要上传凭据。 |
| `busy` / `rate_limit` / `storage` / `unavailable` | 分别检查服务容量、请求速率、数据库就绪状态或上游可用性。 |

若日志显示约 22–24 秒后 `result=protocol` 且 `queue_ms=0`，说明这次失败没有明显排队耗时，`protocol` 指向响应或答案处理失败。仅凭这些字段，不能认定网络连接失败或输出预算耗尽。先部署有细分类别的版本，再用失败时的固定分类、阶段耗时和已知 token 用量判断。不要通过采集完整上游响应、题目、答案或密钥来排障；脱敏诊断也不应读取或上传真实 `.env`。

对于 `model=qwen3.8-flash`、`protocol=response`、`max_tokens=1024` 的组合，输出预算是应优先核实的可能原因。[阿里云 Responses 文档](https://www.alibabacloud.com/help/zh/model-studio/qwen-api-via-openai-responses) 说明 Qwen3.8 默认 `reasoning.effort=xhigh`，`max_output_tokens` 同时包含推理和最终答案，达到上限时响应为 `incomplete`。这解释了为什么简单题可能成功、部分题却无法生成完整 JSON，但实际网关是否沿用该默认值、这次失败是否达到上限仍需分类证据。官方建议使用 `reasoning.effort` 调节推理；本服务的 JSON 配置名是 `reasoning_effort`，不使用已弃用的 `enable_thinking`。

### 调整配置与部署

1. 先确认使用包含 `reasoning_effort` 和 `/ocs/query` 的新二进制或镜像。旧版本使用严格 JSON 配置解析，添加 `reasoning_effort` 会因未知字段而启动失败；旧版本也没有新的 OCS 路由。若暂不能升级，可先仅将已有的 `max_tokens` 调到 4096 观察，但这不能保证消除失败，也不会改善旧 OCS 的错误显示。
2. 参考 [examples/qwen-responses.json](../examples/qwen-responses.json)，由部署者在本机编辑已有 `config.json`：使用 `response` / `responses`，按模型支持范围设置 `"reasoning_effort": "low"`、`"max_tokens": 4096`、`"timeout_seconds": 60`。`reasoning_effort` 可选值为 `none`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max`，服务只校验这些通用值，具体模型可能只支持其中一部分。省略该字段不发送推理设置，保留上游默认行为。显式值参与 AI 缓存命名空间，改变它会重新生成未被人工题库命中的答案。
3. 示例中的 `https://api.example.com/v1` 只是占位地址，不能直接使用。根据服务商或网关文档填写支持 Responses 的 `base_url`，服务默认在其路径后追加 `/responses`；如果提供的是完整资源路径，用 `endpoint_path` 覆盖。不要照搬 Chat Completions 的根地址，也不要把 API 密钥写入 JSON。Compose 部署保留已有的 `listen=0.0.0.0:5000`、`database=/data/question_bank.db` 和 `access_token_env=OCS_ACCESS_TOKEN`，只修改所需模型字段，不要整份覆盖成回环监听的本地示例。
4. 较大的输出上限可能增加实际 token 消耗、费用和耗时；降低推理强度也可能影响复杂题质量。4096 和 60 秒只是起点，按服务商限制、错误分类与真实使用效果调整。OCS 请求与反向代理的超时应大于服务查询总预算。
5. 新版本镜像发布后，在部署目录执行下面的命令以拉取镜像并重新加载配置。使用自建镜像时先完成对应构建或发布。随后重新导入 OCS 示例，检查连接探测，并进行少量实际查询，确认不再出现原错误分类。不要仅依据 HEAD 测试通过判断模型已恢复。

```bash
docker compose pull
docker compose up -d --force-recreate bank
```

这些是部署者手动操作说明；无需让诊断脚本加载真实配置、dotenv 或数据库，也无需提供任何密钥。配置文件和密钥文件继续按既有只读挂载方式部署，重建服务会重新加载配置并刷新编辑器可能替换过的单文件挂载。

## 独立维护工具

```bash
go build -o bin/ocs-maintain ./cmd/maintain
bin/ocs-maintain -database /srv/ocs/data/question_bank.db -action stats
```

工具只读取显式数据库路径，不读取服务配置或 dotenv，不调用 AI，不创建缺失的源数据库，也不自动升级 schema。统计和清理要求 schema v2；备份/恢复支持 v1 与 v2，拒绝未知的新版本。默认总预算 30 秒，大库可设置 `-timeout 5m`，上限 1 小时。

历史统计包含物理 `answers` 行数（含保留的旧审核记录）、人工答案数、调用数、结果分类、已知 token 和未知用量调用数。它依赖成功写入且尚未清理的调用记录，不能替代服务商账单。

## 清理缓存和调用记录

先预览，再按相同条件执行一批：

```bash
bin/ocs-maintain -database /srv/ocs/data/question_bank.db -action prune -model your-model -retain-call-days 90
bin/ocs-maintain -database /srv/ocs/data/question_bank.db -action prune -model your-model -retain-call-days 90 -batch-size 1000 -apply
```

`-model` 可省略；提供时对缓存和调用记录都精确匹配。缓存仅清理已过期或已失效的 AI 记录；`reviewed_answers` 和保留在旧表中的 `legacy-reviewed` 不删除。调用记录默认全部保留，只有指定大于 0 的 `-retain-call-days` 才清理旧记录。

默认每次最多删除每张表 1000 行，可设为 1–10000；一次调用只处理一批，返回匹配数和实际删除数，有剩余时由操作者再次执行。清理不会立即缩小 SQLite 主文件；需要缩小体积时可备份为新的紧凑快照，再停服切换。历史清理会减少统计所涵盖的时间范围，执行前可先备份。

## 一致性备份与恢复

```bash
bin/ocs-maintain -database /srv/ocs/data/question_bank.db -action backup -output /srv/ocs/backups/bank-2026-10-03.db
bin/ocs-maintain -database /srv/ocs/backups/bank-2026-10-03.db -action restore -output /srv/ocs/data/restored.db
```

输出目录必须预先存在。备份使用 `VACUUM INTO`，包含已提交到 WAL 的数据，可在服务运行时创建一致快照；不应直接复制活跃数据库主文件。快照以 0600 权限写入目标目录的临时文件，同步后以硬链接方式发布，因此文件系统需支持硬链接；已有目标（包括空文件和符号链接）一律拒绝覆盖。需要为快照准备足够磁盘空间。参见 [SQLite 在线备份](https://sqlite.org/backup.html)。

恢复先验证 schema 版本及完整性，再生成新的数据库文件。它不会覆盖或替换服务当前数据库。确认恢复内容后停止服务，将数据库配置切换到新文件，保持目录可写，再启动服务。回退旧二进制必须选择升级前的 v1 备份。不要在服务运行时替换数据库或混用不同快照的 WAL/SHM 文件。

容器镜像包含 `/maintain`。可在部署主机上直接使用独立二进制；也可把数据目录挂到一次性容器后以 `--entrypoint /maintain` 执行，镜像用户 UID/GID 65532 需能读取数据库并写入输出目录。维护进程不需要挂载服务配置或 `.env`。
