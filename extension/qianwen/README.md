# 千问页面动作

这个目录不是一份单独的扩展，没有 `manifest.json`。加载未打包扩展时指向仓库里的 [`extension/`](../README.md)。那一份扩展会在通义 / 千问页面注入本目录的 `page.js`，并另外用一条 hello 驱动豆包。

选择器沿用 `internal/strategy/js/ego.js`：输入框、`添加附件`、`上传图片`、`发送`，以及结果区的 `#qk-markdown-react` / `.markdown-pc-special-class`。当前 www.qianwen.com 的文件框是 `aria-hidden` 的选择器，直接赋值留不住缩略图；有主世界桥时仍点「添加附件」再点「上传图片」，由 `chooser.js` 接住 `showPicker`，不弹系统文件框。没有 `aria-hidden` 的旧 input 仍直接赋值。点「添加附件」和「上传图片」时先发 pointer 事件再 click，避免只命中 Radix 的 pointerdown。菜单项可以是 `role="menuitem"`，也可以是菜单里文本恰好为「上传图片」的节点。

输入框是 Slate。提示词用 `beforeinput` 写入，页面才会把字记进自己的文本状态，右侧箭头发送才会从灰色变成可点。箭头没有「发送」两个字，锚点是 `data-session-switch-target="send-query"`，或原来的「发送」「发送消息」。`disabled` 的箭头不算就绪。

源图预览必须能解码。`naturalWidth` 为 0 的 CDN 图不算就绪，输入区里的「上传失败」「加载失败」也不算。未解码时，只有产品 CDN 地址并且 img 上写了大于 0 的宽高才算收下。带可用源图发送前先进入输入壳里的「AI生图」胶囊（`aria-pressed` / `aria-checked`，或占位符含「图像生成与编辑」）。已经选中就不再点，再点会退出该模式。不点整页更靠前的同名入口，那会进创意生图，箭头保持灰色。切换后若输入框被重绘，会再写一次提示词，并等到箭头重新可点。纯文字不切换。发送后若整页或 iframe 进入 Baxia / `_____tmd_____/punish`，按验证码返回，而不是 `E_SEND_UNCERTAIN`。

桌面端打开千问用的是 `https://www.tongyi.com/`。扩展同时匹配 `tongyi.com`、`qianwen.com` 和 `qwen.ai`（含 `chat.qwen.ai`）。

本地核对锚点：

```sh
node extension/qianwen/page_test.js
```

实机步骤、令牌和 `browserBackend` 见 [`extension/README.md`](../README.md)。
