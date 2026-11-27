<div align="center">
  <img src="cmd/tulai/assets/Icon.png" alt="tulai 图来" width="76" height="76" />
  <h1>tulai（图来）</h1>
  <p><strong>本地运行的跨平台 AI 批量生图与图片处理桌面工作台</strong></p>
  <p>支持 <strong>豆包 · 千问 · 千问国际 · Gemini</strong> 多账号池并发调度、受管指纹浏览器隔离、网页图片批量采集与纯本地暗房图像处理</p>
  <p>
    <a href="https://github.com/xxww0098/tulai/releases"><img src="https://img.shields.io/github/v/release/xxww0098/tulai?label=Release" alt="Latest Release" /></a>
    <a href="https://github.com/xxww0098/tulai/actions/workflows/ci.yml"><img src="https://github.com/xxww0098/tulai/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
    <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white" alt="Go" /></a>
    <a href="https://mygo.run"><img src="https://img.shields.io/badge/MyGo-v0.10.0-2563EB" alt="MyGo" /></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-green.svg" alt="MIT License" /></a>
  </p>
  <p>
    <a href="#核心特性">核心特性</a> ·
    <a href="#平台与环境支持">平台支持</a> ·
    <a href="#安装与更新">安装与更新</a> ·
    <a href="#快速开始">快速开始</a> ·
    <a href="#本地数据与隐私">本地数据</a> ·
    <a href="#技术架构与目录">技术架构</a> ·
    <a href="#从源码构建">源码构建</a> ·
    <a href="#文档导航">文档导航</a>
  </p>
</div>

---

