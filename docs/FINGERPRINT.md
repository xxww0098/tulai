# Fingerprint 受管浏览器环境

tulai 的服务商网页账号与网页下载专用环境统一使用项目管理的 **Fingerprint**（基于 `adryfish/fingerprint-chromium`，锁定版本 `148.0.7778.215`）。Go 桌面程序与受管 Node 子进程通过 stdin/stdout 私有管道通信，Node 通过 Playwright Core 与私有 CDP 管道控制账号浏览器，不连接系统浏览器或外部调试端口。

## 平台支持与架构边界

- **支持平台**：Windows x64（`windows/amd64`）、macOS Apple Silicon（`darwin/arm64`）与 Linux x64（`linux/amd64`）。
- **未提供预编译内核的平台**：上游未发布 macOS Intel（`darwin/amd64`）、Windows ARM64（`windows/arm64`）与 Linux ARM64（`linux/arm64`）预编译包；Windows ARM64 设备需通过 x64 仿真运行 x64 版应用。
- **版本锁定**：当前固定在 `148.0.7778.215`（所有官方归档均写入固定 SHA-256 强制校验）；更高版本 `150.0.7871.186` 缺失 macOS 构建并存在 Canvas 读回崩溃问题，暂不升级。

## 安装、镜像与内置运行环境

- `internal/fingerprint/driver/package-lock.json` 仅锁定 `playwright-core 1.62.0`（无外部第三方指纹包或原生二进制依赖）。
- Windows x64 的安装包与免安装程序通过 `-tags=tulai_fingerprint_bundle` 内置 Node `v24.22.2` 与 `playwright-core`；首次准备时直接解压并校验清单和依赖锁摘要，不再联网下载 Node 或执行 npm。浏览器内核仍在首次安装时单独联网下载（`scripts/collect-dist.sh` 强制禁止将浏览器二进制打进安装包）。
- 其他平台及未开启打包标签的开发构建按当前系统与架构下载 Node 并校验 SHA-256。官方源失败、长时间无字节或低速时自动切换 `npmmirror`（仍强制校验固定 SHA-256）；npm 依赖在超时或失败后改用 `npmmirror` registry（仍按 lockfile `integrity` 校验）。
- 内核下载自动在多个 GitHub 加速镜像与直连源之间测速选优（支持 HTTP Range 的源采用多路并发分段下载与容错续传），并始终校验固定 SHA-256：镜像仅用于加速、无法篡改。
  - `TULAI_FINGERPRINT_MIRROR`：指定自建镜像前缀（ghproxy 形式：base + `/` + 原 URL），设为 `off` 关闭镜像。
  - `TULAI_FINGERPRINT_ARCHIVE`：测试时指定本机已校验归档路径，跳过联网下载。
- 多个应用进程同时安装运行环境时使用跨进程锁，等待前一项安装完成并复用结果。CI 按 `install.go`、`fingerprint.go` 与 `package-lock.json` 缓存 `runtime/fingerprint/` 目录。

## 目录结构、指纹连续性与隔离世界桥

