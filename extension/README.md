# tulai 内置页面脚本（Fingerprint）

这个目录存放嵌入桌面端二进制（`extension/bundle.go`）并由项目托管的 **Fingerprint**（`fingerprint-chromium`）通过私有 CDP 管道与隔离世界注入的页面脚本：

- `refuse.js`：豆包与千问共用的审核拒绝话术识别。
- `doubao/`：豆包页面图像模式切换、比例设置、响应原图提取与多图洗图源图关联脚本（`page.js`、`raw.js`、`source_map.js`）。
- `download/`：网页图片下载专用 Fingerprint 环境的隔离世界下载助手脚本（`content.js`、`temu.js`）。

千问、豆包的登录与生图/洗图/裂变以及网页下载专用环境均统一使用受管 Fingerprint 指纹浏览器，不再使用外部浏览器扩展或本地扩展桥。浏览器使用约束详见 [AGENTS.md](../AGENTS.md) 与 [docs/FINGERPRINT.md](../docs/FINGERPRINT.md)。
