# tulai

tulai 是一款本地运行的 AI 批量生图与图片处理桌面应用。它支持豆包和千问，可以根据提示词生成图片，也可以使用参考图批量改图。

项目使用 Go 与 [MyGo](https://mygo.run) v0.10.0（React 19 + Vite 6 + Tailwind CSS v4 + TypeScript 5）构建，支持 Windows、macOS 和 Linux。账号配置、受管浏览器资料和生成结果保存在本机；生图和网页下载需要连接相应服务。

## 功能

- **图片生成工作台**：支持「批量洗图」「自由生图」和「裂变图片」三种模式，提供参考图批量改图、提示词模板与结构化拼装、1 变 4 构图裂变与 pHash 相似度校验、结果大图缩放/原图对比，以及二次清洗与一键撤销。
- **多账号与受管浏览器**：为豆包、千问添加多个独立账号，统一使用项目管理的 Fingerprint（`fingerprint-chromium` 私有 CDP 管道与独立指纹资料）；支持按额度/状态自动调度，并在应用内悬浮抽屉中完成登录或人工验证。
- **提示词库**：按「生图」「洗图」「裂变」分类管理提示词模板与全局后缀，一键填入工作台。
- **网页图片下载**：从网页、图片链接或 CSV / XLSX 表格批量下载，支持托管指纹浏览器动态页面抓取、深度与尺寸过滤及失败重试。
- **本地图片工具**：图片去重（三档 pHash 感知哈希分组）、五模式批量重命名（含 AI 命名与撤销），以及三栏暗房图片编辑（8 手柄交互裁剪、60fps 实时调色预览、可拖拽原图分屏对比、配方与批量导出）。

## 安装

从 [GitHub Releases](https://github.com/xxww0098/tulai/releases) 下载 Windows 10 / 11 安装包或免安装程序。当前发布流水线提供 Windows amd64 和 arm64 版本；macOS 和 Linux 可从源码构建，历史版本也可在 Releases 中查看。

受管 Fingerprint（`fingerprint-chromium`）内核目前支持 Windows x64、macOS Apple Silicon（arm64）和 Linux x64。Windows ARM 设备使用网页服务商账号时，需要运行 x64 版应用，并使用系统的 x64 仿真。

Windows 程序没有代码签名，安装时 SmartScreen 可能提示未知发布者。

## 快速开始

1. 启动 tulai，在设置向导中选择豆包或千问。
2. 在向导的浏览器准备步骤或「浏览器」页按提示下载安装受管 Fingerprint 浏览器环境，添加账号后在 tulai 打开的受管浏览器窗口或内置「可视」抽屉中完成登录。Windows x64 发布包内含 Node 和 Playwright 启动库，首次使用需下载指纹浏览器内核。
3. 打开「图片生成」，在顶部切换「批量洗图」「自由生图」或「裂变图片」，在左侧栏选择服务商并配置提示词与参考图。
4. 点击左下角开始按钮（或按 `⌘/Ctrl+Enter`），在右侧查看账号车道、任务进度和结果。输出目录可以在设置中更改。

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

需要安装 [Go](https://go.dev/dl/)（版本要求以 [go.mod](go.mod) 为准）和 [Bun](https://bun.sh/)。克隆仓库后，先安装前端依赖并在 macOS / Linux 运行：

```bash
git clone https://github.com/xxww0098/tulai.git
cd tulai
bun install --cwd frontend
./dev.sh
```

Windows 命令提示符中，在仓库目录运行：

```bat
bun install --cwd frontend
set CGO_ENABLED=0
go tool mygo dev
```

Windows 和 macOS 的源码版与安装版共用应用数据目录，包括配置库、默认账号资料和 Fingerprint 运行环境。运行环境的版本、架构和依赖一致时可以直接复用；同一账号的浏览器资料不能由两个进程同时打开。

常用开发命令：

```bash
./build.sh                                    # macOS / Linux 打包（CGO_ENABLED=0 go tool mygo build）
go vet ./...                                  # 静态检查 Go 代码
go test -short ./...                          # 运行 Go 短测试
go tool mygo generate                         # 重新生成 frontend/src/mygo.ts IPC 绑定
bun run --cwd frontend typecheck              # 检查前端 TypeScript 类型
bun run --cwd frontend test                   # 运行前端单元测试
bun run --cwd frontend build                  # 构建前端静态资源
```

Windows x64 可运行 [`build-windows.cmd`](build-windows.cmd) 构建两个架构；构建时会联网准备并打包 x64 的 Node/Playwright 启动环境。MyGo 应用配置位于 [`mygo.json`](mygo.json)，浏览器相关测试和安装步骤见 [Fingerprint 受管浏览器说明](docs/FINGERPRINT.md)。

## 文档

- [使用指南](docs/USAGE.md)：页面总览、账号、生成、下载、图片工具、快捷键与数据迁移。
- [Fingerprint 受管浏览器说明](docs/FINGERPRINT.md)：受管浏览器环境、代理和浏览器测试。
- [核心任务执行与恢复设计](docs/CORE_FLOW.md)：MyGo 前后端架构、任务提交、恢复和停止规则。
- [产品说明](PRODUCT.md) · [设计规范](DESIGN.md)
- [发布说明](docs/RELEASING.md) · [更新记录](CHANGELOG.md)

## 许可证

[MIT License](LICENSE)

安装目录中的 `THIRD_PARTY_NOTICES.txt` 列出程序包含的第三方组件及其许可证。
