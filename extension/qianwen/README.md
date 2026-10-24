# xbs 千问扩展

这个目录是未打包的 Manifest V3 扩展。它连接本机 Go 桥，在通义 / 千问页面上执行上传、填词、发送、等待和下载。选择器沿用 `internal/strategy/js/ego.js` 里已经用过的锚点：输入框、`添加附件`、`上传图片`、`发送`，以及结果区的 `#qk-markdown-react` / `.markdown-pc-special-class`。上传优先写输入区旁边的 `input[type=file]`（可以是隐藏的），不弹系统文件框。

豆包页面不在这个扩展里。ego / Chrome 路径也还在。

## 覆盖的站点

桌面端打开千问用的是 `https://www.tongyi.com/`。`ego.js` 同时认 `tongyi.com` 和 `qianwen.com`。扩展把这些主机和原来的 `qwen.ai`（含 `chat.qwen.ai`）都放进了 `host_permissions` 和内容脚本。三个域名如果互相跳转，打开哪一个都能接到命令。

## 加载（Load unpacked）

1. 让桥先听着。二选一：

   只试链路，或在下面的实机命令里由冒烟进程自己听：

   ```sh
   go run ./cmd/xbs-ext-smoke
   ```

   默认 `127.0.0.1:18765`，令牌 `dev-token`。要和桌面端一起跑时，不要再开冒烟进程，两边会抢同一个端口。桌面端在 `browserBackend=extension` 时自己听 `extBridgePort` / `extBridgeToken`（新库默认同样是 `18765` 和 `dev-token`）。

2. 打开 `chrome://extensions`，打开开发者模式，加载已解压的扩展程序，指向本目录 `extension/qianwen`。
3. 打开扩展选项，填入和桥相同的令牌、端口，保存。徽章变成 `ON` 表示 hello 已经成功。
4. 用同一个 Chrome 配置打开 `https://www.tongyi.com/`（或 `www.qianwen.com` / `chat.qwen.ai`），手动登录。扩展不会替你登录。

服务工作者日志在扩展卡片的 Service worker 里。认证失败时徽章是 `ERR`。

## 跑一次真实图生图

先完成上面的加载和登录，然后在仓库根目录：

```sh
XBS_EXT_LIVE=1 \
XBS_EXT_IMAGE=/绝对路径/源图.png \
XBS_EXT_PROMPT='把这张图变成水彩' \
go run ./cmd/xbs-ext-smoke
```

可选：`XBS_EXT_WAIT=180`（等待秒数，默认 120），`XBS_EXT_OUT=ext-live-out`（结果目录）。成功时目录里会有 `result-01.png`。这一次会真的点发送。

用桌面端跑同一条路径时，把配置库写成：

```sql
UPDATE settings SET value='extension' WHERE key='browserBackend';
```

`extBridgeToken`、`extBridgePort` 要和扩展选项一致。然后只勾千问、放一张源图，开始任务。豆包任务即使用了这个开关，仍然走 ego / Chrome。界面上的「ego lite / Chrome」下拉框是另一个键 `browser_backend`，不要改它来切换扩展。

## 发送和验证码

点过发送之后，同一条幂等键不会再点第二次。平台没有确认接受时返回 `E_SEND_UNCERTAIN`，整批任务停止，需要你看一眼网页再开新的一批。验证码只检测并尽量把画布裁下来交给主机，不在扩展里自动拖动。出现验证码时任务变成「待验证」。

## 保活

- 连上之后每 15 秒发一条协议 ping。
- `chrome.alarms` 每 0.5 分钟把服务工作者叫起来。Chrome 通常把这个周期限制在 30 秒。
- 桥 45 秒没有任何帧会断开，正在等的命令变成 `E_SW_SLEEP`。