- **运行环境目录**：`runtime/fingerprint/<version>-<playwright>-<node>-<os>-<arch>/`
- **账号资料目录**：`BrowserData/Fingerprint/account-*`
- **下载专用环境**：`BrowserData/DownloadBrowser/`
- **指纹持久化与冲突检查**：
  - 每个账号（及 `DownloadBrowser`）在自身资料目录持久化 `.tulai-fingerprint.json`，保存唯一的全 31 位（含奇偶数）`--fingerprint` 种子、宿主平台、真实操作系统版本 `--fingerprint-platform-version`、`Chrome` 品牌、合理的 `hardwareConcurrency` 与动态随机化的桌面屏幕/窗口尺寸及无头窗口位置。
  - 启动参数不带 `--disable-blink-features=AutomationControlled`：内核的 `009-webdriver` 补丁已经去掉了唯一读取该特性的代码（`Navigator::webdriver()`），页面里的 `navigator.webdriver` 恒为 `false`；而这个开关会让每个有头窗口的首个标签页显示「不受支持的命令行标记」横幅，并挤掉该标签页 52px 的视口高度（`outerHeight - innerHeight` 从 121 变成 173）。账号窗口与下载专用环境驱动的正是这个标签页。同时忽略 Playwright 默认的 `--enable-automation`、`--hide-scrollbars` 与 `--force-color-profile=srgb`（sRGB 配置会把广色域屏幕压平，真实窗口不会如此）；无头模式下自动附带 `--screen-info` 与 `--window-position`，避免暴露默认 `800×600` 虚拟屏幕与固定窗口坐标，显示器参数取自同目录下最近一次有头会话的记录（见下文「指纹自洽与反检测」）。
  - 新账号（及 `DownloadBrowser`）生成身份时通过跨进程生成锁与 `collisionRoots` 双向扫描 `BrowserData/Fingerprint` 与 `DownloadBrowser`，严格避免并发竞态、种子与视口碰撞，并均衡分散内核派生的 GPU 槽位（`seed % gpuMod`）、内存档位（`seed % 3`）、补丁版本（`seed % 4`）、与 GPU 匹配的 CPU 核数/系统版本（含 macOS Apple M4/M2 硬件版本一致性、内存档位与核数联动，以及 Windows 10/11 任务栏高度一致性）及屏幕预设；`.tulai-fingerprint.json` 损坏时直接报错，不静默换号，并向后兼容旧版 `.tulai-fingerprint.json`。
- **私有 CDP 隔离世界页面桥与无痕执行**：
  - 上游 `001-disable-runtime.enable.patch` 为避免 `Runtime.enable` 检测而禁用了常规 `Runtime.addBinding` / `Runtime.consoleAPICalled`，导致标准 Playwright `exposeBinding` 在导航后失效。
  - `internal/fingerprint/driver/bridge.mjs` 通过独立隔离世界（`Page.addScriptToEvaluateOnNewDocument` + `Runtime.executionContextCreated` 按需绑定）与 `scopeBootstrap`（文档启动前捕获原生 `EventTarget.prototype.dispatchEvent`、`CustomEvent` 及核心内建原语，免疫原型篡改与 `Object.prototype` 陷阱）转发 `tulaiBridge`；主框架运行句柄由隔离世界触发移交并经私有 CDP `Runtime.callFunctionOn` 执行（不启用 `CommandLineAPI` / `getEventListeners`），子框架与跨域 OOPIF 探测走真实 `Page.createIsolatedWorld` 隔离世界，避免在调用栈暴露 Playwright `UtilityScript` 或触发页面钩子监控的 `dispatchEvent`/`CustomEvent`，在跨源导航与严格 CSP 下稳定工作，同时不在页面主世界 `window`（含 Symbol）或 DOM 暴露 `tulai` 标识。
  - 主框架求值不在页面钩子可见的调用栈里留下工具帧：表达式在该文档的生成器作用域内（保留跨次求值的顶层 `var`）即时编译成具名函数，脚本地址取页面真实加载的同源脚本、否则取文档地址，并在新的微任务中执行。站点钩住 `JSON.stringify`、`Object.keys`、`createElement` 等原生方法时，看到的调用帧全部落在站点自己的 URL 下，没有 `Generator.next`、`callFunctionOn` 或匿名注入脚本的帧。**因此传给 `Session.Evaluate` 的脚本应写成单个表达式（必要时用 IIFE 包裹）**；多语句脚本仍走同步生成器路径，会暴露工具帧。
- **网页下载专用环境与隔离世界下载助手**：
  - `DownloadBrowser` 使用同一 Fingerprint 内核并独立保存登录状态和指纹。
  - `bridge.mjs` 在独立隔离世界注入 `extension/download/` 中的 `settings.js`、`temu.js`、`content.js`，通过私有 CDP 绑定把「下载到 tulai」与 Temu 自动加载状态回传给 Node/Go，并在浏览器标签页导航登录期间保持当前标签页已采集记录。
  - 下载专用环境的网页访问和图片请求使用直连，开启全局或系统代理时也可启动。

## 指纹自洽与反检测

目标是每个账号「像一台真实机器」且账号之间互不相同：单账号内部各处取值能互相对得上，账号之间靠内核按种子派生的渲染指纹区分。

