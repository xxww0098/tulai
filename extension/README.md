# xbs 页面扩展

这个目录是未打包的 Manifest V3 扩展，当前版本 **0.3.4**。桌面端第一次打包是 **0.1.0**，两个编号各自走：扩展不跟着桌面端降到 0.1.0。hello 的 `payload.version` 用清单里的 0.3.4。发布附件里的 `xbs-extension-0.3.4.zip` 就是这个目录，见 [`docs/RELEASING.md`](../docs/RELEASING.md)。

加载一次即可同时驱动通义 / 千问和豆包。后台开两条 WebSocket：一条 hello `qianwen`，一条 hello `doubao`。命令只发给对应站点的标签页。

页面动作分开放：

- `qianwen/page.js`：`添加附件`、`上传图片`、`#qk-markdown-react`
- `doubao/page.js`：`button[data-testid="upload_file_button"]`、菜单全文「上传文件或图片」、结果必须在 `[data-testid="mdbox_image"]`

两边共用 `content.js`。豆包和旧千问页面直接写输入区旁边的 `input[type=file]`（可以是隐藏的），不弹系统文件框。当前千问作曲器里的文件框标了 `aria-hidden`，直接赋值留不住缩略图；有主世界桥时点「上传图片」，由 `qianwen/chooser.js` 拦住这一次 `showPicker`。豆包仍只给已有的 file input 赋值，不点菜单项。ego / Chrome 路径还在。

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

## 下载与去水印

扩展只把结果地址的字节拿回来，不去改 CDN。主机在 `extDownload`（`internal/tasks/extqianwen.go`）里处理，豆包和千问共用这一条。`cmd/xbs-ext-smoke` 实机保存和 `browserBackend=extension` 的任务落盘都写这里返回的字节。

豆包页面地址如果带 `watermark` 或 `wm=` 查询，主机会先用 `strategy.PreferUnwatermarkedURL` 去掉整段查询再让扩展下载。这和 ego 的 `Doubao.FetchResultBytes` 是同一个函数。这次下载失败才退回页面上的原地址。千问结果地址没有这组参数时保持原样，这里也不另写一套千问专用参数。

字节校验通过之后，豆包和千问都会跑 `images.RemoveWatermark`。算法匹配到已校准的角落水印时，冒烟进程和任务引擎写下的就是去水印 PNG。匹配不到、不是静态方图、是动画，或处理失败时，仍然保存下载到的原字节。任务设置里的「去水印（实验）」仍只控制 ego 路径要不要另存 `_去水印.png`。扩展这条路径命中后直接替换要保存的那张图，不再算第二遍。

用桌面端跑同一条路径时，把配置库写成：

```sql
UPDATE settings SET value='extension' WHERE key='browserBackend';
```

`extBridgeToken`、`extBridgePort` 要和扩展选项一致。然后勾千问或豆包、放一张源图，开始任务。界面上的「ego lite / Chrome」下拉框是另一个键 `browser_backend`，不要改它来切换扩展。

## 发送和验证码

点过发送之后，同一条幂等键不会再点第二次。平台没有确认接受时返回 `E_SEND_UNCERTAIN`，整批任务停止，需要你看一眼网页再开新的一批。发送后若页面出现验证码，包括阿里云 Baxia 和地址里的 `_____tmd_____/punish`，内容脚本仍返回 `E_CAPTCHA_HUMAN`，不会记成发送状态未确认。千问滑块不会停在这一步：主机连到本机 Chrome 的远程调试端口，用 `Input.dispatchMouseEvent` 做一次信任拖动（页面里合成的 `MouseEvent` 是 `isTrusted=false`，阿里云经常因此失败）。有拼图背景和拼图块时，在本机用形状匹配找缺口，再按轨道和图片的可移动距离线性换算鼠标行程；没有拼图图时，把滑块拖到轨道尽头。只拖一次，不连拖修正。拖完后同一条命令再问一次页面。滑块消失就继续等图；对不上、端口没开，或拖完还在，任务仍是「待验证」。豆包不走这条拖动。

实机要让载着扩展的 Chrome 开着调试端口。冒烟常用：

```sh
google-chrome \
  --remote-debugging-port=9222 \
  --user-data-dir=/tmp/xbs-chrome-profile \
  --remote-allow-origins=*
```

然后按上面的方式加载扩展、登录，再跑 `XBS_EXT_LIVE=1`。默认会试 `XBS_CDP_URL`、`XBS_CDP_PORT`、`XBS_CHROME_PROFILE/DevToolsActivePort`、`/tmp/xbs-chrome-profile/DevToolsActivePort`，最后是 `127.0.0.1:9222`。`XBS_BAXIA_SOLVE=0` 关掉自动拖动。调试连接只 detach，不关千问标签页。缺口匹配是纯 Go，不调用付费验证码接口。

千问源图要在输入区解码成功（`naturalWidth` 大于 0）才算就绪。预览来自千问 / 通义 CDN 但解码失败（`naturalWidth` 为 0），或输入区出现「上传失败」「加载失败」，都不算就绪，不发送。未解码时，只有 img 上同时写了大于 0 的像素宽高、并且地址是产品 CDN，才当成已经收下。带可用源图发送前会点输入壳里的「AI生图」；胶囊已经选中就不再点，避免退出该模式。当前页面的发送是箭头按钮，灰色（disabled）时不点；切模式后会等到它重新可点。纯文字发送不切换模式。点过发送之后，确认方式不变：输入清空或出现包含提示词的新用户消息才算接受，否则仍是 `E_SEND_UNCERTAIN`。

豆包发送按钮和千问一样要先 pointer 再 click（`Page.activate`）；只调用 `HTMLElement.click()` 时页面常常不收下。 后台标签页时豆包也常不收下，后台在下发页内命令前会 `tabs.update({active:true})` 把目标页拉到前台。附件就绪看输入区是否出现可见的 `attachment-image-card`（卡内 blob 预览经常长期不解码，不能干等 `naturalWidth`）。

豆包结果只收集本轮助手消息里的 `mdbox_image`。用户上传的 `message_image_content` 不会当成生成图。空 SVG 占位不会当成结果，下载只接受 PNG、JPEG、GIF、WebP。千问和豆包等待结果时都会把还没换成 http(s) 实图的生成卡滚进视口并改掉 `loading=lazy`；灰占位、blob、SVG 和很小的 data 图不算结果。

## 保活

- 每条连接在连上之后每 15 秒发一条协议 ping。
- `chrome.alarms` 每 0.5 分钟把服务工作者叫起来。Chrome 通常把这个周期限制在 30 秒。
- 桥 45 秒没有任何帧会断开，正在等的命令变成 `E_SW_SLEEP`。
