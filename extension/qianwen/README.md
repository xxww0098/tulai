# xbs 千问扩展（PR1 骨架）

这个目录是未打包的 Manifest V3 扩展。它连接本机 Go 桥，完成 hello 和 ping/pong，并把命令转给千问标签页。`upload`、`setPrompt`、`send`、`waitResult`、`download`、`captchaAssist` 目前固定返回 `E_SELECTOR_MISS`。页面点击和 DOM 选择器留到 PR2。

## 加载（Load unpacked）

1. 在仓库根目录启动桥，并记下它打印的地址和令牌：

   ```sh
   go run ./cmd/xbs-ext-smoke
   ```

   默认监听 `127.0.0.1:18765`，令牌是 `dev-token`。可以用环境变量覆盖：

   ```sh
   XBS_EXT_ADDR=127.0.0.1:18765 XBS_EXT_TOKEN=choose-a-token go run ./cmd/xbs-ext-smoke
   ```

2. 打开 `chrome://extensions`。
3. 打开开发者模式（Developer mode）。
4. 选择「加载已解压的扩展程序」（Load unpacked），指向本目录 `extension/qianwen`。
5. 打开扩展的详情页，进入「扩展程序选项」（Extension options）。填入和第 1 步相同的令牌和端口，保存。
6. 徽章变成 `ON` 时，hello 已经成功。冒烟进程会打印 `hello 已通过`、`ping/pong 成功`，以及一条 `status` 应答。没有打开千问页时，`status` 的 `tabs` 为空，这是正常的。

服务工作者的日志在扩展卡片的「Service worker」里。认证失败时徽章是 `ERR`，选项页会显示 `E_AUTH` 或桥返回的说明。改令牌后会自动重连。

## 它在保活什么

- 连接成功后每 15 秒发送一条协议 `ping`（小于 20 秒），用来减轻 MV3 服务工作者休眠。
- `chrome.alarms` 每 0.5 分钟唤起一次服务工作者。Chrome 通常把周期闹钟限制在 30 秒，所以闹钟只负责把进程叫起来并重连，不代替 15 秒心跳。
- 桥如果 45 秒没有收到任何帧，会断开并把正在等待的命令标成 `E_SW_SLEEP`。

## 和页面的关系

内容脚本只匹配 `https://chat.qwen.ai/*` 和 `https://*.qwen.ai/*`。后台把主机命令以 `COMMAND_FROM_HOST` 发给其中一个标签页，再把内容脚本的 ack 原样送回 Go。取消命令在没有标签页时直接成功；有标签页时也会转到内容脚本，目前只确认没有页面内任务。

现有桌面端打开千问用的是 `https://www.tongyi.com/`。本扩展按这一期的主机权限只覆盖 qwen.ai。要在真实千问页面上做 PR2 之前，先确认实际域名，再补 host permission。
