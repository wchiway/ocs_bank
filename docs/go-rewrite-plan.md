# Go 重构计划

## 实施进度

- P0：计划已通过 PR #1 合并。
- P1：Go 骨架、接口契约、AGENTS 工作流及 CI 已通过 PR #2 合并。
- P2：三种上游协议及异常处理已通过 PR #3 合并。
- P3：SQLite、缓存、并发协调与集成测试已通过 PR #4 合并。
- P4：迁移工具、部署文件、OCS 示例与进程测试在当前 PR 交付。现场真实凭据/浏览器联调见 docs/validation.md。

## 目标与范围

采用 Go、chi/v5、database/sql、modernc.org/sqlite 和标准库 net/http、encoding/json、log/slog，构建单实例题库服务。保留 OCS GET /api/query 的 title、options、type 参数及响应兼容性。第一版实现同步文本问答，不包含管理 UI、多模态、工具执行、流式输出或微服务。

本计划中的 chat、message、response 指调用上游的三种协议，不是向外暴露三套代理接口。配置接受这三个名称，内部规范名称为 chat_completions、messages、responses。可接入实现相应协议的官方服务或自建兼容网关；不假定任意网关都支持所有参数。

## 仓库与提交方式

新仓库：https://github.com/wchiway/ocs_bank（private）；默认分支 main。旧 Python 版本保留在本地 legacy/python，原远程保留为 upstream。旧历史包含非空 API key，不推送旧分支或历史；新仓库从 GitHub 初始化提交开始。

每阶段使用独立 worktree 和任务分支，PR 指向 main，检查和审查通过后 squash merge。禁止直接向 main 提交实现、push --all、push --mirror。合并后 fetch，基于新的 origin/main 开始下一阶段。分支保护若受账户套餐限制，需明确记录限制，不能以绕过 PR 代替。旧数据库保持在旧分支，迁移时从备份副本读取。

## 模块结构

```text
cmd/server/main.go       # 依赖组装、启动、优雅退出
internal/config/         # 配置加载和验证
internal/httpapi/        # chi 路由、OCS 协议转换、中间件
internal/question/       # 查询流程、缓存键、答案校验、并发协调
internal/provider/       # 公共模型接口和三个独立适配器
internal/store/          # SQLite 数据访问
migrations/              # 版本化 SQL
```

业务层不依赖 chi 或上游协议结构。接口围绕实际替换边界定义：答案存储与答案生成。测试放在同包 *_test.go 文件中。

## 三种 AI 协议

| 配置值 | 默认端点 | 请求与鉴权 | 响应解析 |
|---|---|---|---|
| chat | POST /v1/chat/completions | model、messages；Bearer | choices 中 assistant message.content，检查 finish_reason 和 refusal |
| message | POST /v1/messages | model、messages、顶层 system、max_tokens；x-api-key、anthropic-version | content 中 text 块；检查 stop_reason，不把 thinking/tool_use 当答案 |
| response | POST /v1/responses | model、input、instructions；Bearer | 遍历 output 中 message 的 output_text 内容块；检查 status、error、incomplete_details 和 refusal |

统一配置包含 protocol、base_url、model、api_key_env、请求总超时、最大并发、重试上限和可选 endpoint_path。base_url 定义为 API 根路径（例如 https://host/v1），适配器只追加资源路径，测试覆盖尾斜杠、子路径及重复 /v1。endpoint_path 的覆盖规则必须明确并验证。密钥由环境变量引用，不写入示例配置。

不同协议分别维护请求 DTO、鉴权和错误解析，不通过换 URL 复用同一 payload。token 上限、结构化输出、temperature 等参数按协议和已配置模型能力发送，不能盲目转发所有参数。Responses 使用原始 HTTP JSON 的 output 数组，不依赖 SDK 的 output_text 便捷属性。

