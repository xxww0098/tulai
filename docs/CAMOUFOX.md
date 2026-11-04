# Camoufox 账号环境

服务商账号只使用官方 Camoufox。Windows x64、macOS Intel / Apple Silicon 均有安装入口；不再发现或启动本机 Chrome、Edge 或内置 Chromium。Go 桌面程序与受管 Node 子进程通过 stdin/stdout 私有管道通信，Node 调用 Playwright Firefox / Juggler，不开 CDP TCP 端口。

## 安装与资料

`internal/camoufox/driver/package-lock.json` 固定 Camoufox 0.5.9、Playwright Core 1.62.0 和传递依赖。Node v24.22.2 按当前系统和架构下载，安装时校验 SHA-256。浏览器与模型的下载和版本配对交给官方启动库，缓存按系统和架构隔离，避免切换应用架构时复用错误的浏览器。旧版安装没有平台标记，升级后需要重新安装一次；账号资料和旧缓存保留。安装用临时目录完成，准备好浏览器和指纹数据后再替换已安装运行环境。实际浏览器版本写在其缓存目录的 `version.json`。

安装进度按字节报告：Node 启动环境由 Go 直接下载并读 `Content-Length`；浏览器内核由官方启动库下载，`bridge.mjs install` 复用它的 `webdl` 下载函数，把已下载字节数写成 `{"event":"progress"}` 行。Go 读这些行并推给界面，界面显示百分比、速度和剩余时间；无法按字节计量的阶段只显示状态文字。

运行环境在应用配置目录的 `runtime/camoufox/0.5.9/`，浏览器和模型缓存位于 `runtime/camoufox/cache/<系统>-<架构>/`。账号资料在 `BrowserData/Camoufox/account-*`。每个账号一个独立 Firefox 进程和持久化 profile，Cookie、LocalStorage 和指纹文件彼此分离。同一账号的并发启动串行，已启动时复用本应用记录的进程，不凭一个开放端口判断归属。Firefox 自己的 profile 锁阻止其他进程同时打开资料。

默认在原账号 profile 中加载临时 Firefox 页面桥，通过本机连接绑定到同一账号记录；可在「设置 → 账号与并发」关闭。豆包洗图按账号和标签页隔离会话，每个完成的对话最多用于 10 张；重试、页面变化或未确认结果时从新对话开始。发送请求的响应由 Firefox `webRequest` 流式观察并原样转发；只把状态和受限信号写入日志，豆包原图地址只用于匹配和下载。批次结束后关闭本批浏览器并清理临时扩展，账号登录资料保留。

网页图片下载使用同一指纹引擎，但保留独立的 `DownloadBrowser` 资料目录，不加入生图账号列表。在「浏览器」页的下载专用环境可以直接进入新建下载任务。登录状态和指纹在重新打开后保留；关闭专用页面会结束该下载会话，之后需要重新打开目标网址。下载专用环境暂不支持代理，遇到全局或系统代理时会提示并保留原有会话。

迁移只更新配置库中的资料路径和登录状态。旧 Chrome / Edge / Chromium 目录保留，路径记录在 `browsers.legacy_user_data_dir`。首次切换需要重新登录；后续正常关闭会等待 Firefox 保存数据。账号改名不移动目录；删除账号只清理其当前 Camoufox 目录，拒绝越界和指向数据目录外的父级符号链接。

## 对照 Firefox 技术清单

