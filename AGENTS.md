# AGENTS.md

`tulai`（界面标题「图来」）是本地运行的跨平台 AI 批量生图与图片处理桌面应用，支持 `豆包`、`千问`、`千问国际` 与 `Gemini` 多账号并发调度。本文档为编码代理提供仓库约定、已验证命令、架构边界与强约束；用户向说明见 [README.md](README.md)、[PRODUCT.md](PRODUCT.md) 与 [docs/USAGE.md](docs/USAGE.md)。

## 技术栈与运行环境

- **后端与桌面壳**：Go `1.27.4`（`CGO_ENABLED=0`，见 [go.mod](go.mod)）+ [MyGo](https://mygo.run) `v0.10.0` WebView 框架（`go tool mygo`，配置在 [mygo.json](mygo.json)）+ 纯 Go SQLite（`modernc.org/sqlite`）。
- **前端**：React 19 + Vite 6 + Tailwind CSS v4 + TypeScript 5（strict）+ Zustand + Radix UI + Vitest，包管理与脚本运行使用 `Bun`（见 [frontend/package.json](frontend/package.json)）。
- **受管浏览器**：项目管理的 `Fingerprint`（基于 `adryfish/fingerprint-chromium` + 内置 Node/Playwright 私有 CDP 管道与隔离世界页面桥，同时承载网页账号与下载专用环境）。
- **本地数据目录**：Windows 为 `%APPDATA%\tulai\`，macOS 为 `~/Library/Application Support/tulai/`，Linux 普通二进制为当前工作目录。Windows / macOS 的源码开发版与安装版共用应用数据目录，切勿在两个进程中同时打开同一账号资料目录。

## 开发、测试与构建命令

CI 完整流水线定义在 [.github/workflows/ci.yml](.github/workflows/ci.yml)。

### 安装依赖与本地开发

```bash
# 安装前端依赖（CI 使用 --frozen-lockfile）
bun install --cwd frontend

# macOS / Linux 启动热重载开发模式（CGO_ENABLED=0 + go tool mygo dev）
./dev.sh
```

Windows 命令提示符：

```bat
bun install --cwd frontend
set CGO_ENABLED=0
go tool mygo dev
```

### 常用验证命令

图来通过 `go.mod` 直接依赖 RealBrowser 维护的公开指纹核心模块 `github.com/xxww0098/realbrowser/fingerprintkernel`（升级依赖运行 `go get github.com/xxww0098/realbrowser/fingerprintkernel`），无需额外检出相邻仓库或配置 `go.work`；本地与相邻源码联调时可按需使用临时 `go work`。浏览器版本、下载、安装、进程托管和指纹数据格式由该模块统一管理。

```bash
# Go 静态检查、构建与短测试（带竞态检测）
go vet ./...
go build ./cmd/...
go test -short -race ./...
go test -race github.com/xxww0098/realbrowser/fingerprintkernel/...

# 重新生成 MyGo TypeScript 绑定并校验无漂移
go tool mygo generate && git diff --exit-code frontend/src/mygo.ts

# 前端类型检查、单元测试与生产构建
bun run --cwd frontend typecheck
bun run --cwd frontend test
bun run --cwd frontend build
```

### 专项测试与打包发布

```bash
# 发布门禁与脚本单测
bash scripts/check-release-ci.test.sh
python3 -m unittest discover -s scripts -p '*_test.py'

# 受管浏览器驱动与指纹策略单测
npm ci --prefix internal/fingerprint/driver --ignore-scripts --omit=dev --no-audit --no-fund
node --test internal/fingerprint/driver/*.test.mjs

# 内置页面脚本与下载助手测试
for f in $(find extension -name '*_test.js' | LC_ALL=C sort); do node "$f"; done

# macOS / Linux 打包（CGO_ENABLED=0 + go tool mygo build）
./build.sh

# 按平台收集构建产物到 dist/ 并校验 SHA256SUMS.txt
scripts/collect-dist.sh windows   # 或 linux / macos
```

Windows x64 打包使用 [build-windows.cmd](build-windows.cmd)（会先执行 `go run ./cmd/tulai-fingerprint-bundle` 准备内置运行环境，再以 `-tags=tulai_fingerprint_bundle` 构建 `windows/amd64,windows/arm64`）。受管浏览器集成测试说明见 [docs/FINGERPRINT.md](docs/FINGERPRINT.md)。

## 浏览器使用约束（强约束）

- 服务商网页账号与网页下载专用环境统一使用项目管理的 `Fingerprint`（`adryfish/fingerprint-chromium`），各自使用独立的账号资料（`BrowserData/Fingerprint/` 与 `BrowserData/DownloadBrowser/`）。登录、验证、网页任务与动态网页下载均使用受管 Fingerprint 实例。
- 只使用 tulai 安装并启动的专用 `fingerprint-chromium`，通过私有 CDP 管道与隔离世界页面桥控制。不得启动、连接、控制或依赖系统浏览器、外部调试端口或用户另行打开的浏览器。测试同样使用项目管理的运行环境和独立测试资料。
- 不得把 `fingerprint-chromium` 等受管浏览器二进制打进应用安装包（[scripts/collect-dist.sh](scripts/collect-dist.sh) 会对此强制校验并报错）。浏览器测试使用 `t.TempDir()` 或 `TULAI_FINGERPRINT_TEST_DATA` 隔离资料，不登录真实服务商。

## 仓库架构与模块边界

### 桌面入口与辅助命令（`cmd/`）

- [cmd/tulai/main.go](cmd/tulai/main.go)：程序入口。当环境变量 `MYGO_GENERATE` 非空时走快路径，仅调用 `ui.RegisterBindingsOnly()` 与 `mygo.App.Run()`，跳过数据库与浏览器初始化以供 `go tool mygo generate` 无副作用提取绑定。
- [cmd/tulai/version.go](cmd/tulai/version.go)：定义 `ProductVersion`，必须与 [mygo.json](mygo.json) 保持一致。
- [cmd/tulai/ui_embed.go](cmd/tulai/ui_embed.go)：嵌入唯一应用图标 [cmd/tulai/assets/Icon.png](cmd/tulai/assets/Icon.png) 并通过 `ui.SetAppIcon` 提供给媒体协议。
- [cmd/tulai/update.go](cmd/tulai/update.go)：`version` / `update` 等 CLI 子命令与 Windows 启动版本修复。
- 辅助工具：`cmd/tulai-fingerprint-bundle`（Windows x64 内置 Node/Playwright 运行环境打包）、`cmd/tulai-fingerprint-smoke`（Fingerprint 安装与冒烟）。

### Go UI 控制层与 IPC（`internal/ui/`）

- **单协程 Controller Actor**（[internal/ui/actor.go](internal/ui/actor.go)、[internal/ui/controller.go](internal/ui/controller.go)）：`App`（别名 `Controller`）通过 `StartActor` / `call` / `callValue` 串行化全部 MyGo IPC 状态读写与 `postUI` 回调，避免在业务文件里散落细粒度锁。**严禁在 `app.call(...)` 回调内部调用会阻塞的原生 OS 对话框**（应先离开 Actor 调用宿主对话框，再切回 Actor 写状态）。
- **宿主隔离与 9 个绑定服务**（[internal/ui/host.go](internal/ui/host.go)）：`DesktopHost` 抽象文件/目录选择、保存、打开/定位路径、剪贴板、确认框、主题与事件发射，使 `App` 与全部服务可 100% 无头单测（见 [cmd/tulai/ui_test.go](cmd/tulai/ui_test.go)）。通过 `mygo.BindAs` 注册 9 个服务：`Core`、`Workbench`、`Browser`、`Prompts`、`Download`、`Dedup`、`Rename`、`ImageEdit`、`Settings`，实现分别位于 [internal/ui/bridge_core.go](internal/ui/bridge_core.go)、[internal/ui/bridge_workbench.go](internal/ui/bridge_workbench.go)、[internal/ui/bridge_browser.go](internal/ui/bridge_browser.go)、[internal/ui/bridge_prompts.go](internal/ui/bridge_prompts.go)、[internal/ui/bridge_download.go](internal/ui/bridge_download.go)、[internal/ui/bridge_dedup.go](internal/ui/bridge_dedup.go)、[internal/ui/bridge_rename.go](internal/ui/bridge_rename.go)、[internal/ui/bridge_imageedit.go](internal/ui/bridge_imageedit.go)、[internal/ui/bridge_settings.go](internal/ui/bridge_settings.go)。后端通过事件 `tulai:topics`（合并去重后的状态域失效通知）和 `tulai:file-drop`（原生 OS 拖入绝对路径）推送给前端。
- **纯 Go 业务逻辑层**：8 个 `*_ops.go`（`browser_ops.go`、`dedup_ops.go`、`download_ops.go`、`prompts_ops.go`、`rename_ops.go`、`update_ops.go`、`wizard_ops.go`、`workbench_ops.go`）及配套状态文件（`workbench.go`、`download_tasks.go`、`imageedit_state.go`、`imageedit_batch.go`、`wizard_policy.go`、`heal.go`、`browser_exit.go`）保持与具体 WebView 实现解耦。
- **本地媒体协议 `tulaimedia`**（[internal/ui/media_handler.go](internal/ui/media_handler.go)）：使用不透明随机令牌映射本机图片（`/file?t=<token>&v=<mtime>&w=<px>`，带 64 MiB LRU 缩略图缓存）、实时浏览器抽屉帧（`/drawer/frame`）、图片编辑原图/预览缓冲（`/edit/orig`、`/edit/preview`）与 `/brand-icon`。**平台 Origin 差异**：Windows WebView2 使用 `http://tulaimedia.localhost`，macOS WKWebView 与 Linux WebKitGTK 使用 `tulaimedia://localhost`（由 `MediaOrigin()` 统一返回并在 `ShellStateDTO.mediaOrigin` 下发）。

### 前端应用（`frontend/src/`）

- **状态与 IPC**：[frontend/src/store/index.ts](frontend/src/store/index.ts) 与 [frontend/src/store/types.ts](frontend/src/store/types.ts) 使用 Zustand 管理切片状态，并通过单调递增序号防并发乱序覆盖；[frontend/src/lib/ipc.ts](frontend/src/lib/ipc.ts) 封装服务调用、`isMyGoRuntime()`、`safeIpcCall`、`fireIpc` 与原生拖放 `useFileDrop`。
- **关键前端工具**：[frontend/src/lib/assets.ts](frontend/src/lib/assets.ts)（`toThumbUrl` / `toFullUrl` / `getMediaOrigin`）、[frontend/src/lib/useSyncedDraft.ts](frontend/src/lib/useSyncedDraft.ts)（中文 IME 与异步 IPC 安全草稿）、[frontend/src/lib/shortcuts.ts](frontend/src/lib/shortcuts.ts)（全局快捷键与优先级 `Escape` 栈）。
- **界面组织**：入口 [frontend/src/App.tsx](frontend/src/App.tsx)，共享 Bento 基础件 [frontend/src/components/primitives.tsx](frontend/src/components/primitives.tsx)，Radix 基础组件库 `frontend/src/components/ui/`，9 个页面位于 `frontend/src/pages/`（`WorkbenchPage.tsx`、`BrowserPage.tsx`、`PromptsPage.tsx`、`DownloadPage.tsx`、`DedupPage.tsx`、`RenamePage.tsx`、`ImageEditPage.tsx`、`SettingsPage.tsx`、`WizardView.tsx`），主题令牌位于 [frontend/src/styles/tokens.css](frontend/src/styles/tokens.css) 与 [frontend/src/styles/global.css](frontend/src/styles/global.css)。

### 核心业务与内置脚本（`internal/`、`extension/`）

- 浏览器与控制：`internal/browser`、`internal/cdp`、`internal/fingerprint`、`internal/fingerprinttest`、`internal/captcha`。
- 策略与任务调度：`internal/strategy`、`internal/tasks`（提交边界与恢复语义见 [docs/CORE_FLOW.md](docs/CORE_FLOW.md)）。
- 存储、工具与更新：`internal/store`、`internal/appdata`、`internal/logging`、`internal/prompts`、`internal/download`、`internal/dedup`、`internal/rename`、`internal/imageedit`、`internal/images`、`internal/update`。
- 内置页面脚本与下载助手：`extension/`（由 `extension/bundle.go` 嵌入桌面端，供受管 Fingerprint 通过私有 CDP 与隔离世界注入：`extension/refuse.js`、`extension/doubao/` 与 `extension/download/`，说明见 [extension/README.md](extension/README.md)）。

## 生成文件与版本同步约束

- **严禁手改 [frontend/src/mygo.ts](frontend/src/mygo.ts)**：该文件由 `go tool mygo generate` 自动生成。修改 Go 服务签名、事件或 DTO 后，必须在仓库根目录运行 `go tool mygo generate` 并提交最新生成结果。
- **Go DTO 契约**：所有 IPC DTO 字段使用 `camelCase` JSON tag（[cmd/tulai/ui_test.go](cmd/tulai/ui_test.go) 中 `TestServicesDTOJSONCamelCaseContract` 强制校验），返回切片须初始化为非 `nil` 空切片（如 `[]T{}`），避免序列化成 `null`。
- **桌面端版本同步**：修改版本号时须同时更新 [mygo.json](mygo.json) 的 `version` 与 [cmd/tulai/version.go](cmd/tulai/version.go) 的 `ProductVersion`；保持 [mygo.json](mygo.json) 中 `identifier` 为 `"dev.tulai.app"`、`slug` 为 `"tulai"`（`TestProductVersionMatchesConfig` 与 [cmd/tulai/installed_version_windows_test.go](cmd/tulai/installed_version_windows_test.go) 依赖该标识修复 Windows 卸载注册表项）。
- **保留历史更新日志**：[CHANGELOG.md](CHANGELOG.md) 为历史记录，不要改写旧版本条目；准备新发布时须新增对应的 `## <version>` 节（发布流水线依赖该节生成 Release Notes，详见 [docs/RELEASING.md](docs/RELEASING.md)）。

## UI 与设计规范

- **遵循设计系统**：严格遵循 [DESIGN.md](DESIGN.md)（**Modular Bento Dispatch Console**）与 [PRODUCT.md](PRODUCT.md)。复用 [frontend/src/styles/tokens.css](frontend/src/styles/tokens.css) 和 [frontend/src/styles/global.css](frontend/src/styles/global.css) 的 Light/Dark 双主题令牌及 [frontend/src/components/primitives.tsx](frontend/src/components/primitives.tsx) 卡片原语；避免单页私自配色、装饰性渐变/毛玻璃、低对比度灰字，以及多余的工作台顶栏、重复品牌条或遮挡操作流的弹窗。
- **数据排版与标题栏**：计数器、配额、进度、耗时、尺寸一律加 `.tnum`（`tabular-nums`）；macOS 隐藏内嵌标题栏（`TitleBarHeight: 38`）内的可交互控件必须保持 `.app-no-drag`，拖拽区使用 `.app-drag-region`。
- **中文输入法安全**：凡与 Go 异步 IPC 同步的输入框/文本域，一律使用 [frontend/src/lib/useSyncedDraft.ts](frontend/src/lib/useSyncedDraft.ts)（处理 `compositionstart` / `compositionend` 与防抖提交），防止拼音组合被回写打断或光标跳动。
- **图片加载规范**：列表、网格、队列与悬停预览统一用 `toThumbUrl(url, width)`（控制单图体积以适配 Windows WebView2 限制）；Lightbox 与图片编辑原图视图用 `toFullUrl(url)`。不得硬编码 `tulaimedia://localhost`。
- **快捷键与 `Escape` 优先级栈**：弹窗、抽屉与页面模式退出统一注册到 [frontend/src/lib/shortcuts.ts](frontend/src/lib/shortcuts.ts) 的 `useEscapeHandler` / `pushEscapeHandler`（优先级：`50` Lightbox > `40` Modal Dialog > `30` BrowserDrawer > `20` LogDrawer > `10` 页面模式）。全局快捷键须在 `e.isComposing` 期间跳过；其中 `⌘/Ctrl+1..8` 与 `⌘/Ctrl+J` 须避开 `isEditableTarget`，`⌘/Ctrl+\` 与 `⌘/Ctrl+,` 允许在输入焦点内生效。
- **脱离 MyGo 运行时兜底**：前端单发不等待的 IPC 调用须通过 `fireIpc` / `safeIpcCall` 或 `isMyGoRuntime()`（[frontend/src/lib/ipc.ts](frontend/src/lib/ipc.ts)）保护，确保独立 Vite 预览与 Vitest 不会因缺少 `mygo-runtime` 宿主抛未捕获异常。

## 提交与完成前检查

- **任务提交边界安全**：修改任务执行、重试或会话恢复逻辑时遵守 [docs/CORE_FLOW.md](docs/CORE_FLOW.md)，发送状态未确认（`E_SEND_UNCERTAIN` / `需核对`）绝不静默自动重发。
- **改动 Go 或 IPC**：必须通过 `go vet ./...`、`go test -short -race ./...`；改动 `internal/ui/bridge_*.go` 或 `internal/ui/host.go` 后必须执行 `go tool mygo generate && git diff --exit-code frontend/src/mygo.ts`。
- **改动前端**：必须通过 `bun run --cwd frontend typecheck` 与 `bun run --cwd frontend test`；涉及样式、资源或打包配置时加跑 `bun run --cwd frontend build`。
- **改动扩展、驱动或发布脚本**：补跑对应专项测试（`find extension -name '*_test.js'` 全量用例、`node --test internal/fingerprint/driver/*.test.mjs`、`bash scripts/check-release-ci.test.sh`、`python3 -m unittest discover -s scripts -p '*_test.py'`）。
