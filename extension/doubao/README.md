# 豆包页面动作

这个目录不是一份单独的扩展，没有 `manifest.json`。加载未打包扩展时指向仓库里的 [`extension/`](../README.md)。那一份扩展会在豆包页面注入本目录的 `page.js`。

锚点来自 `internal/strategy/js/ego.js`，不是新编的 class：

- 首页 `https://www.doubao.com/chat/`，主机 `doubao.com`（含 `www` 和裸域）
- 洗图和生图都先点击输入区的「图像生成」（`skill_bar_button_3`），确认已选模式后才上传或填词。已选中时不重复点击；切换失败则停止任务。2026-11 实测：冷加载页面点击后输入区要 2 秒多才重绘，未挂载完时第一次点击会被吞掉——等待窗口 12 秒、每 3.5 秒补点，已选证据优先看 `skill_input_exit_button`
- 图像模式激活后输入区有「比例」触发按钮（文本「比例 自动」/「比例 4:3」，`aria-haspopup`），下拉为 radix `[role="menu"]`，选项 自动 / 9:16 / 2:3 / 3:4 / 1:1 / 4:3 / 3:2 / 16:9。`setOutputRatio` 按「点开菜单→点选项→等触发按钮标签变成目标比例」确认；选中与否没有 data-state，只能看按钮标签
- 上传入口 `button[data-testid="upload_file_button"]`，二级菜单全文「上传文件或图片」
- 仍优先写输入区旁边的隐藏 `input[type=file]`，不点击菜单项本身
- 结果必须在 `[data-testid="mdbox_image"]` 内；`message_image_content` 是用户图。空 SVG / blob 占位不算结果
- 批量洗图复用对话时，发送提示词会列出本批附件的唯一文件名。结果仍须通过源图 URI 关联校验；引用旧批次的结果进入「待核对」，不会按当前源文件名保存
- 发送按钮与千问相同：`chat_input_send_button` 或「发送」
- 验证码只检测。客户端推广「立即下载电脑版」旁的「关闭」只在清理附件时点一次

本地核对锚点：

```sh
node extension/doubao/page_test.js
```
