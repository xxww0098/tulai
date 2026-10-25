# 扩展控制面

桌面端继续负责界面、任务队列、SQLite、验证码求解和落盘。千问和豆包的页面操作都可以由同一个 MV3 扩展执行。两边用只绑定在 `127.0.0.1` 的 WebSocket 通信，hello 时校验令牌。一条连接只登记一个 `platform`。扩展会同时 hello `qianwen` 和 `doubao`。命令按上传、填词、发送、等待、下载拆开。

`internal/cdp` 和 ego 都还在。`browser_backend` 仍是 `ego lite` 或 `Chrome`。另一个键 `browserBackend=extension` 让千问和豆包改走扩展。

## 这一期包含什么

- `internal/extbridge`：v1 信封、本机监听、令牌 hello、ping/pong、会话表、带超时和幂等键的 RPC。
- `extension/`：一份未打包扩展。`qianwen/page.js` 和 `doubao/page.js` 各自实现页面动作，共用 `content.js` 和 `background.js`。找不到节点时仍返回 `E_SELECTOR_MISS`。
- `cmd/xbs-ext-smoke`：手动加载扩展时使用的桥。`XBS_EXT_LIVE=1` 时跑一次图生图。`XBS_EXT_PLATFORM=doubao` 时打到豆包那条 hello。
- 配置键 `browserBackend`，取值 `extension` 或 `ego`，默认 `ego`。千问和豆包任务都会读它。另外两个键 `extBridgePort`（默认 `18765`）和 `extBridgeToken`（默认 `dev-token`）要和扩展选项一致。

不删除 CDP。验证码只检测并把画布交回主机，扩展里不自动求解。

## 连接

```
小帮手 Go                         Chrome MV3
internal/extbridge                extension/
  监听 127.0.0.1:18765                background.js（两条 hello）
  hello 校验 token                    每 15s ping
  Call(upload/...)  ───────────────►  转 COMMAND_FROM_HOST
  等待 ack / event / error  ◄───────  content.js + 对应站点的 page.js
```

扩展是 WebSocket 客户端。Go 是主机。`Session.Call` 是桌面端发出命令的入口。

## 信封

字段顺序固定为 `v`、`id`、`type`、`ts`、`platform`、`sessionId`、`taskId`、`idempotencyKey`、`payload`。`v` 目前只有 `1`。`ts` 是 Unix 毫秒。`type` 是 `cmd`、`ack`、`event`、`ping`、`pong`、`hello`、`error`。`platform` 是 `qianwen` 或 `doubao`（对应界面里的千问、豆包）。

第一条消息必须是 hello：

```json
{"v":1,"id":"...","type":"hello","ts":1,"platform":"qianwen","payload":{"token":"...","version":"0.1.0"}}
```

令牌正确时主机回 `type=ack`，`payload.name` 为 `helloAck`，并带上 `sessionId`。令牌错误、或 hello 之前发送了别的消息，主机回 `E_AUTH` 并关闭连接。令牌放在 payload 里，不放在 URL 上。

之后任何一方都可以发 `ping`，对方回相同 `id` 的 `pong`。扩展在收到 helloAck 之后每 15 秒发一次。主机空闲 45 秒则断开，正在等待的调用得到 `E_SW_SLEEP`。

命令名：`ping`、`helloAck`、`upload`、`setPrompt`、`send`、`waitResult`、`download`、`captchaAssist`、`cancel`、`status`。

`Call` 发送 `type=cmd`，并等待相同 `id` 的第一条 `ack`、`event` 或 `error`。`ack` 里 `ok:false` 会作为错误返回，信封本身仍然带回来。页面找不到锚点时仍是 ack，不是断开连接：

```json
{"name":"upload","ok":false,"code":"E_SELECTOR_MISS","message":"未找到当前输入区上传入口"}
```

错误码：`E_NOT_CONNECTED`、`E_NO_TAB`、`E_NOT_LOGGED_IN`、`E_SELECTOR_MISS`、`E_UPLOAD_FAIL`、`E_SEND_UNCERTAIN`、`E_TIMEOUT`、`E_CAPTCHA_HUMAN`、`E_CAPTCHA_SOLVE_FAIL`、`E_DOWNLOAD_FAIL`、`E_SW_SLEEP`、`E_PROTOCOL`、`E_AUTH`、`E_CANCELLED`。

同一个 `idempotencyKey` 的已完成结果会缓存最近 64 条，包括 `ok:false`。重复调用直接返回缓存，不再发给扩展。进行中的相同键合并为同一次等待。调用超时是 `E_TIMEOUT`，不写入缓存，下一次会重新发送。所以只有「点了就不能再点」的发送命令带这个键；上传分片不带，避免一次失败把后面的重试也吃掉。内容脚本另外按上传编号记住已经交给 `input` 的文件。