| 能力 | 当前处理 |
|---|---|
| Juggler 与 CDP | 使用官方 Firefox/Juggler 管道，没有 Chromium CDP 监听端口；本地测试检查启动参数、`navigator.webdriver` 和常见控制台栈读取探针 |
| 启动窗口 | 生图和洗图使用无头账号进程；用户手动启动账号时可显示窗口。macOS 的显卡探测使用同内核无头进程，集成测试对比有头与无头探测结果 |
| Navigator、字体、WebGL、媒体、语音 | 使用 Camoufox 原生能力和官方指纹模型，保存每个账号生成的配置；新账号的屏幕和 Navigator 组合与同目录账号重复时重新抽取 |
| 指纹连续性 | `.tulai-identity.json` 固定设备身份；文件损坏直接报错，不静默生成新身份；代理重启时语言未变则保留语音列表 |
| 系统与浏览器身份 | 选择宿主系统的 Firefox 指纹；内核升级后更新 UA 中 Firefox 版本，保留其他设备参数 |
| 代理隔离 | 账号独立代理优先于全局代理；修改前必须关闭账号 |
| 地理一致性 | 配置代理时由官方 GeoIP 查询出口；启动前清除旧出口派生字段，再计算时区、语言、地理位置和 WebRTC 地址 |
| Cookie 和存储 | 持久化 profile；账号进程彼此独立；采集中的匿名上下文不继承账号 Cookie |
| 主页面、iframe、Worker | 本地集成测试比较 UA、platform、language、languages、hardwareConcurrency 和时区，并检查 UA、语言与请求头一致 |
| 可信输入 | 使用 Firefox 原生输入、鼠标及文件选择器；富文本多行使用浏览器编辑命令，避免按回车意外发送 |
| 资源请求 | 服务商页面及图片资源页使用 Firefox 自己的网络栈，保留 Cookie 和签名 URL；不把 HTTP 错误改成成功 |
| 下载采集重定向 | Playwright 不逐跳拦截重定向；受限下载采集的已拦截请求用独立请求上下文及 `maxRedirects: 0`，对 3xx 直接中止，避免绕过既有目标检查 |
| 安装和退出 | 校验 Node 下载及 npm 锁文件；安装失败不发布半成品；正常关闭保存资料，超时仅终止本应用启动的进程 |
| 取消任务 | 取消未完成的输入或脚本时关闭对应页面，终止延迟操作；采集请求随页面关闭或操作取消断开，保留账号进程和持久化资料 |

屏幕和 Navigator 的可用组合有限。若连续 128 次抽取都重复，启动会报错，不会覆盖或更改已有账号的指纹。

当前没有自编译 Gecko、增加 C++ 补丁或修改 NSS。Canvas / Audio / RFP 的策略沿用所固定 Camoufox 版本的默认值，没有把参考清单中的旧噪声开关直接叠加进去。特别是当前上游已经调整了音频种子行为，不能把旧文档当成当前版本的可用配置。

现有结果截获脚本需要访问站点主世界，已明确开启 Camoufox 的 `main_world_eval`。应用状态保存在每个文档的闭包中，页面桥使用随机事件名，不再把应用变量写到页面全局对象。图片、输入框和 Camoufox 文件输入不再使用固定的应用 DOM 标记。结果截获仍会包装 fetch / XHR，行为与事件仍可能被页面观察；不能保证通过任何网站的风控。

账号页面及资源页保持 Firefox 网络栈；受限下载采集里的 Playwright 请求上下文和独立 Go HTTP 下载使用各自的 HTTP 客户端，不把它们声明为 Firefox NSS 请求。受限采集请求继承当前页面的 Cookie 和代理，响应由 Firefox 接收；拒绝重定向会使需要跳转的受限采集链接报错，页面和已取得的结果会保留。

代理地址（包括认证信息）保存在本机配置库。HTTP 代理可认证；Playwright 不支持 SOCKS5 认证。出口 GeoIP 失败会阻止该账号启动，不退回直连。未设置代理时使用本机网络。实际代理出口、DNS / IPv6 / WebRTC 泄漏仍需在目标网络上验收，单元测试不等于网络泄漏测试。

## 验证

```sh
go run ./cmd/tulai-camoufox-smoke
npm ci --prefix internal/camoufox/driver --ignore-scripts --omit=dev --no-audit --no-fund
node --test internal/camoufox/driver/*.test.mjs
go test -short -race ./internal/camoufox ./internal/browser ./internal/cdp ./internal/captcha ./internal/store ./internal/strategy ./internal/download
go test -count=1 ./internal/browser ./internal/cdp ./internal/captcha ./internal/strategy
TULAI_RENDERED_E2E=1 go test -count=1 ./internal/download
```

本地浏览器测试使用隔离 profile 和 HTTP 夹具，不登录真实服务商、不提交真实生成任务。可将 `TULAI_CAMOUFOX_TEST_DATA` 指向独立运行环境，避免并发测试覆盖应用的启动脚本。未安装运行环境时浏览器测试会跳过，先执行冒烟命令。CI 在 Windows x64、macOS ARM64 和 macOS Intel runner 上执行安装与浏览器集成测试；本地交叉编译不代替 Windows 实机运行。

上游参考：[Camoufox](https://github.com/daijro/camoufox)、[Playwright 路由行为](https://playwright.dev/docs/api/class-page#page-route)、[GitHub runner 平台](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)。用户提供的两份指纹技术清单用作范围参考，具体行为以固定依赖和本仓库测试为准。
