# tulai

tulai 是一款本地运行的 AI 批量生图与图片处理桌面应用。它支持豆包、千问、ChatGPT 和 Antigravity，可以根据提示词生成图片，也可以使用参考图批量改图。

项目使用 Go 和 [MyGo](https://github.com/egoist/mygo) 构建，支持 Windows、macOS 和 Linux。账号配置和生成结果保存在本机；生图和网页下载需要连接相应服务。

## 功能

- **批量生图**：输入提示词进行文生图，或导入参考图进行图生图；支持提示词模板、任务队列和历史记录。
- **裂变图片**：选择一张母图，按四种构图方向生成图片，并检查结果之间的 pHash 相似度。
- **多账号管理**：为服务商添加多个账号，按可用状态调度任务；需要登录或人工验证时，可在应用内操作账号浏览器。
- **网页图片下载**：从网页、图片链接或 CSV / XLSX 表格批量下载，支持动态页面、下载筛选与失败重试。
- **图片工具**：图片去重、批量重命名，以及格式转换、裁剪、旋转和尺寸调整。

## 安装

从 [GitHub Releases](https://github.com/xxww0098/tulai/releases) 下载 Windows 10 / 11 安装包或免安装程序。当前发布流水线提供 Windows amd64 和 arm64 版本；macOS 和 Linux 可从源码构建，历史版本也可在 Releases 中查看。

Windows ARM 设备使用网页服务商账号时，需要运行 x64 版应用，并使用系统的 x64 仿真：目前 Camoufox 没有 Windows ARM64 内核。

Windows 程序没有代码签名，安装时 SmartScreen 可能提示未知发布者。

## 快速开始

1. 启动 tulai，在设置向导中选择豆包、千问、ChatGPT 或 Antigravity。
2. 豆包、千问和 ChatGPT 按向导安装应用管理的 Camoufox，并在账号浏览器中登录。Windows x64 发布包内含 Node 和启动库；浏览器内核及指纹数据首次使用时仍需联网下载。Antigravity 按向导安装 `agy` 命令行工具，再登录或导入已有账号；网页授权也需要 Camoufox。
3. 打开「图片生成」，选择「自由生图」「批量洗图」或「裂变图片」，填写提示词并按需添加图片。
4. 点击开始按钮，在界面查看任务进度和结果。输出目录可以在设置中更改。

同一平台可以添加多个账号。平台额度、登录状态、网络和网站页面变化都可能影响任务；发送状态未确认时，请先检查原账号网页，再决定是否重试。更多操作和限制见[使用指南](docs/USAGE.md)。

## 本地数据

配置库、账号浏览器资料和默认输出保存在应用数据目录：

| 系统 | 数据目录 |
|---|---|
| Windows | `%APPDATA%\tulai\` |
| macOS | `~/Library/Application Support/tulai/` |
| Linux 普通可执行程序 | 启动时的工作目录 |

账号登录资料和代理设置可能包含敏感信息，请妥善保管备份。旧版账号迁移和数据目录说明见[使用指南](docs/USAGE.md#本地数据与迁移)。

## 从源码运行

需要安装 [Go](https://go.dev/dl/)，版本要求以 [go.mod](go.mod) 为准。克隆仓库后，在 macOS / Linux 运行：

```bash
git clone https://github.com/xxww0098/tulai.git
cd tulai
./dev.sh
```

Windows 命令提示符中，在仓库目录运行：

```bat
set CGO_ENABLED=0
go run ./cmd/tulai
```

Windows 和 macOS 的源码版与安装版共用应用数据目录，包括配置库、默认账号资料和 Camoufox 运行环境。运行环境的版本、架构和依赖一致时可以直接复用；同一账号的浏览器资料不能由两个进程同时打开。

常用开发命令：

```bash
./build.sh           # macOS / Linux 打包
go test -short ./... # 运行短测试
```

Windows x64 可运行 `build-windows.cmd` 构建两个架构；构建时会联网准备并打包 x64 的 Camoufox 启动环境。浏览器相关测试和安装步骤见 [Camoufox 接入说明](docs/CAMOUFOX.md)。

## 文档

- [使用指南](docs/USAGE.md)：账号、生成、下载、图片工具和数据迁移。
- [Camoufox 接入说明](docs/CAMOUFOX.md)：账号环境、代理和浏览器测试。
- [核心任务执行与恢复设计](docs/CORE_FLOW.md)：任务提交、恢复和停止规则。
- [发布说明](docs/RELEASING.md) · [更新记录](CHANGELOG.md)

## 许可证

[MIT License](LICENSE)
