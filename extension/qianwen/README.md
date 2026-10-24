# 千问页面动作

这个目录不是一份单独的扩展，没有 `manifest.json`。加载未打包扩展时指向仓库里的 [`extension/`](../README.md)。那一份扩展会在通义 / 千问页面注入本目录的 `page.js`，并另外用一条 hello 驱动豆包。

选择器沿用 `internal/strategy/js/ego.js`：输入框、`添加附件`、`上传图片`、`发送`，以及结果区的 `#qk-markdown-react` / `.markdown-pc-special-class`。输入区旁边已有 `input[type=file]` 时直接赋值。当前页面要先点「上传图片」才会临时创建 input 并调用 `showPicker`；`chooser.js` 在主世界接住这一次调用，把本地图片写进去，不弹系统文件框。点「添加附件」和「上传图片」时先发 pointer 事件再 click，避免只命中 Radix 的 pointerdown。

源图预览必须能解码。`naturalWidth` 为 0 的 CDN 图不算就绪，输入区里的「上传失败」「加载失败」也不算。未解码时，只有产品 CDN 地址并且 img 上写了大于 0 的宽高才算收下。带可用源图发送前先进入「AI生图」（胶囊 `aria-pressed`，或占位符含「图像生成与编辑」）；已经选中就不再点。纯文字不切换。发送后若整页或 iframe 进入 Baxia / `_____tmd_____/punish`，按验证码返回，而不是 `E_SEND_UNCERTAIN`。

桌面端打开千问用的是 `https://www.tongyi.com/`。扩展同时匹配 `tongyi.com`、`qianwen.com` 和 `qwen.ai`（含 `chat.qwen.ai`）。

本地核对锚点：

```sh
node extension/qianwen/page_test.js
```

实机步骤、令牌和 `browserBackend` 见 [`extension/README.md`](../README.md)。
