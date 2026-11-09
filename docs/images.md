# OCS 图片题接入

服务支持 OCS 的 POST 图片题字段，以及 Chat Completions、Messages、Responses 三种协议的原生图片输入。文字题保留旧 GET 接口和原有纯文本上游请求格式，不需要更换文字模型。

图片题必须使用支持视觉输入的模型和网关。接口名称兼容或文字题能成功，不代表网关会转发图片，也不代表模型能看图。

## 配置 OCS

1. 先部署包含 POST 图片接口的新版服务。旧服务只有 GET，直接更换题库配置会返回 405。
2. 使用 OCS 5.4.0 或兼容更新版本，在“通用 → 全局设置 → 高级设置”开启“图片题优化”。
3. 将 [examples/ocs.json](../examples/ocs.json) 的整个数组重新导入题库配置，修改 URL 为浏览器可达的 `/ocs/query` 地址。
4. 按脚本管理器提示允许题库及图片相关域名。配置使用 `GM_xmlhttpRequest`，不需要关闭浏览器安全检查。
5. 若配置了服务访问令牌，在已有 `headers` 中加入 `Authorization: Bearer …`，保留 `Content-Type: application/json`。这里不能填写上游 AI 密钥。

新版配置的 `data` 为：

```json
{
  "title": "${title}",
  "options": "${options}",
  "type": "${type}",
  "images": {
    "handler": "return (env) => Array.isArray(env.images) ? env.images : []"
  },
  "suggestion_title": "${suggestion_title}",
  "suggestion_options": "${suggestion_options}"
}
```

`method` 必须为 `post`。`images` 必须通过字段解析器返回数组，不要使用 `"${images}"`，也不要把 Base64 拼入文字题干或 URL。

OCS 5.4.0 检查 POST 配置是否实际引用了图片字段；只添加 `v: 2` 或只改请求方法不能证明完整支持。该检查是配置提醒，不会验证服务或模型的视觉能力。参考[开发文档](https://docs.ocsjs.com/docs/other/api)和[5.4.0 图片环境源码](https://github.com/ocsjs/ocsjs/blob/1e93399a22b54fac80bb6bae131a4b208b5d8af9/packages/scripts/src/utils/answerer-env.ts)。

## 请求字段与限制

`POST /ocs/query` 与 `POST /api/query` 接收 JSON 对象，不接受与 URL 查询参数混用。未知字段、重复字段、错误类型和尾随 JSON 均被拒绝；可选 `v` 仅兼容配置元数据，不参与题目查询。

| 字段 | 内容 |
| --- | --- |
| `title` | 原始题干，必填；最大 16 KiB |
| `options` | 原始选项，多项用换行分隔；最大 32 KiB |
| `type` | 题型，兼容原 GET 接口 |
| `images` | PNG/JPEG/GIF/WebP 的 Base64 Data URL 数组；无图时为空数组或省略 |
| `suggestion_title` | 原题图片地址替换为 `[图片N]` 的文本；最大 16 KiB |
| `suggestion_options` | 原选项图片地址替换为 `[图片N]` 的文本；最大 32 KiB |

图片编号从 1 开始，与数组顺序对应。题干和选项共用编号；相同内容的图片也不会在服务端直接去重，以免破坏编号。引用不存在的图片被拒绝。

- 最多 6 张图片。
- 单张解码后最多 3 MiB，全部图片合计最多 12 MiB。
- 单张最多 2000 万像素，每条边最多 8192 像素；尺寸及实际格式从图片头读取。
- 整个 POST 请求体最多 17 MiB，包含 Base64 编码、原文和 JSON 开销。
- 读取请求体最多等待 10 秒，且不超过查询总超时预算。
- 不下载 HTTP、内网、文件或其他客户端提供的图片 URL。原文中的 URL 只是文字；图片内容必须通过 `images` 传入。
- 原始 SVG/BMP 等格式不作为上游图片输入；需先由浏览器或调用者转换为支持格式。

OCS 的 GM 回退可能将 JPEG/WebP 字节标记成 PNG。服务根据实际格式修正 MIME，不会只改后缀或将 JPEG 字节当作 PNG 转发。

有建议文本时 AI 优先使用它，否则使用原文。若原文含内联 Base64 图片源，必须提供对应建议文本，服务用占位文本替代原文后再校验和保存，避免图片字节进入文字 prompt 或 SQLite。建议文本自身不能包含内联图片数据。旧版本 OCS 的缺失建议字段字面量 `undefined` 被当作空值。

## 上游、答案与缓存

图片与文字分别生成协议内容块：

| 协议 | 图片请求形式 |
| --- | --- |
| Chat Completions | 用户 `content` 数组中的 `text`、`image_url`，后者携带 Data URL |
| Messages | 用户 `content` 数组中的 `image`、`text`；`source` 携带真实 MIME 与不带 Data URL 前缀的 Base64 |
| Responses | 用户消息的 `content` 数组中的 `input_text`、`input_image` |

模型仍返回 `{"answer":"…"}`，多选与填空仍使用 `###`。连续标记、无歧义的图片选项会映射回原选项后进行现有校验。无明确选项结构或多行选项不做猜测；答案保留 `[图片N]` 时，OCS 用本题图片顺序还原为原始地址再匹配。直接使用 API 的调用者也需处理这种标记。

图片摘要、顺序、建议文本和图片提示词版本参与缓存及共享请求身份；相同文字但图片不同不会复用 AI 答案。原文字题缓存键不变。图片数组不写入 SQLite 或日志，缓存只保存题目文字、答案及既有元数据。

现有 `reviewed_answers` 没有图片身份，无法判断同样文字对应的图片内容是否相同，因此图片请求不会命中这些旧记录。文字请求保持人工题库优先，旧记录不删除、不覆盖，不需要修改数据库 schema。

## 排障与验证

- **“暂不支持图片、公式”提醒**：重新导入整份新版配置，确认 POST 和图片字段解析器均存在，不要只关闭提示。
- **图片转换失败或数组为空**：先检查 OCS 图片题优化、图片域名授权及图片下载状态；服务端不会自行抓取 URL 补图。
- **`image_invalid`**：检查数组类型、Data URL、格式、尺寸、大小和 `[图片N]` 引用。不要上传题目、完整图片或密钥用于诊断。
- **`request_too_large`**：缩小图片或减少数量；同时检查反向代理请求体限制，代理应允许所需 JSON 大小，但后端上限仍是 17 MiB。
- **`protocol` / `unavailable`**：核对模型与网关是否支持所选协议的原生图片内容块；不能据此认定图片已送达模型。
- **`answer_invalid`**：检查建议选项与原选项的对应关系，以及模型是否返回完整选项内容而非字母。

`/api/query` 对图片参数错误返回 400、过大请求体返回 413；`/ocs/query` 使用 HTTP 200 的安全业务错误，让 OCS handler 显示固定中文提示。失败、截断或无效答案均不缓存。公开 `HEAD /ocs/query` 仅验证连接端点，不验证图片、访问令牌或模型。

自动测试使用合成图片、临时 SQLite 和本机 `httptest` 上游，覆盖三协议图片内容块、MIME 修正、请求限制、答案映射、缓存和并发隔离、错误不缓存及旧文字请求。真实 OCS/模型联调由部署者使用已配置的服务完成；开发和诊断不得读取真实 dotenv。
