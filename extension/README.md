# xbs 页面扩展

这个目录是未打包的 Manifest V3 扩展。加载一次即可同时驱动通义 / 千问和豆包。后台开两条 WebSocket：一条 hello `qianwen`，一条 hello `doubao`。命令只发给对应站点的标签页。

页面动作分开放：

- `qianwen/page.js`：`添加附件`、`上传图片`、`#qk-markdown-react`
- `doubao/page.js`：`button[data-testid="upload_file_button"]`、菜单全文「上传文件或图片」、结果必须在 `[data-testid="mdbox_image"]`

两边共用 `content.js`。上传优先写输入区旁边的 `input[type=file]`（可以是隐藏的），不弹系统文件框。千问当前菜单里没有这个 input：`qianwen/chooser.js` 在页面主世界拦住这一次 `showPicker`，点「上传图片」后把源图交进去。豆包仍只给已有的 file input 赋值，不点菜单项。ego / Chrome 路径还在。

## 加载（Load unpacked）

1. 让桥先听着。二选一：

   ```sh
   go run ./cmd/xbs-ext-smoke
   ```

   默认 `127.0.0.1:18765`，令牌 `dev-token`。要和桌面端一起跑时，不要再开冒烟进程，两边会抢同一个端口。桌面端在 `browserBackend=extension` 时自己听 `extBridgePort` / `extBridgeToken`（新库默认同样是 `18765` 和 `dev-token`）。

2. 打开 `chrome://extensions`，打开开发者模式，加载已解压的扩展程序，指向本目录 `extension`。不要指向 `extension/qianwen` 或 `extension/doubao`，那两个目录没有清单。
3. 打开扩展选项，填入和桥相同的令牌、端口，保存。徽章变成 `ON` 表示至少一条 hello 已经成功。冒烟进程会先后打印 `platform=qianwen` 和 `platform=doubao`。
4. 用同一个 Chrome 配置打开要跑的页面，手动登录。扩展不会替你登录。
   - 千问：`https://www.tongyi.com/`（也认 `qianwen.com`、`chat.qwen.ai`）
   - 豆包：`https://www.doubao.com/chat/`

服务工作者日志在扩展卡片的 Service worker 里。认证失败时徽章是 `ERR`。

## 跑一次真实图生图

先完成上面的加载和登录，然后在仓库根目录。千问：

```sh
XBS_EXT_LIVE=1 \
XBS_EXT_IMAGE=/绝对路径/源图.png \
XBS_EXT_PROMPT='把这张图变成水彩' \
go run ./cmd/xbs-ext-smoke
```

豆包加上平台：

```sh
XBS_EXT_PLATFORM=doubao \
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

`extBridgeToken`、`extBridgePort` 要和扩展选项一致。然后勾千问或豆包、放一张源图，开始任务。界面上的「ego lite / Chrome」下拉框是另一个键 `browser_backend`，不要改它来切换扩展。

## 发送和验证码

点过发送之后，同一条幂等键不会再点第二次。平台没有确认接受时返回 `E_SEND_UNCERTAIN`，整批任务停止，需要你看一眼网页再开新的一批。发送后若页面出现验证码，包括阿里云 Baxia 和地址里的 `_____tmd_____/punish`，返回 `E_CAPTCHA_HUMAN`，任务变成「待验证」，不会记成发送状态未确认。验证码只检测并尽量把画布裁下来交给主机，不在扩展里自动拖动。

千问附件预览如果已经挂在输入区、来自千问 / 通义 CDN，但图片解码失败（`naturalWidth` 为 0），且页面没有「上传失败」，仍视为源图就绪。输入区出现「上传失败」则上传失败，不继续发送。

豆包结果只收集本轮助手消息里的 `mdbox_image`。用户上传的 `message_image_content` 不会当成生成图。空 SVG 占位不会当成结果，下载只接受 PNG、JPEG、GIF、WebP。

## 保活

- 每条连接在连上之后每 15 秒发一条协议 ping。
- `chrome.alarms` 每 0.5 分钟把服务工作者叫起来。Chrome 通常把这个周期限制在 30 秒。
- 桥 45 秒没有任何帧会断开，正在等的命令变成 `E_SW_SLEEP`。