**tulai（图来）** 是一款面向电商视觉、内容工作室与设计团队的高密度桌面生产力工具，基于 **Go + [MyGo](https://mygo.run) v0.10.0**（React 19 + Vite 6 + Tailwind CSS v4 + TypeScript 5 + SQLite）构建，完整适配浅色与深色主题。

所有账号配置、独立浏览器指纹资料、提示词模板、工作区会话与导出图片均保存在本机；仅在执行生图、账号登录、网页抓图或检查更新时连接目标站点与软件源。

## 核心特性

### 1. 多平台 AI 生图工作台
- **四大服务商独立工作区**：内置 **豆包**、**千问**（`qianwen.com`）、**千问国际**（`chat.qwen.ai`）与 **Gemini**（`gemini.google.com/app`），各平台的参考素材、提示词草稿、并发上限与队列进度彼此隔离。
- **三种批量生产模式**：
  - **批量洗图**：参考图批量重绘（豆包支持同提示词每 10 张合并发送并自动关联源图保存），内置多款画风预设、逐张断点续洗、本地画质轻检（检测对称留白/黑边与疑似原图直出）、审核拒绝自动对半拆批重试，以及单张或多选放回「二次清洗」与一键撤销。
  - **自由生图**：拆分为独立草稿的「文生图」（带行号多行编辑、一键去重、`.txt` / `.md` 拖入导入）与「图生图」（统一描述 / 逐行循环配对），支持模板套用、结构化参数拼装与全局后缀拼接。
  - **裂变图片**：单张母图 1 变 4 构图/视角/配色裂变（或无参考图纯提示词裂变），完成后以 63 位 pHash 校验结果相似度，自动标记过高相似图片供单项重新生成。
- **原生比例与全分辨率原图**：发图前直接操作网页端原生比例菜单（不向提示词注入尺寸文本，保存时不做二次缩放或裁剪）；自动提取高分辨率原图（豆包 `image_ori_raw`、千问系去缩略图参数、Gemini `=s0` 原图及签名回退），支持本机校准去水印、大图 `100%–400%` 缩放与原图分屏对比，以及任务成功后自动清理网页对话历史。

### 2. 多账号池并发与受管指纹浏览器
- **专用 Fingerprint 隔离环境**：统一托管基于 `adryfish/fingerprint-chromium` 的受管浏览器（共享指纹核心 `realbrowser/fingerprintkernel`，支持内核在线检查与升级），通过私有 CDP 管道与隔离世界无痕页面桥控制，**绝不启动、连接或依赖系统浏览器与外部调试端口**。
- **独立指纹与防泄漏代理**：每个账号拥有独立资料目录与防碰撞硬件/GPU/屏幕指纹配置，支持按账号自定义语言、时区、窗口尺寸与网页深浅外观；支持单账号 HTTP / SOCKS5 代理，启用代理时自动附带 `--webrtc-ip-handling-policy=disable_non_proxied_udp` 阻断非代理 UDP 泄漏。
- **实时额度同步与故障轮换**：自动或手动同步账号套餐、剩余积分、用量百分比与重置时间；遇限流、当日额度用尽、登录失效或验证码时自动冷却并切换下一可用账号。内置可拖拽悬浮 **可视抽屉（`BrowserDrawer`）**，无需切出应用即可完成扫码登录或人工验证，并内置离线/视觉模型验证码求解后端（`free` / `custom` / `ikunocr`）。
- **提交边界安全**：严格区分「未发送」与「发送状态未确认（`需核对`）」，网络波动或异常中断时绝不静默换号重发，避免重复消耗额度或漏存结果。

### 3. 网页图片批量下载
- **多源采集与表格导入**：支持从网页、图片直链或 **CSV / XLSX 工作簿**（最高 10,000 行）批量抓图，覆盖主流电商商品页、设计社区、图库与图片搜索结果页（内置可搜索「站点说明」），支持抓取深度、文件大小与像素宽高过滤，并按实际图像编码自动修正扩展名。
- **动态页面托管抓取**：内置独立的下载专用 Fingerprint 浏览器与隔离世界采集助手，支持登录态页面一键「下载到 tulai」、Pinterest 多屏滚动、Temu 自动加载，以及公网域名返回 Fake-IP 时的加密 DNS 解析回退。

### 4. 本地批量图片工具箱
全部图像处理均由本地纯 Go 引擎离线完成，无需额外安装 Python、OpenCV 或模型权重：
- **提示词库**：按「生图 / 洗图 / 裂变 / 通用」分类管理提示词模板与全局后缀，一键填入工作台。
- **图片去重**：基于 63 位 DCT 感知哈希（pHash）提供「严格 / 均衡 / 宽松」三档相似分组，自动将每组最高分辨率原图复制至输出目录并生成 `重复报告.csv`，不删改源文件。
- **批量重命名**：提供查找替换（支持 Go 正则）、插入文本/EXIF 拍摄时间与图片宽高、序号重排、逐行粘贴编辑及视觉模型 **AI 命名** 五种模式，具备实时冲突检测、批内安全交换与一键撤销。
- **暗房图片编辑**：三栏暗房界面（可折叠多选胶片栏 + 交互画布 + 参数面板），支持 8 手柄比例锁定裁剪、旋转翻转、60fps 实时亮度/对比度/饱和度/模糊预览、`5%–95%` 可拖拽原图对比帘、参数「配方」复用及 JPG / PNG / WebP / BMP / AVIF 批量导出。

## 平台与环境支持

### AI 服务商能力一览

| 服务商 | 站点入口 | 网页端原生比例 | 原图拉取与去水印 | 账号池能力 |
|---|---|---|---|---|
| **豆包** | `doubao.com` | 自动 + 7 档比例（`1:1` `4:3` `3:4` `16:9` `9:16` `3:2` `2:3`） | 优先提取 `image_ori_raw` + 方图角落水印修复；同词洗图每消息最多 10 张 | 实时状态/额度同步、对话批量与自动清理 |
| **千问** | `qianwen.com` | 原比例 + 7 档比例 | 去缩略图参数原图 + 可选「去结果水印」另存 | 实时状态/额度同步、对话分页与自动清理 |
| **千问国际** | `chat.qwen.ai` | 原比例 + 7 档比例 | 去缩略图参数原图 | 对话分页与自动清理 |
| **Gemini** | `gemini.google.com/app` | 原生 5 档比例（`1:1` `9:16` `3:4` `4:3` `16:9`） | 自动升级 `=s0` 全分辨率原图（签名拒绝时自动回退） | 实时状态/额度同步、对话批量与自动清理 |

### 操作系统与受管浏览器架构

| 系统与架构 | 应用获取方式 | 受管 Fingerprint 内核 | 说明 |
|---|---|---|---|
| **Windows 10 / 11 (x64)** | [GitHub Releases](https://github.com/xxww0098/tulai/releases) 安装器 / 便携版 | 原生支持（`windows/amd64`） | **推荐**；发布包已内置 Node 与 Playwright 启动环境，首次仅需下载浏览器内核 |
| **Windows 11 (ARM64)** | Releases 提供 `arm64` 与 `amd64` 包 | 需运行 `amd64` 版（系统 x64 仿真） | 上游暂未发布 `windows/arm64` 预编译浏览器内核，使用网页账号请安装 `amd64` 版 |
| **macOS 12+ (Apple Silicon)** | 源码构建 `./build.sh`（或历史 DMG） | 原生支持（`darwin/arm64`） | 首次准备自动下载并校验运行环境与内核 |
| **macOS 12+ (Intel)** | 源码构建 `./build.sh` | 暂无预编译内核 | 本地去重、重命名、暗房编辑与普通直链下载可正常使用 |
| **Linux (x64)** | 源码构建 `./build.sh`（或历史包） | 原生支持（`linux/amd64`） | 需系统 WebKitGTK 环境；`linux/arm64` 暂无预编译浏览器内核 |

## 安装与更新

### 下载安装

前往 [GitHub Releases](https://github.com/xxww0098/tulai/releases) 下载最新正式版：

- **Windows 安装版**：`tulai.Setup.<版本>.amd64.exe`（或 `arm64.exe`），安装至当前用户目录，无需管理员权限。
- **Windows 便携版**：`tulai-<版本>-windows-amd64.exe`（或 `arm64.exe`），单文件直接运行。
- **完整性校验**：每个正式版均附带 `SHA256SUMS` 清单。

> **未签名提示**：Windows 可执行程序未附带商业代码签名证书，首次安装或运行时 SmartScreen 可能提示“未知发布者”；macOS 自构建或历史包若遇 Gatekeeper 拦截，可在访达中右键打开或执行 `xattr -dr com.apple.quarantine /Applications/tulai.app`。

### 应用内与命令行更新

支持在「设置 → 关于与更新」或终端执行 `tulai update`（仅检查可运行 `tulai update --check`）升级版本。更新器内置多镜像与 GitHub 直连并发测速、6 路 HTTP Range 分段加速及断流自动切源，强制核对 `SHA256SUMS` 摘要；若当前有生图任务运行，桌面端会等待任务结束后再执行安装。

## 快速开始

1. **选择服务商**：首次启动 tulai，在快速配置向导中选择豆包、千问、千问国际或 Gemini。
2. **准备浏览器与登录**：按向导或「浏览器」页提示一键下载受管 Fingerprint 环境（支持多镜像并发测速与 SHA-256 校验），新建账号并在受管窗口或应用内「可视」抽屉完成登录。
3. **配置任务**：进入「图片生成」，在顶部切换 **批量洗图**、**自由生图** 或 **裂变图片**，在左栏导入参考素材、编写或填入提示词模板并选择原生输出比例。
4. **并发执行与验收**：点击左下角开始按钮（或按 `⌘/Ctrl+Enter`），在右侧实时查看账号车道状态、「小白洗图君」加工工位与四阶段结果画廊。

常用全局快捷键：

| 快捷键 | 功能 | 快捷键 | 功能 |
|---|---|---|---|
| `⌘/Ctrl+1` … `8` | 切换 8 个主功能页面 | `⌘/Ctrl+Enter` | 开始当前生图模式 / 提交下载 |
| `⌘/Ctrl+\` | 折叠或展开左侧导航栏 | `⌘/Ctrl+J` | 打开或关闭悬浮运行日志控制台 |
| `⌘/Ctrl+,` | 打开设置页 | `Esc` | 按优先级逐层关闭大图 / 弹窗 / 抽屉 / 多选 |

更多功能细节、站点采集范围与故障排查请参阅 **[使用指南](docs/USAGE.md)**。

## 本地数据与隐私

配置数据库（`database/config.db`）、受管账号指纹资料（`BrowserData/Fingerprint/`）、下载浏览器资料（`BrowserData/DownloadBrowser/`）、浏览器运行环境（`runtime/fingerprint/`）、按日滚动日志（`logs/`，保留 7 天）及默认输出目录统一存放在本机应用数据目录：

| 操作系统 | 默认数据目录 |
|---|---|
| Windows | `%APPDATA%\tulai\` |
| macOS | `~/Library/Application Support/tulai/` |
| Linux（普通二进制） | 启动时的当前工作目录 |

- **隐私与凭据保护**：软件不包含遥测埋点或云端账号体系；界面日志复制与导出会自动脱敏常见密钥凭据。`BrowserData/` 与代理配置包含登录态等敏感信息，迁移或备份时请妥善保管（旧版目录迁移方法见 [使用指南 · 本地数据与迁移](docs/USAGE.md#本地数据与迁移)）。
- **合规使用**：受管浏览器与验证码辅助仅用于用户本人合法持有的平台账号与授权素材处理，请遵守各 AI 服务商与目标网站的服务条款。

## 技术架构与目录

- **后端与桌面宿主**：Go `1.27`（`CGO_ENABLED=0`）+ [MyGo](https://mygo.run) `v0.10.0` 原生 WebView + 纯 Go SQLite（`modernc.org/sqlite`）。由单协程 Controller Actor（`internal/ui/actor.go`）串行化 9 个强类型绑定服务，配合 `tulai:topics` 合并状态推送与 `tulaimedia` 本地媒体协议（64 MiB LRU 缩略图缓存与实时缓冲帧流）。
- **前端界面**：React 19 + Vite 6 + Tailwind CSS v4 + TypeScript 5（strict）+ Zustand + Radix UI，遵循 **Modular Bento Dispatch Console** 设计系统（见 [DESIGN.md](DESIGN.md)），数值指标全面启用等宽数字排版（`.tnum`）。
- **受管浏览器控制**：通过 stdin/stdout 私有管道驱动受管 Node 子进程与 Playwright Core，经私有 CDP 管道与隔离世界桥（`internal/fingerprint/driver/bridge.mjs`）执行页面操作与内置脚本注入，不在页面主世界暴露自动化特征。

```text
tulai/
├── cmd/
│   ├── tulai/                     # 桌面主程序入口、CLI 子命令 (version/update) 与嵌入图标
│   ├── tulai-fingerprint-bundle/  # Windows x64 内置 Node/Playwright 运行环境打包工具
│   └── tulai-fingerprint-smoke/   # 受管 Fingerprint 自动安装与冒烟验证工具
├── internal/
│   ├── ui/                        # 单协程 Controller Actor、9 个 IPC 服务绑定与 tulaimedia 媒体协议
│   ├── tasks/ & strategy/         # 多账号并发调度引擎、提交边界恢复与四大服务商页面策略
│   ├── browser/ & cdp/            # 账号会话生命周期、代理防泄漏检查与私有 CDP 客户端
│   ├── fingerprint/ & captcha/    # Fingerprint 运行环境安装、驱动桥与验证码求解后端
│   ├── download/                  # 多站点网页解析器、CSV/XLSX 导入与动态页面抓取协调
│   ├── dedup/ rename/ imageedit/  # pHash 感知去重、批量重命名与暗房图像处理引擎
│   └── images/ store/ update/     # 本机去水印、SQLite 存储与多源分段自更新器
├── frontend/                      # React 19 + Tailwind CSS v4 前端工程（Bun 管理）
├── extension/                     # 嵌入二进制的隔离世界页面脚本与下载助手
├── scripts/                       # CI 发布门禁校验、产物收集与数据迁移脚本
└── docs/                          # 使用指南、Fingerprint 技术说明、核心恢复设计与发布文档
```

## 从源码构建

### 环境要求与检出

需要安装 **[Go](https://go.dev/dl/)**（版本以 [`go.mod`](go.mod) 为准）和 **[Bun](https://bun.sh/)**。图来通过 Go Modules 直接依赖 [`go.mod`](go.mod) 锁定的公开模块 `github.com/xxww0098/realbrowser/fingerprintkernel`（无需额外检出 `realbrowser` 仓库；升级版本可运行 `go get github.com/xxww0098/realbrowser/fingerprintkernel`）：

```bash
git clone https://github.com/xxww0098/tulai.git
cd tulai
bun install --cwd frontend
./dev.sh
```

Windows 命令提示符下进入 `tulai` 目录运行：

```bat
bun install --cwd frontend
set CGO_ENABLED=0
go tool mygo dev
```

> **注意**：Windows 与 macOS 的源码开发版与安装版共用同一应用数据目录（含配置库与 `runtime/fingerprint/` 缓存），版本与架构一致时可直接复用运行环境，但**切勿在两个进程中同时打开同一账号的浏览器资料**。

### 常用构建与验证命令

```bash
# Go 静态检查、构建与竞态短测试
go vet ./...
go build ./cmd/...
go test -short -race ./...
go test -race github.com/xxww0098/realbrowser/fingerprintkernel/...

# 重新生成 MyGo TypeScript IPC 绑定并检查无漂移
go tool mygo generate && git diff --exit-code frontend/src/mygo.ts

# 前端类型检查、单元测试与生产构建
bun run --cwd frontend typecheck
bun run --cwd frontend test
bun run --cwd frontend build

# 桌面端打包（macOS / Linux）
./build.sh
```

Windows x64 打包可运行 [`build-windows.cmd`](build-windows.cmd)（自动联网准备并嵌入 x64 Node/Playwright 运行环境，随后构建 `windows/amd64` 与 `windows/arm64`）。浏览器驱动测试与端到端冒烟步骤详见 [Fingerprint 受管浏览器说明](docs/FINGERPRINT.md#验证)。

## 文档导航

- **[使用指南](docs/USAGE.md)**：界面总览、账号与代理配置、生图/洗图/裂变详解、支持下载站点清单、本地图片工具、快捷键与数据迁移。
- **[Fingerprint 受管浏览器说明](docs/FINGERPRINT.md)**：内核安装与镜像加速、指纹自洽策略、隔离世界页面桥、代理防泄漏与集成测试。
- **[核心任务执行与恢复设计](docs/CORE_FLOW.md)**：MyGo Actor IPC 架构、任务提交边界、异常恢复语义与停止规则。
- **[产品定义](PRODUCT.md)** · **[设计系统规范](DESIGN.md)** · **[编码代理指南](AGENTS.md)**
- **[发布与更新机制](docs/RELEASING.md)** · **[版本更新记录](CHANGELOG.md)**

## 许可证

本项目基于 **[MIT License](LICENSE)** 开源。

发行版安装目录附带的 `THIRD_PARTY_NOTICES.txt` 列出了所包含第三方开源组件及其许可证声明。