统一生成结果携带文本、模型、来源、usage、完成状态；错误使用类型区分认证、限流、超时、协议异常、拒答与上游不可用。仅已完成、通过校验的非空答案可以入库。JSON 解析只允许明确的兼容规则，例如旧 anwser 字段，禁止全局替换引号或正则猜测修复。格式校验不等于事实验证，AI 答案始终保留来源与失效能力。

## 查询、缓存与可靠性

- OCS 参数校验 → 缓存 → 合并相同请求 → 获取并发额度 → 上游调用 → 校验 → 保存 → OCS 响应。
- 缓存键基于稳定序列化的题目、选项、规范题型和缓存策略版本；仅做保守规范化，不改变选项顺序或题目语义。记录 provider、model、提示词版本、生成时间和失效信息。
- 使用唯一约束和幂等写入；不在外部 HTTP 调用期间持有数据库事务。缓存保存失败仍可返回有效答案，同时记录失败。
- context 贯穿服务和 HTTP/SQL；重试共用总预算。认证及参数错误不重试；限流考虑 Retry-After；瞬时错误有限指数退避和抖动。
- 共享调用有独立受限生命周期；单个等待者取消不影响其他等待者，所有等待者退出或服务关闭时取消任务。限制等待队列和模型并发。
- SQLite 使用本地持久磁盘，明确连接池、WAL 和 busy timeout 配置；迁移有版本记录，失败不启动服务。
- 日志仅记录请求 ID、耗时、缓存命中和错误类别；不记录密钥和完整题目。启用 TLS 验证，限制上游响应体大小，防止重定向泄露鉴权。
- 对外开放时提供访问令牌和限流，健康检查不调用模型。

## PR 阶段与验收

### P0：计划和工作流（当前 PR）

提交本计划、贡献流程、忽略规则；确认新仓库 private、main 基线独立于旧历史、开发位于 worktree。

### P1：Go 骨架与接口契约

建立 Go module、chi 路由、配置、日志、健康检查与优雅退出；实现假的 provider 和临时存储供测试。配置缺失必须启动失败，测试明确 OCS 成功、未找到、参数错误、服务错误的 JSON。修正示例 OCS handler 的 res.code/res.data.code 不一致，并添加 CI（构建、格式、vet、test、race）。具体 Go/依赖版本实现时核对支持范围并固定。

### P2：三个 provider 适配器

独立实现 chat、message、response，使用 httptest.Server 验证 URL、请求体、鉴权、正常响应、多内容块、空响应、拒答、截断、错误 JSON、401、429、5xx、超时和取消。三个适配器都可通过配置选择；失败无有效答案返回。没有真实密钥也能运行完整测试。

### P3：存储与完整查询流程

实现迁移、复合缓存键、唯一约束、答案校验、请求合并、并发限制和总超时。验收不同选项不会串答案、并发同题只调用一次、失败不入库、单个等待者取消不影响其他人、缓存写入失败仍返回有效答案。用临时 SQLite 做真实集成测试。

### P4：迁移工具、部署和端到端验证

提供只读旧库的检查/导入工具：先 dry-run，报告重复与无效记录，信息不足的旧数据不得直接进入新缓存。仅操作副本，支持备份和回退。补齐配置示例、运行文档、二进制/容器部署和实际 OCS 兼容性验证。自动验收使用模拟上游；真实凭证验证另行安排，不阻塞离线测试。

## 统一完成标准

各实现 PR 通过 gofmt、go vet ./...、go test ./...、go test -race ./... 和构建检查。PR 说明行为、测试结果及兼容性影响。全部三种协议、缓存边界、错误分类与取消语义必须有自动化断言。正式切换前验证 OCS 并保留回退路径。

## 官方协议参考

- Chat Completions：https://platform.openai.com/docs/api-reference/chat/create
- Responses：https://platform.openai.com/docs/api-reference/responses/create
- Messages：https://github.com/anthropics/anthropic-sdk-python/blob/main/api.md
- chi：https://github.com/go-chi/docs/blob/master/quickstart.md
- SQLite 驱动：https://pkg.go.dev/modernc.org/sqlite
