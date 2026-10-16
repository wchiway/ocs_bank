# MyGo 桌面端

桌面端使用 [MyGo](https://github.com/egoist/mygo) v0.5.5 的系统 WebView，直接管理同一进程内的 Go 查询服务。界面提供模型设置、服务启停、当前运行计数，以及 OCS 配置的复制和导出。界面资源通过 `go:embed` 嵌入程序，构建和使用均无需 Bun、Node.js 或单独的前端服务器。

## 首次使用

1. 打开 OCS Bank，在“模型设置”填写 API 根地址、模型、协议和 API Key。
2. 选择“保存并启动”，确认“题库服务”显示正在运行。默认查询地址为 `http://127.0.0.1:5000/ocs/query`。
3. 在“OCS 接入”复制完整配置或保存 JSON 文件，导入本机浏览器中的 OCS。先测试连接，再实际查询一道题；连接探测本身不会验证模型配置。

设置有效时，后续打开应用会自动启动服务。重复打开会唤起已有窗口。关闭窗口或选择“退出应用”会停止服务；它不会继续在后台提供查询。修改模型和端口等设置前，先停止服务。页面中的计数是本次运行的进程指标，不是历史账单。

桌面端仅监听 `127.0.0.1`，供本机 OCS 使用，不提供公网监听或访问令牌设置。需要局域网、服务器或容器部署时，继续使用现有 `cmd/server` 和 [Docker 部署方式](../README.md#数据和部署)。

## 配置和数据位置

默认目录为 `os.UserConfigDir()` 下的 `ocs-bank`，与程序所在位置和启动工作目录无关：

| 系统 | 默认目录 |
| --- | --- |
| Windows | `%APPDATA%\ocs-bank\` |
| Linux | `$XDG_CONFIG_HOME/ocs-bank/`；未设置时为 `~/.config/ocs-bank/` |
| macOS | `~/Library/Application Support/ocs-bank/` |

该目录保存 `config.json`、`.env` 和 `question_bank.db`，SQLite 的 WAL/SHM 文件也在这里。API Key 只写入 `.env` 的 `AI_API_KEY`，不进入 JSON 配置，也不回传到界面。再次保存时密钥框留空会保留原密钥；输入新密钥会替换它。“打开数据目录”可在文件管理器中定位该目录。

需要独立的便携或测试目录时，从命令行传入绝对路径：

```bash
# Linux/macOS，使用已编译的可执行文件
./ocs-bank -data-dir /absolute/path/to/ocs-bank-data

# 开发时使用自建的隔离目录
go run ./cmd/desktop -data-dir /absolute/path/to/ocs-bank-data
```

Windows 对应参数为 `"OCS Bank.exe" -data-dir "C:\path\to\ocs-bank-data"`；在 PowerShell 中用 `& ".\OCS Bank.exe" -data-dir "C:\path\to\ocs-bank-data"`。macOS 的应用包也可直接调用 `"OCS Bank.app/Contents/MacOS/OCS Bank" -data-dir /absolute/path/to/ocs-bank-data`。

桌面端不会自动读取旧命令行服务的配置或导入旧数据库。初次使用请在界面填写设置；保留已有部署及数据。如需迁移数据库，先按[维护文档](maintenance.md)备份、验证，并在服务停止后操作。不要在应用运行时复制或替换活跃 SQLite 主文件。

## 运行要求

| 系统 | 架构 | WebView 运行组件 |
| --- | --- | --- |
| Windows 10/11 | amd64、arm64 | Microsoft Edge WebView2 Runtime；Windows 11 通常已包含 |
| Linux 桌面 | amd64、arm64 | GTK 3 与 WebKitGTK 4.1 |
| macOS 12 或更新 | Intel、Apple Silicon | 系统自带 WKWebView |

Debian/Ubuntu 可安装 `libwebkit2gtk-4.1-0`，Fedora 对应 `webkit2gtk4.1`。这些是桌面运行依赖；Go 编译不需要 CGO 或这些运行库。Linux 需要图形会话，不能将桌面入口当作无界面服务器运行。

## 本地构建

使用 `go.mod` 指定的 Go 版本。MyGo CLI 通过 Go 的项目工具依赖固定到 v0.5.5，无需全局安装：

```bash
# 当前系统的应用包
CGO_ENABLED=0 go tool mygo build

# 指定目标系统与架构
CGO_ENABLED=0 go tool mygo build -platform linux/amd64,linux/arm64
CGO_ENABLED=0 go tool mygo build -platform windows/amd64,windows/arm64
CGO_ENABLED=0 go tool mygo build -platform darwin/universal -skip-notarize
```

以上为 Bash 命令。在 PowerShell 中先执行 `$env:CGO_ENABLED = "0"`，再运行 `go tool mygo build`。构建只打包程序及嵌入的界面、OCS 模板，不包含用户配置、密钥或数据库。

产物位于 `desktop-build/<系统>-<架构>/`：

| 平台 | 主要产物 | 说明 |
| --- | --- | --- |
| Windows | `OCS Bank.exe`、`OCS Bank Setup 0.1.0 <架构>.exe` | 安装程序由 NSIS 生成；在 Windows 构建时缺少 NSIS 会自动下载，其他主机构建安装程序需要自行安装 `makensis` |
| Linux | `ocs-bank`、`.desktop`、`.deb`、`.tar.gz`、`install.sh` | 推荐使用 `.deb` 安装，或在同一输出目录按 `install.sh` 的提示安装到用户目录；直接运行文件需要执行权限 |
| macOS | `OCS Bank.app`、DMG | `darwin/universal` 同时包含 Intel 与 Apple Silicon；签名及 DMG 制作需要 macOS，其他系统交叉构建时会跳过相应步骤 |

`mygo.json` 不配置 `frontendDist`、`bindings` 或前端构建命令：页面已由 Go 入口嵌入，直接使用 `window.mygo.call`，CLI 打包时不会为生成 TypeScript 绑定而启动应用。

当前配置中的 `version: "0.1.0"` 是开发包元数据，尚未接入 Git tag 同步。不要将不同提交的 CI 测试包当作对应正式版本。接入正式桌面发布时，需要明确 tag 与包版本的同步策略，并一起调整现有发布约定。

## CI 构建与分发范围

[Desktop packages workflow](../.github/workflows/desktop.yml) 在相关 PR 修改时运行，也支持 Actions 手动触发。Linux 和 Windows 各构建 amd64、arm64，macOS 构建通用应用；各自使用 Ubuntu、Windows、macOS runner。成功后在对应 Actions 运行页面下载 artifact，保留 14 天。Linux 使用其中的 `.deb` 或 `.tar.gz`，macOS 使用 DMG，以保留安装后所需的文件权限和应用包结构。

这条 workflow 只生成 Actions artifact，不创建版本 tag、不上传 Release 附件、不更新容器，也不调用 AI 总结服务。现有命令行、Docker 和版本发布流程继续使用原入口。

当前 Windows 包未配置代码签名；macOS 在 macOS 主机上仅使用临时签名，跳过公证。这些是供验证的构建包，不能保证在其他机器上打开时没有系统安全提示。面向正式分发时需要另行配置 Windows 签名、Apple Developer ID 与公证。workflow 不使用签名或 API 密钥。

CI 打包成功只能验证构建与包装流程，不能替代三平台真机的首次配置、双击启动、WebView 渲染、OCS 搜题和退出测试。
