# 千问页面动作

这个目录不是一份单独的扩展，没有 `manifest.json`。加载未打包扩展时指向仓库里的 [`extension/`](../README.md)。那一份扩展会在通义 / 千问页面注入本目录的 `page.js`，并另外用一条 hello 驱动豆包。

选择器沿用 `internal/strategy/js/ego.js`：输入框、`添加附件`、`上传图片`、`发送`，以及结果区的 `#qk-markdown-react` / `.markdown-pc-special-class`。输入区旁边已有 `input[type=file]` 时直接赋值。当前页面要先点「上传图片」才会临时创建 input 并调用 `showPicker`；`chooser.js` 在主世界接住这一次调用，把本地图片写进去，不弹系统文件框。点「添加附件」和「上传图片」时先发 pointer 事件再 click，避免只命中 Radix 的 pointerdown。

`workspace-zb-cdn.qianwen.com` 的预览有时解码失败（`naturalWidth` 为 0）。附件还在输入区、页面没有「上传失败」时仍算就绪；出现「上传失败」则不算。发送后若整页或 iframe 进入 Baxia / `_____tmd_____/punish`，按验证码返回，而不是 `E_SEND_UNCERTAIN`。

桌面端打开千问用的是 `https://www.tongyi.com/`。扩展同时匹配 `tongyi.com`、`qianwen.com` 和 `qwen.ai`（含 `chat.qwen.ai`）。

本地核对锚点：

```sh
node extension/qianwen/page_test.js
```

实机步骤、令牌和 `browserBackend` 见 [`extension/README.md`](../README.md)。
