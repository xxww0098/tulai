# 错误记录

开发与排障中实际出过的错，按时间倒序。每条写清**现象、原因、处理**，能归纳出做法的再加**避免**。设计和现行策略写在对应模块的文档里（例如 [FINGERPRINT.md](FINGERPRINT.md)），这里只记「哪里出过错、下次怎么不再犯」。

约定：

- 新条目加在对应日期小节的顶部，只记实际发生过的错误。
- 结论标注来源：**实测**（本仓库的测试或探针跑出来的）、**源码**（读过上游源码或补丁，注明版本）、**推断**（没有直接证据，需要复核）。
- 修好的写明改了哪里；没有解决的汇总在该日期小节末尾的「未解决」。

## 2026-11-26 独立账号指纹随机化与反检测

背景：优化每个账号的指纹生成并去掉 CDP 与自动化痕迹，目标是单个账号内部各处取值互相吻合、账号之间互不相同。改动集中在 `internal/fingerprint/driver/fingerprint-identity.mjs`、`internal/fingerprint/driver/bridge.mjs` 与 `internal/cdp/fingerprint_stealth_test.go`，现行策略见 [FINGERPRINT.md](FINGERPRINT.md#指纹自洽与反检测)。内核为 `fingerprint-chromium 148.0.7778.215`，依赖为 `playwright-core 1.62.0`。

### 实现中犯的错误

#### 留着一个多余的启动参数，没查它在有头窗口里的副作用

- **现象**：有头账号窗口顶部一直显示「不受支持的命令行标记：--disable-blink-features=AutomationControlled。稳定性和安全性会有所下降。」
- **原因**：做反检测审查时只确认了这个参数“没有坏处”，没有追问它在当前内核里还有什么用。读源码才知道：Chromium 把整个 `--disable-blink-features` / `--enable-blink-features` 开关列进了“不支持的命令行标记”名单，传任何值都会弹横幅；而 `AutomationControlled` 这个特性在 Blink 里唯一的读取点是 `Navigator::webdriver()`，内核的 `009-webdriver.patch` 已经把它删了。参数对页面没有任何作用，唯一的效果就是横幅。
- **实测**：横幅只出现在有头窗口的启动标签页上，占 52px 视口高度（`outerHeight - innerHeight` 从 121 变成 173）。同一窗口的新标签页、弹出的窗口和无头会话都是 121，而账号窗口与下载专用环境驱动的正是这个启动标签页。带不带参数，页面里的 `navigator.webdriver` 都是 `false`，worker 和 Service Worker 里没有这个属性。
- **处理**：去掉这个参数。新增 `TestFingerprintHeadedWindowOpensWithoutStartupInfobar`，比较有头窗口首个标签页与其它标签页的浏览器外框高度（带着参数时它报 173 对 121）；驱动的两个单元测试断言启动参数里没有任何 blink-feature 开关。
- **避免**：每个启动参数都要能回答「它在当前内核里改变了什么页面可见的行为」，答不上来的参数只是多出来的指纹面。

#### 生成的页面脚本有语法错误，模块检查发现不了

- **现象**：`bridge.mjs` 能通过 `node --check`，页面里安装运行器时却报语法错误。共两处：模板字符串里的 `\n` 被展开成真实换行，落进生成代码的单引号字符串里；另一处多了一个 `}`。
- **原因**：运行器是用模板字符串拼出来的另一段 JS，`node --check` 只检查外层模块。
- **处理**：`\n` 改成 `\\n`，删掉多余的 `}`。
- **避免**：驱动的单元测试用的是假 Playwright，只记录初始化脚本的内容、不编译它，这类错误目前只有真实内核测试（`TestFingerprintPageSurfaceStaysSelfConsistent` 等）能抓到。可以在 `bridge.test.mjs` 里加一条用例，把假 Playwright 记录到的初始化脚本逐个交给 `new vm.Script()` 编译；还没有加。

#### 被 `catch` 吞掉的异常，表现为功能悄悄降级

- **现象**：调用帧的函数名干净了，脚本地址却一直是 `<anonymous>`，没有任何报错。
- **原因**：取页面脚本地址时，用捕获的 `Function.prototype.call` 去调用一个已经绑定好的函数，参数位置放错了，抛出 `TypeError`；外层 `url()` 的 `try/catch` 吞掉异常并返回空串，`sourceURL` 因此为空。
- **处理**：直接调用绑定好的函数（`perfEntries('resource')`）。
- **避免**：`catch` 里不要悄悄返回空值。排查「结果不对但不报错」时，先把 `catch` 到的异常抛出来看。

#### 具名函数表达式会遮蔽页面变量

- **现象**：审查时发现，没有实际触发。
- **原因**：运行器最初写成 `(function k(eval, scope) { ... })`，函数名 `k` 是随机单字母。在函数体内这个名字会遮蔽页面同名的全局变量，以及之前 `Evaluate` 用顶层 `var` 声明的同名变量，页面脚本里的 `k + 1` 可能拿到函数本身。
- **处理**：改成 `const k = function () { ... }`。名字只存在于 eval 自己的作用域，调用栈照样显示 `k`。

#### `eval` 的完成值是 `undefined`，新路径整体没有生效

- **现象**：实测调用栈里仍有 `Generator.next` 等工具帧，以为新路径没有接上。
- **原因**：编译语句是 `const k = function () { ... }`，声明语句没有完成值，`eval` 返回 `undefined`，被当成「源码不是表达式」而退回旧路径。
- **处理**：在编译语句末尾追加 `;k`。

#### 破坏了「顶层 `var` 跨 `Evaluate` 保留」

- **现象**：既有真实内核测试 `TestFingerprintCDPStealthAndEdgeSerialization` 失败，报 `retainedTopVar is not defined`。
- **原因**：新运行器把表达式编译在生成器作用域之外，看不到之前语句式求值留在生成器作用域里的变量。
- **处理**：编译移进生成器内部的 `with(scope)` 块，表达式和语句共用同一个作用域。
- **避免**：改运行器之前先把既有测试里固化的契约读一遍。这个契约只有真实内核测试覆盖，驱动的单元测试不涉及。

#### 误判 canvas 噪声，把它整层关掉

- **现象**：为了修 `measureText` 宽度约为 0 的问题，一度用 `--disable-spoofing=canvas,audio,clientrects` 关掉了整个 canvas 层。
- **原因**：凭印象把它当成「每次读取都随机变化的噪声」，没有先看数据。回看实测数据才发现它按种子稳定：同一账号多次读取、两张相同的画布、`toDataURL` 与 `getImageData` 的结果全部一致，不同账号之间才不同。
- **后果**：关掉它确实能让 `measureText` 恢复（源码），但所有账号会渲染出完全相同的 canvas 和 WebGL 图像，等于主动把账号关联到同一台设备。
- **处理**：恢复 canvas 层，只关 `audio` 和 `clientrects`，`measureText` 作为已知缺陷记录（见下文）。新增 `TestFingerprintAccountsRenderDifferentlyAndStably`，固化「账号之间不同、同一账号跨会话稳定」。
- **避免**：关闭一个指纹层之前，先弄清它提供了什么，并用数据证明它有害。

#### `--force-color-profile=display-p3-d65` 让截图偏色

- **现象**：无头页面里的纯红 `255,0,0` 被截成 `234,51,35`，纯绿、纯蓝同样偏移，PNG 和 JPEG 都一样。浏览器抽屉直接显示这些截图。
- **原因**：为了让无头窗口的 `(color-gamut: p3)` 与有头窗口一致，加了 `--force-color-profile=display-p3-d65`。合成器因此按 P3 输出，截图再按 sRGB 解码。
- **处理**：HDR 主机改用 `hdr10`（页面报告 P3 与 HDR，截图取色不变）；广色域非 HDR 主机改用 `Emulation.setEmulatedMedia` 模拟 `color-gamut: p3`，只影响 `matchMedia` 和 CSS 媒体查询。新增 `TestFingerprintHeadlessReplaysWideGamutDisplaysWithoutRecoloringCaptures`，对截图取色做断言。
- **避免**：改启动参数后，除了看页面里的取值，还要看截图结果。

#### 编辑被拒：文件在读过之后被修改

- **现象**：`Edit` 两次报 `File has been modified since read`。
- **原因**：一次是同一批操作里先改再读，读到的是改之前的快照；一次是刚用脚本改过同一个文件，编辑工具手里的状态过期了。
- **处理**：重新读取后再编辑。

### 测试代码里的错误

这些都只出现在编写测试的过程中，逐条记下是因为每一个都多花了一轮排查：

- Go 原始字符串里的正则写成了 `\\s`、`\\d`（用脚本写文件时多转义了一层），正则永远不匹配。原始字符串里应该写单反斜杠。
- 按行锚定（`^…$`）的正则拿去匹配整段多行文本，永远不匹配。要逐行匹配，或者去掉行锚点。
- 把有头会话记录的显示器数值当成物理像素。记录来自页面，单位是 CSS 像素；只有 `--screen-info` 才乘上 `devicePixelRatio`。
- 用 `assert.equal` 比较对象，比较的是引用。
- 往还不存在的目录写显示器记录（账号资料目录没有先创建），`writeHostDisplay` 直接失败。
- 重启同一个资料目录之前没有关闭上一个浏览器，报 ProcessSingleton 错误，提示资料目录已被另一个实例占用。
- 局部变量取名 `page`，遮蔽了 cdproto 的 `page` 包。
- 用图片宽度反推截图的缩放比例。应该直接读页面的 `devicePixelRatio`。

### 内核与依赖的缺陷

#### `measureText` 宽度约为 0（未解决）

- **现象**（实测）：`ctx.measureText(text).width` 恒为 0 量级的极小值。
- **原因**（源码，`015-canvas-measure-text.patch`）：把 `[-5e-6, 5e-6]` 的噪声值直接传给 `TextMetrics::Shuffle`，而 `Shuffle` 是把宽度、包围盒、基线等各项度量都乘以传入的参数（定义在 `fingerprinting-flags-client-rects-and-measuretext.patch`）；被它替换掉的旧实现传的是约等于 1 的系数。
- **影响**：页面脚本拿不到真实的文字宽度。`measureText` 与 canvas 像素噪声共用 `--disable-spoofing=canvas` 开关，无法单独关闭。
- **现状**：保留 canvas 层，把 `measureText` 记为已知缺陷；tulai 的页面脚本和策略代码不要依赖 `measureText` 的数值，需要文字宽度时用 DOM 测量（`getBoundingClientRect` / `offsetWidth`）。
- **待定**：等上游修复后升级内核，或在页面主世界包一层 `measureText` 并伪装 `toString`。后者会引入新的可检测面，没有做。

#### `audio` 层谎报采样率（已关闭）

- **现象**（实测）：`new OfflineAudioContext(1, 44100, 44100).sampleRate` 读回 `44099.992`。
- **原因**（源码，`003-audio-fingerprint.patch`）：给采样率加上按种子固定的 ±0.01 噪声。同一个补丁里还有一个往渲染帧数加噪声的 `getNoiseData`，只有定义、没有调用。
- **处理**：`--disable-spoofing=audio`。

#### `clientrects` 层平移布局矩形（已关闭）

- **现象**：`getBoundingClientRect` / `getClientRects` 返回的矩形与 CSS 位置、`offsetLeft` 等对不上。
- **原因**（源码，`014-client-rects.patch`）：按种子给每个文档固定一对 x / y 偏移量，最大 ±0.001px，加到 `Element` 与 `Range` 的矩形上；`Element` 上 `position: absolute` 且 `top` / `left` 为固定值的元素跳过。
- **处理**：`--disable-spoofing=clientrects`。

#### `Element.prototype.fakeShadowRoot`（已删除）

- **现象**（实测）：`Element.prototype` 上多出一个 `fakeShadowRoot`，原生 Chrome 没有。
- **原因**（源码，`007-shadow-root.patch`）：为方便自动化访问 closed shadow root，直接返回内部的 shadow root，不区分 open 与 closed。
- **处理**：每个文档启动前执行 `delete Element.prototype.fakeShadowRoot`。

#### 屏幕相关的内核开关只有声明、没有实现

- **现象**（实测）：`--fingerprint-screen-width`、`--fingerprint-screen-height`、`--fingerprint-device-scale-factor`、`--fingerprint-location` 不改变 `screen.*`。
- **原因**（源码）：`000-add-fingerprint-switches.patch` 只声明了这几个开关并转发给渲染进程，全部补丁里没有任何地方读取它们。内核不改写 `screen.*`，有头窗口看到的始终是宿主的真实显示器，无头窗口默认是 800×600。
- **处理**：有头启动时把页面看到的显示器写入账号资料目录旁的 `.tulai-host-display.json`，无头启动用 `--screen-info` 复刻。

#### Playwright `serviceWorkers: 'block'` 会改写 `navigator.serviceWorker.register`

- **现象**（实测）：`register` 的函数源码里写着 "blocked by Playwright"。
- **处理**：不再设置，使用默认的允许。
- **副作用**（源码，Playwright 1.62）：Service Worker 自己发起的请求不经过页面级的 `Fetch.enable` 拦截，只受上下文级路由约束；文档导航请求仍然受控。

#### Playwright 的默认参数

- 默认带 `--force-color-profile=srgb`，会把广色域屏幕压成 sRGB，已在 `ignoreDefaultArgs` 里去掉。
- `ignoreDefaultArgs` 按完整字符串精确匹配，要写 `--force-color-profile=srgb`，不能只写 `--force-color-profile`。

#### 任何 blink-feature 开关都会让有头窗口显示「不受支持的命令行标记」横幅

- **现象**（实测）：带 `--disable-blink-features=<任意值>` 启动时，有头窗口的首个标签页显示一条横幅，可以点右侧的 × 关闭。
- **原因**（源码，`chrome/browser/ui/startup/bad_flags_prompt.cc`）：`kBadFlags` 名单包含 `--disable-blink-features` 与 `--enable-blink-features`，按开关名匹配、不看值。同一份名单里还有 `--no-sandbox`、`--disable-web-security`、`--ignore-certificate-errors`、`--host-resolver-rules`、`--unsafely-treat-insecure-origin-as-secure`、`--use-fake-ui-for-media-stream` 等。
- **影响**：网页读不到横幅，也读不到命令行，只能量到那个标签页的视口少了 52px。无头模式没有界面，不受影响。
- **处理**：启动参数里不再出现任何 blink-feature 开关。`ignoreHTTPSErrors` 会带来名单里的 `--ignore-certificate-errors`，目前只有测试在用。

#### `AutomationControlled` 特性的来源

- **事实**（源码，`content/child/runtime_features.cc`）：`--enable-automation`、`--headless`、`--remote-debugging-pipe` 任意一个存在都会打开 `AutomationControlled`，所以去掉 `--enable-automation` 并不能让它关闭。
- **事实**（源码）：这个特性没有对应的 base feature，只能靠 blink-feature 开关或上述三个启动开关控制。
- **为什么现在不需要管它**：它的唯一读取点已被内核补丁删除。升级内核时如果上游去掉或改了这个补丁，`navigator.webdriver` 会在所有会话里变回 `true`，`TestFingerprintAutomationSignals` 与 `TestFingerprintStrictCSPAndStealthSignals` 会失败。
- **Playwright 1.62 的一条旁路**（源码，`validateBrowserConfig`）：它的 CLI / MCP 启动路径会在参数里没有任何 `--disable-blink-features` 时自动补上 `--disable-blink-features=AutomationControlled`；经典的 `chromium.launchPersistentContext` 走 `_innerDefaultArgs`，不会补。tulai 的桥接只用后者，所以去掉参数后不会被悄悄加回来。

### 查证过的行为

看起来像泄漏、实际正常，或者会限制设计的事实，免得下次重新排查：

- `navigator.deviceMemory` 返回 16 或 32 是正常的。原生 Chromium 148 桌面端默认开启 `kUpdatedDeviceMemoryLimitsFor2026`，`deviceMemory` 的上限是 32、下限是 2（源码：`approximated_device_memory.cc`、`features.cc`）。内核按 `seed % 3` 返回 8 / 16 / 32（`021-device-memory-cap.patch`），都在合法取值内。这条此前只是印象，写这份记录时才读了源码确认。
- 内核默认的 `hardwareConcurrency` 恒为 8 到 32 之间的偶数（`((seed % 13) + 4) * 2`），传入 `--fingerprint-hardware-concurrency` 时以传入值为准（源码，`005-hardware-concurrency-fingerprint.patch`）。
- `--disable-spoofing` 按子串匹配，共五类：`audio`、`font`、`gpu`、`canvas`、`clientrects`。`canvas` 同时管着 `getImageData`、`toDataURL`、WebGL `readPixels` 和 `measureText`。
- `window.chrome.runtime` 为 `undefined` 是正常的：原生 Chrome 的普通网页上，只有安装了在 `externally_connectable` 里声明该网站的扩展才会暴露它。
- 无头模式下 UA 的产品名是 `Chrome` 而不是 `HeadlessChrome`（源码，`010-headless.patch`）。

### 调用栈与 CSP 的实测结论

- `//# sourceURL=` 对 `Page.addScriptToEvaluateOnNewDocument` 注入的脚本和运行时 eval 的脚本都生效，要写绝对地址。
- eval 脚本里的匿名函数在调用栈里显示为 `eval` 帧（地址仍是脚本地址）；有名字的函数，或者能从变量名推断出名字的函数（`const k = function () {}`），显示名字。
- 在新的微任务里执行的代码不会带上提交它的调试器帧。把求值放进微任务，站点钩子才看不到 `Generator.next`、`callFunctionOn`。
- 严格 CSP（没有 `unsafe-eval`）下页面自己的 `eval` 被拦截，但运行器的编译在 CDP 调用内同步完成，不受影响（`TestFingerprintStrictCSPAndStealthSignals` 覆盖）。
- 多语句脚本仍然走同步生成器路径，会暴露工具帧。传给 `Session.Evaluate` 的脚本要写成单个表达式，必要时用 IIFE 包裹。按正则粗筛 Go 侧的 87 处求值调用，没有发现多语句写法。

### 环境与协作

- 真实内核测试要把 `TULAI_FINGERPRINT_TEST_DATA` 指向运行环境的副本（目录里要有 `runtime/fingerprint/<version>/`），不要指向正在运行的应用数据目录；启动前清除代理环境变量（`HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY` 及其小写形式）。有头测试会打开真实窗口。
- 宿主显示器在测试期间变过：先是 1728×1117、30 位色深、HDR，后来是 1920×1080、24 位、广色域非 HDR。测试不能写死显示器数值，只能比较「有头记录 → 无头回放」是否一致。
- 这段时间仓库里有别的进程在同时改文件，例如 `bridge.mjs` 的文本输入逻辑、`internal/cdp/mouse.go`、`internal/cdp/client.go`、`internal/strategy/`、`internal/tasks/`、`internal/ui/` 与 `frontend/`。做法是每次改之前重新读取、不覆盖对方的改动、在合并后的状态上重跑测试。
- 并发改动留下的过期断言：那个进程把页面基础运行时的标记 `__tulaiBaseInstalled` 从 3 升到 4，`inject.go`、`input_test.go`、`mouse_test.go` 和 `fingerprint_test.go` 里的几处跟着改了，漏掉 `automation_test.go` 与 `fingerprint_test.go` 里各一处 `probe.Base != 3`，于是 `TestFingerprintAutomationSignals` 和 `TestFingerprintStrictCSPAndStealthSignals` 失败，探针的其余各项都正常。已把这两处改成 4。教训：改一个带版本号的标记时全局搜索它的所有引用；验证结论只在跑的那一刻成立，别人改过之后要重跑。
- 改到一半的文件会让整包编译失败：11:36 那一轮 `internal/ui` 与 `internal/tasks` 报 `ListBrowsers`、`setSlotPaused`、`sourceHash` 未定义，是那个进程改到一半的文件，12 秒后补齐，重跑通过。遇到不属于自己改动的编译错误，先确认来源，等一等再重跑，不要去“修”别人的半成品。
- `internal/fingerprint/`、`docs/FINGERPRINT.md`、`internal/cdp/fingerprint_stealth_test.go` 和本文档在 git 里尚未跟踪，改之前先在仓库外留一份原件。

### 验证范围

对应 2026-11-26 11:35–11:41 的代码状态，包含同一时间另一进程的改动和上面补的两处断言：

- 驱动单元测试 `node --test internal/fingerprint/driver/*.test.mjs`：30 项全部通过。
- `go vet ./...`、`go build ./cmd/...`：没有输出。
- `go test -short -race ./...`：全部通过。
- 真实内核集成测试（`TULAI_FINGERPRINT_TEST_DATA` 指向运行环境副本）：`./internal/browser`、`./internal/cdp`、`./internal/captcha`、`./internal/strategy`、`./internal/download`、`./internal/tasks`、`./internal/fingerprint` 全部通过。
- `TestFingerprintHeadedWindowOpensWithoutStartupInfobar` 与两个 `webdriver` 测试各连跑 3 次通过；临时把 `--disable-blink-features=AutomationControlled` 加回去时，它报首个标签页 173px、其它标签页 121px。
- 没有运行：`go tool mygo generate` 漂移检查、前端检查和扩展脚本测试。本次改动不涉及 IPC、前端与 `extension/`。

### 未解决

- `measureText` 宽度约为 0：需要在上游升级与页面包装之间做决定，见上文。
- 有意保留或暂无低风险方案的差异（`userActivation`、单次 `insertText`、2–3 步点击轨迹、Service Worker 请求不经页面级拦截、弹出窗口首个文档的色域）列在 [FINGERPRINT.md](FINGERPRINT.md#指纹自洽与反检测) 的「仍然存在的差异」，不在这里重复。
