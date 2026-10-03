# 诊断、统计与数据库维护

## 运行状态

- `GET /healthz`：进程存活，不调用模型。
- `GET /readyz`：检查服务未关闭且数据库能在 1 秒预算内响应；不可用返回 503，不返回内部错误。
- `GET /api/stats`：JSON 进程统计，使用与 `/api/query` 相同的 Bearer 令牌规则；仅在未配置令牌的本机部署中免认证。

`/api/stats` 返回 `scope: "process"` 与 `counters`。计数从本次启动开始，未出现的计数可视为零：

| 字段 | 含义 |
| --- | --- |
| `queries_total` | 所有查询请求，包括认证和参数拒绝 |
| `query_result_*` | 查询结果分类，例如 ok、invalid、unauthorized、timeout、storage |
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