`status` 由后台直接回答，列出这一条 hello 对应站点的标签页 `idle` 或 `busy`。它不经过内容脚本，所以没打开页面也能验证链路。千问匹配 `tongyi.com`、`qianwen.com` 和 `qwen.ai`。豆包匹配 `doubao.com`（含 `www.doubao.com`，桌面端首页是 `https://www.doubao.com/chat/`）。

等待结果只用一条最终 ack 带回图片 URL。不要对同一个 `id` 先发一条进度 event，否则 `Call` 会在那条 event 上返回，后面的 ack 会被丢掉。

## 怎么试

```sh
go test ./internal/extbridge/... ./internal/tasks/ -count=1
node --check extension/content.js
node extension/qianwen/page_test.js
node extension/doubao/page_test.js
go run ./cmd/xbs-ext-smoke
```

然后按 [`extension/README.md`](../extension/README.md) 加载未打包扩展。会看到两条 hello（`qianwen` 和 `doubao`）以及各自的 `ping/pong`。`status` 只列出这一条连接对应的标签页。

实机图生图（需要已登录的页面，CI 做不到）：

```sh
XBS_EXT_LIVE=1 XBS_EXT_IMAGE=/绝对路径/源图.png XBS_EXT_PROMPT='水彩风格' go run ./cmd/xbs-ext-smoke
XBS_EXT_PLATFORM=doubao XBS_EXT_LIVE=1 XBS_EXT_IMAGE=/绝对路径/源图.png XBS_EXT_PROMPT='水彩风格' go run ./cmd/xbs-ext-smoke
```

监听地址只允许字面量 `127.0.0.1` 或 `::1`。`0.0.0.0` 和主机名 `localhost` 会在绑定前被拒绝。冒烟进程和桌面端不要同时占用 `18765`。

## browserBackend

新库的 `settings.browserBackend` 默认是 `ego`。写成 `extension` 之后，千问和豆包任务不再打开 ego / Chrome，改为等对应平台的扩展 hello，再按上传、填词、发送、等待、下载调用 `Session.Call`。浏览器页的下拉框仍然只切换 `browser_backend`，避免把未在界面上露出的扩展选项塞进现有的 ego lite / Chrome 选择。

发送的幂等键是「这一批 + 任务序号 + send」。成功和 `E_SEND_UNCERTAIN` 会被桥缓存，同一批里不会再发第二次 send。调用超时不进缓存；内容脚本自己记住已经点过的键，超时重试只复查输入框是否清空，不再点击。`E_SEND_UNCERTAIN` 会停掉整批，和 ego 路径的「发送状态未确认」一样。

## 已知风险

- 千问改版会让 `添加附件`、`上传图片`、`发送` 或 markdown 容器对不上。豆包改版会让 `upload_file_button`、`上传文件或图片` 或 `[data-testid="mdbox_image"]` 对不上。命令返回 `E_SELECTOR_MISS` 或 `E_UPLOAD_FAIL`。锚点写在对应 `page.js` 顶部注释里，对照 `ego.js`，不要换成临时 class。
- 豆包结果只收本轮助手消息里的 `mdbox_image`。用户消息上的 `message_image_content` 不算结果。空 SVG / blob 占位即使 `naturalWidth` 更大也丢掉，同一张卡片里只留更大的 http(s) 实图（含 srcset、data-src、背景图）。消息角色属性如果对不上，等待会超时，不要猜一套新的卡片 class。
- 上传要等输入区里出现新的完整预览图。如果站点改成只显示文件名、不插 `img`，会报未就绪。豆包「立即下载电脑版」会在清理附件时尝试点旁边的「关闭」；范围里有输入框或验证码时不点。
- 结果图先在页面里 `fetch`（带 cookie），跨域失败再让后台下载并回传绝对路径。正文不是 PNG、JPEG、GIF、WebP 时返回 `E_DOWNLOAD_FAIL`，不会把空 SVG 壳存成 `.png`。CDN 不给扩展读正文时，这一步也会是 `E_DOWNLOAD_FAIL`。豆包图片编辑器里的「保存」不由扩展点击，下载用的是 `mdbox_image` 上的图片地址。主机拿到字节后才去水印：豆包地址若带 `watermark` 或 `wm=`，先按 `strategy.PreferUnwatermarkedURL` 去掉查询再下载，失败才用页面原地址；千问共用这条下载，没有这组参数时不改地址，也不另做千问专用剥离。随后豆包和千问都跑 `images.RemoveWatermark`。匹配成功时，冒烟目录里的 `result-01.png` 和扩展任务落盘的那张图就是去水印 PNG。
- 验证码只报告 `captchaBlocked`，并尽量附上 canvas 的 data URL。没有自动拖动。桌面端把这种情况当成需要人工验证，停掉这一批。
- `qwen.ai` 沿用通义的卡片 class 兜底。国际站如果结构不同，要拿一次真实 DOM 再改选择器，不要猜。
- 豆包页面目前只匹配 `doubao.com`。聊天和出图都在 `https://www.doubao.com/chat/`，没有单独的图片站主机。
