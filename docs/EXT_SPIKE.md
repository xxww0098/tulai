# 扩展控制面（PR0 + PR1）

桌面端继续负责界面、任务队列、SQLite、验证码求解和落盘。页面操作改由 MV3 扩展执行。两边用只绑定在 `127.0.0.1` 的 WebSocket 通信，hello 时校验令牌。这是 Omni-Adapter 式的分工，但命令按上传、填词、发送、等待、下载拆开，而不是一条打包的生图任务。

这一期不替换 `internal/cdp` 和 ego。任务引擎仍然读取原来的 `browser_backend`（`ego lite` 或 `Chrome`）。

## 这一期包含什么

- `internal/extbridge`：v1 信封、本机监听、令牌 hello、ping/pong、会话表、带超时和幂等键的 RPC。
- `extension/qianwen`：千问 MV3 骨架。消息能从 Go 走到内容脚本再回来。六个页面命令返回 `E_SELECTOR_MISS`。
- `cmd/xbs-ext-smoke`：手动加载扩展时使用的桥。
- 配置键 `browserBackend`，取值 `extension` 或 `ego`，默认 `ego`。任务引擎还不读它。

不包含豆包扩展、真实 DOM 自动化，也不删除 CDP。

## 连接

```
小帮手 Go                         Chrome MV3
internal/extbridge                extension/qianwen
  监听 127.0.0.1:18765                background.js
  hello 校验 token                    每 15s ping
  Call(upload/...)  ───────────────►  转 COMMAND_FROM_HOST
  等待 ack / event / error  ◄───────  content.js（目前是选择器占位）
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

`Call` 发送 `type=cmd`，并等待相同 `id` 的第一条 `ack`、`event` 或 `error`。`ack` 里 `ok:false` 会作为错误返回，信封本身仍然带回来。页面命令的占位结果是 ack，不是断开连接：

```json
{"name":"upload","ok":false,"code":"E_SELECTOR_MISS","message":"stub: upload selector is not implemented until PR2"}
```

错误码：`E_NOT_CONNECTED`、`E_NO_TAB`、`E_NOT_LOGGED_IN`、`E_SELECTOR_MISS`、`E_UPLOAD_FAIL`、`E_SEND_UNCERTAIN`、`E_TIMEOUT`、`E_CAPTCHA_HUMAN`、`E_CAPTCHA_SOLVE_FAIL`、`E_DOWNLOAD_FAIL`、`E_SW_SLEEP`、`E_PROTOCOL`、`E_AUTH`、`E_CANCELLED`。

同一个 `idempotencyKey` 的已完成结果会缓存最近 64 条。重复调用直接返回缓存，不再发给扩展。进行中的相同键合并为同一次等待。调用超时是 `E_TIMEOUT`，不写入缓存，下一次会重新发送。

`status` 由后台直接回答，列出千问标签页的 `idle` 或 `busy`。它不经过内容脚本，所以没打开页面也能验证链路。

## 怎么试

```sh
go test ./internal/extbridge/...
go run ./cmd/xbs-ext-smoke
```

然后按 [`extension/qianwen/README.md`](../extension/qianwen/README.md) 加载未打包扩展，把选项页的令牌和端口设成冒烟进程打印的值。看到 `hello 已通过` 和 `ping/pong 成功` 即表示这一期的链路是通的。随后的 `status` 会列出已打开的 qwen.ai 标签页；若对一个页面命令调用 `Call`，内容脚本会回 `E_SELECTOR_MISS`。

监听地址只允许字面量 `127.0.0.1` 或 `::1`。`0.0.0.0` 和主机名 `localhost` 会在绑定前被拒绝。

## browserBackend

新库的 `settings.browserBackend` 默认是 `ego`。把它写成 `extension` 只表示以后要走扩展，`internal/tasks` 现在仍根据 `browser_backend` 在 ego lite 和 Chrome 之间选择。没有把扩展加进浏览器页的下拉框，避免一个未接通的选项改变现有任务。后续再让引擎在 `extension` 时调用 `extbridge.Session.Call`，并在界面上单独露出这个开关。

## 留给 PR2

- 千问页面上的上传、填词、发送、等待结果、下载和验证码辅助。当前这些命令只证明消息能往返。
- 确认真实站点是 `chat.qwen.ai` 还是现有策略使用的 `https://www.tongyi.com/`，再补主机权限。
- 让 `browserBackend=extension` 进入任务引擎。桌面端继续保存文件、跑队列和求解验证码；扩展只把验证码画面交回来。
- 豆包扩展。
- 若等待结果需要多条进度事件，再把 `Call` 从「第一条 event 即完成」改成可区分的最终 ack。这一期一条 event 就会结束等待。