- **内核噪声层只保留能带来区分度的部分**：启动参数固定 `--disable-spoofing=audio,clientrects`。
  - `audio` 层会让 `OfflineAudioContext` 报告的 `sampleRate` 不等于创建时传入的值，`clientrects` 层会把 `getBoundingClientRect` / `getClientRects` 返回的矩形整体平移最多 0.001px（`Element` 与 `Range` 都是），与 CSS 位置、`offsetLeft` 等对不上。两者都是页面可以当场证伪的谎言，而带来的账号区分度可以忽略，因此关闭。
  - `canvas` 层**保留**：它按种子稳定（同一账号多次读取、跨会话一致），是同机多账号在 canvas / WebGL 渲染哈希上互不相同的唯一来源。关闭它会让所有账号渲染出完全相同的 canvas 与 WebGL 图像，等于主动把账号关联到同一台设备。
- **已知缺陷（所固定的内核版本）**：`canvas` 层同时把 `measureText` 的各项度量乘以一个约 `±5e-6` 的种子常量（上游补丁本意是 `1 + 噪声`），导致页面里 `measureText(...).width` 恒约为 0。上游 `150.0.7871.186` 的补丁源码尚未公开，无法确认已修复，且该版本暂不可升级（见上文版本锁定）。页面脚本与策略代码不要依赖 `measureText` 的数值；需要真实文字宽度时用 DOM 测量（`getBoundingClientRect` / `offsetWidth`）。
- **显示器一致**：内核不改写 `screen.*`，有头窗口看到的始终是宿主真实显示器，而无头窗口由启动参数描述。于是每次有头启动都会把页面看到的显示器（宽高、可用区域、`devicePixelRatio`、色深、是否广色域、是否 HDR、是否多屏）写入账号资料目录旁的 `.tulai-host-display.json`，同目录账号共用；随后的无头启动用 `--screen-info`（物理像素）复刻它，窗口尺寸与位置按其工作区收敛。色域与 HDR 也能对上：HDR 主机附带 `--force-color-profile=hdr10`（原生报告 P3 与 HDR，截图取色不变）；广色域但非 HDR 的主机没有既对齐查询又不让截图偏色的开关（`display-p3-d65` 会把纯红截成 `234,51,35`），因此改为在每个页面用 `Emulation.setEmulatedMedia` 模拟 `color-gamut: p3`——只影响 `matchMedia` 与 CSS 媒体查询，实际渲染和截图不变。同一账号在有头与无头之间切换时，`screen`、`devicePixelRatio`、`colorDepth`、`color-gamut`、`dynamic-range` 保持一致；没有记录时退回账号自己的屏幕预设。
- **macOS 硬件取值联动**：内核按种子奇偶选择 Apple M4 / M2、按 `seed % 3` 给出 `deviceMemory`（8 / 16 / 32）。新账号只在真实存在的机型组合里取值：M4 为 10 / 14 / 16 核、系统 15.x、内存不取 8 GiB；M2 的 8 GiB 机型为 8 核、32 GiB 机型为 10 / 12 核。
- **Playwright 默认行为**：Service Worker 保持开启（Playwright 的 `block` 会把 `navigator.serviceWorker.register` 替换成一个源码里写着 "blocked by Playwright" 的函数）；`Element.prototype.fakeShadowRoot`（内核为方便自动化而暴露的属性，原生 Chrome 没有）由文档启动脚本删除。
- **导航注入脚本与页面绑定的调用帧**：注册时的页面地址会作为这些文档脚本的 `sourceURL`，其调用帧显示为该页面的内联代码；在 `about:blank` 上注册的脚本不带地址。
- **仍然存在的差异（有意保留或尚无低风险方案）**：
  - 页面求值带 `userGesture`，`navigator.userActivation` 在任务期间保持激活（上传入口依赖它打开文件选择器）。
  - 文本输入为一次 `insertText`，点击轨迹为 2–3 步快速逼近——这是为消除数秒级输入延迟而做的速度取舍。
  - 广色域非 HDR 主机上，站点自己弹出的窗口在其第一个文档里可能仍读到 `color-gamut: srgb`（CDP 会话在该文档开始运行后才附加）。
  - 求值脚本里的匿名函数在调用栈里显示为 `eval` 帧（地址仍是页面 URL）。
  - 站点 Service Worker 自己发起的请求不经过页面级 `Fetch.enable` 拦截（文档导航请求仍然受控）。

