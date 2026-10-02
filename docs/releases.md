# 自动发布与 DeepSeek 版本总结

## 两个 workflow

- `.github/workflows/release.yml`：推送 `v1.2.3` 等正式版本 tag 时创建 Release。
- `.github/workflows/pre-release.yml`：推送 `v1.2.3-rc.1`、`v1.2.3-beta.1`、`v1.2.3-alpha.1` 等 SemVer 预发布 tag 时创建 Pre-release。

版本号以 tag 为准，无需手工维护重复的 VERSION 文件。tag 必须指向已合入 `main` 的提交；workflow 再次严格校验版本格式与发布通道。禁止移动已经发布的 tag，修复请递增版本。普通代码提交不会创建 Release。

每个 workflow 的步骤：校验 tag → 测试发布脚本 → 创建 Release → 调用 DeepSeek Responses API → 追加中文版本总结。两个 workflow 共用 composite action 和脚本，发布身份仍分别为 Release 与 Pre-release。预发布不会标记为 Latest。

## GitHub 配置

在仓库 Settings → Secrets and variables → Actions 设置：

| 类型 | 名称 | 内容 |
|---|---|---|
| Secret | `DEEPSEEK_API_KEY` | DeepSeek API 密钥，由仓库管理员设置 |
| Variable，可选 | `DEEPSEEK_MODEL` | 默认 `deepseek-v4-flash`，必须支持 Responses API |

使用官方端点 `https://api.deepseek.com/responses`，Bearer 鉴权，非流式输出。不从仓库 `.env` 读取密钥；无需向代理提供密钥。`GITHUB_TOKEN` 由 Actions 自动提供，发布 job 具有 `contents: write` 权限。

模型输入仅包含相邻版本间的提交标题、短 SHA 和版本信息，不上传源码、diff、配置、数据库、环境文件或思考链。仓库是私密的，这些变更记录会发送至已指定的 DeepSeek 服务进行总结。

## 发布步骤

先通过任务分支 PR 合并版本所需代码，然后在最新 `main` 上打 tag：

```bash
git switch main
git pull --ff-only origin main
git tag -a v0.1.0-rc.1 -m 'v0.1.0-rc.1'
git push origin v0.1.0-rc.1
```

正式发布使用新的正式 tag：

```bash
git tag -a v0.1.0 -m 'v0.1.0'
git push origin v0.1.0
```

不要使用 `git push --tags`、`--all` 或 `--mirror`。尤其不能上传包含历史凭据的 `legacy/python` 分支。

## 发布内容

仅使用 GitHub 为 tag 自动提供的 **Source code (zip)** 和 **Source code (tar.gz)** 下载入口。不执行 Go 编译、交叉编译、Docker 构建，不上传二进制或手工归档，也不增加版本注入代码。

源码包内容来自对应 tag 的 Git 树，不包含未提交的 `.env`、运行时配置或数据库。版本号直接使用 tag。常规 PR CI 仍按原有规则检查代码，与发布 workflow 相互独立。

## 变更范围与重跑

正式版比较上一个可达的正式 tag，包含此前预发布阶段的全部改动；预发布比较上一个可达且版本更低的 tag。首次发布使用当前历史。AI 总结之外始终保留原始提交记录与比较链接。

总结使用 120 秒总预算，429/5xx 最多重试两次。响应截断、格式错误、缺少密钥或 API 调用失败时，workflow 标记失败，但已经创建的 Release 和 GitHub 源码下载保留，版本总结显示待生成。配置正确后在 Actions 重跑，或通过对应 workflow 的 Run workflow 输入已有 tag。

重复执行复用同一 Release，并只替换带标记的 AI 总结区域；人工添加的其他说明保留。发现已有 Release 的提交标记或发布类型不一致时会拒绝覆盖。重新生成总结失败时保留之前成功的总结。

本次只交付自动化流程，不创建真实版本 tag，也不发布测试 Release。配置 Secret 后的首次真实发布才会调用 DeepSeek。

参考：[DeepSeek Responses API](https://api-docs.deepseek.com/guides/responses_api)、[GitHub tag 触发规则](https://docs.github.com/actions/reference/workflow-syntax-for-github-actions)。
