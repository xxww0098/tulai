# 豆包页面动作

这个目录不是一份单独的扩展，没有 `manifest.json`。加载未打包扩展时指向仓库里的 [`extension/`](../README.md)。那一份扩展会在豆包页面注入本目录的 `page.js`。

锚点来自 `internal/strategy/js/ego.js`，不是新编的 class：

- 首页 `https://www.doubao.com/chat/`，主机 `doubao.com`（含 `www` 和裸域）
- 上传入口 `button[data-testid="upload_file_button"]`，二级菜单全文「上传文件或图片」
- 仍优先写输入区旁边的隐藏 `input[type=file]`，不点击菜单项本身
- 结果必须在 `[data-testid="mdbox_image"]` 内；`message_image_content` 是用户图。空 SVG / blob 占位不算结果
- 发送按钮与千问相同：`chat_input_send_button` 或「发送」
- 验证码只检测。客户端推广「立即下载电脑版」旁的「关闭」只在清理附件时点一次

本地核对锚点：

```sh
node extension/doubao/page_test.js
```