## 代理隔离与 WebRTC 防泄漏

- 账号独立代理优先于全局代理；修改前须先关闭该账号。HTTP 代理支持认证，SOCKS5 认证受 Playwright 限制而不支持。
- 配置代理且未启用直连模式时，`fingerprint-identity.mjs` / `bridge.mjs` 自动为 `fingerprint-chromium` 附带 `--webrtc-ip-handling-policy=disable_non_proxied_udp`，禁用非代理 UDP 以防止 WebRTC 绕过代理泄露本机或真实公网 IP（请勿在有头窗口中通过 `chrome://flags/#webrtc-ip-handling-policy` 手动改写，以免 `Local State` 覆盖命令行参数）。直连模式或未配置代理时不附带该参数。
- `Manager.start` 启动账号前通过 `SamePlatformSharedExit` 与 `NormalizeProxyKey`（对认证信息做 SHA-256 脱敏摘要）检查同一平台其他已启用账号是否共享同一出口或直连，并在日志中记录告警（不阻断单出口用法）。
- 实际代理出口、DNS / IPv6 / WebRTC 泄漏仍需在目标网络实测验收，单元测试不替代网络泄漏测试。

## 进程与资料管理

- 账号改名不移动资料目录；删除账号只清理当前目录，并拒绝越界路径或指向数据目录外的符号链接。
- 账号浏览器的生命周期以进程本身为准：用户手动关闭窗口后 `Manager` 摘掉记录并通知界面更新状态；生图/洗图任务结束后按批次清理无头账号进程并保存登录资料。

## 验证

短测试与指纹策略单测（无需下载内核）：

```sh
npm ci --prefix internal/fingerprint/driver --ignore-scripts --omit=dev --no-audit --no-fund
node --test internal/fingerprint/driver/*.test.mjs
go test -short -race ./internal/fingerprint ./internal/browser ./internal/cdp ./internal/captcha ./internal/store ./internal/strategy ./internal/download ./internal/tasks
```

真实内核冒烟与端到端集成测试（在独立测试数据目录执行，不登录真实服务商）：

```sh
go run ./cmd/tulai-fingerprint-smoke

export TULAI_FINGERPRINT_TEST_DATA="$(mktemp -d)"
TULAI_FINGERPRINT_TEST_INSTALL=1 go test -count=1 -run TestManagedFingerprintLifecycle ./internal/fingerprint
go test -count=1 -run TestFingerprint ./internal/cdp
# 指纹自洽：页面表面互相对得上、调用栈不带工具帧、有头记录的显示器在无头回放且截图不偏色、账号间渲染互不相同、有头窗口首个标签页没有启动横幅
go test -count=1 -run 'TestFingerprintPageSurfaceStaysSelfConsistent|TestFingerprintHeadedDisplayIsReplayedHeadless|TestFingerprintHeadlessReplaysWideGamutDisplaysWithoutRecoloringCaptures|TestFingerprintAccountsRenderDifferentlyAndStably|TestFingerprintHeadedWindowOpensWithoutStartupInfobar' ./internal/cdp
go test -count=1 -run TestManagedFingerprintAccountSessions ./internal/browser
go test -count=1 -run 'TestTemuAssistantAutoLoadKeepsEarlierCards|TestDownloadAssistantBrowserButton' ./internal/download
go test -count=1 -run 'TestManagedFingerprintSubmitsTenPerMessageAndMapsReversedResults|TestBatchClosesRealHeadlessBrowsers' ./internal/tasks
go test -count=1 ./internal/browser ./internal/cdp ./internal/captcha ./internal/strategy
```

本地测试使用 `t.TempDir()` 与本地 HTTP 夹具，不登录真实服务商、不提交真实生成任务。若本机已安装运行环境，可将 `TULAI_FINGERPRINT_TEST_DATA` 指向应用数据目录以复用内核缓存（测试账号资料仍严格隔离在临时目录）。CI 在 Windows x64 与 macOS Apple Silicon 上执行真实内核冒烟与浏览器集成测试。

上游参考：[fingerprint-chromium](https://github.com/adryfish/fingerprint-chromium)、[Playwright 文档](https://playwright.dev/docs/api/class-page#page-route)、[GitHub runner 平台](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)。
