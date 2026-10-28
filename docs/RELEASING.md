# 发布 tulai 0.1.4

当前桌面端版本是 **0.1.4**，标签 **`v0.1.4`**。页面桥扩展是 **0.3.8**。扩展编号单独走，不要为了桌面端发布去改它。0.1.2 发布时扩展是 0.3.6，0.1.3 发布时是 0.3.7；0.3.8 只改技术 id。

## 0.1.4

技术 id 从 xbs 改成 tulai。Go 模块路径是 `github.com/xxww0098/tulai`，应用 id 是 `dev.tulai.app`。可执行文件、安装路径、发布附件名、`TULAI_*` 环境变量和数据目录 `<UserConfigDir>/tulai` 都用 tulai。页面桥扩展升到 0.3.8，配对地址是 `127.0.0.1:18764/tulai-pair`。

0.1.0 是第一次对外打安装包。0.1.1 带上 GitHub Releases 检查更新、只要提示词的文生图、缩短的开跑步骤，以及应用图标。0.1.2 相对 0.1.1：用户界面名改为「tulai」（技术 id 和可执行文件仍是 xbs）；网页方式去掉 ego lite，改为 Chrome 和 Edge；页面桥扩展加上 popup（扩展版本仍是 0.3.6）。0.1.0 的安装包里还没有「检查更新」，要先手动装上带这个入口的版本。

## 0.1.3 更新说明

相对已发布的 0.1.2。下面按合并进 `main` 的 pull request 写。

- **页面桥令牌（#28）**。浏览器页显示扩展令牌，可以复制，也可以重新生成。以前令牌只写在配置库里，弹出页没有地方可抄。
- **清掉残留的用户可见品牌（#29）**。标题栏、macOS 菜单、扩展弹出页和选项页、命令行帮助里还露着的 xbs 文案改掉。安装路径、可执行文件和发布附件名仍是 xbs。
- **产品名 tulai、更宽的界面、提示词、页面桥更稳（#30）**。窗口标题和菜单上的名字是 tulai。间距加大。内置提示词模板可以另存成自己的提示词。页面桥按心跳重连，界面能看到连接状态。
- **多账号轮换、自动配对、MyGo v0.6.4（#31）**。同一个平台可以挂多个免费账号。额度用尽、请求过于频繁、验证码或未登录时，这个账号冷却或标成已用尽，这一批改走下一个还能用的账号。已经点过发送、但不确定有没有送出的任务不会换号重发。桌面端生成令牌，扩展从本机 `127.0.0.1:18764/xbs-pair` 自己读走；自动对接失败时仍可复制令牌手动保存。构建用的 MyGo 从 v0.6.2 升到 v0.6.4。产品版本不跟工具版本走。
- **自动找浏览器，没有就下载（#32）**。网页方式先用本机已安装的 Chrome、Edge 或 Chromium。都没有时，向导里可以下载 Chrome for Testing。内置浏览器不打进安装包。以前保存的 ego lite 会在打开时改成系统默认浏览器。
- **首次设置向导（#33）**。第一次打开，或还没有可用账号时，先确认浏览器、勾选豆包或千问，并弹出登录页。扫码登录后进入主界面。完成状态写在配置库里。右上角「设置向导」可以再打开，用来多加一个账号。
- **一键出图（#34）**。出图页收成拖图、提示词和「开始出图」。平台、模式、提示词、管理浏览器和开始在同一屏。
- **登录检测和自动恢复（#35）**。掉线、浏览器崩溃或页面卡住时自动恢复，工作台写出提示。账号退出登录会提示重新扫码；在列表里点「恢复选中」后回到轮换。
- **自动更新（#36）**。启动时以及之后大约每 6 小时静默检查，设置里的「自动检查更新」默认开着。有新版本时显示横幅「发现新版本 x.y.z」，可以「立即更新」「稍后」或「跳过此版本」。按当前系统选择安装包，用发布里的 `SHA256SUMS` 校验。GitHub 连不上时再试镜像前缀。出图任务还在跑时不打断，结束后再装。新程序起不来会回到上一版。页面桥扩展不随桌面端更新。

## 三个编号

| 东西 | 当前版本 | 写在哪 | 作用 |
|---|---|---|---|
| 桌面端 | `0.1.4` | `mygo.json` 的 `version`，以及 `cmd/tulai/version.go` 的 `ProductVersion` | 安装包文件名、macOS / Windows 版本信息、`mygo.App.Version()`。两边必须相同，`TestProductVersionMatchesConfig` 会检查。 |
| Go 模块 | `v0.1.4` | git 标签；模块路径是 `github.com/xxww0098/tulai` | 推送标签 `v0.1.4` 之后，模块版本就是这个标签。 |
| 页面桥扩展 | `0.3.8` | `extension/manifest.json`，以及 `extension/protocol.js` 里 hello 的 `payload.version` | Chrome 扩展自己的版本。hello 里的字符串必须和清单一致。 |
| 协议信封 | `1` | `internal/extbridge` 的 `Version`，扩展里的 `TULAI.version` | WebSocket 信封的 `v`。和上面两个产品编号都不是一回事。 |

扩展在桌面端第一次打安装包之前已经迭代到 0.3.4，0.1.2 发布时是 0.3.6，0.1.3 发布时是 0.3.7，当前是 0.3.8。桌面端版本和扩展版本各自走。同一份 Release 里会同时放上桌面安装包和 `tulai-extension-0.3.8.zip`。

## 流水线

`.github/workflows/ci.yml`：

- pull request 和 `main`：跑 `go test ./... -short`、`go tool mygo vet`、扩展的 node 测试，并打出三个系统的安装包。产物在 Actions 的 artifacts 里，保留 14 天：`tulai-linux`、`tulai-windows`、`tulai-macos`、`tulai-extension`。
- 标签 `v*`：同一套构建。标签必须等于 `v` 加上 `mygo.json` 的版本，否则一开始就失败，不会发布。通过之后用 `GITHUB_TOKEN` 创建 GitHub Release，并上传安装包、扩展 zip 和 `SHA256SUMS`。

没有自动更新密钥，也没有 Apple / Windows 签名证书。不要在仓库里造这些 secret。`-upload` 也不用：它要配 `updates` 和签名私钥，装好的程序只接受用那把私钥签过的清单。本仓库的桌面端不走这条签名更新，改由程序自己读公开的 GitHub Releases API。Release 仍由 `gh release create` 上传。

macOS 包在 `macos-latest` 上构建，才能做出磁盘映像并做 ad-hoc 签名。Windows 安装器在 Ubuntu 上用 NSIS 交叉制作。Linux 同时打 amd64 和 arm64。这些都是 `CGO_ENABLED=0` 的 `go tool mygo build`，和本机的 `./build.sh` 同一条命令。

## 切 0.1.4

等这套流水线在 `main` 上是绿的，并且 `mygo.json` 与 `ProductVersion` 都是 `0.1.4`：

```sh
git checkout main
git pull origin main
git tag v0.1.4
git push origin v0.1.4
```

推送标签会跑 CI。完成后 Release 页面出现 **tulai 0.1.4**。安装包文件名以 `tulai` 开头。标签和 `mygo.json` 不一致时，构建不会开始。这个标签在版本号合并进 `main` 之后再推，不放在版本号的 pull request 里。

再跑一次同一个标签的 workflow 会覆盖同名附件，不会另开一个 Release。

## 附件

桌面版本记为 `0.1.4` 时，文件名是：

| 系统 | 文件 | 怎么用 |
|---|---|---|
| macOS | `tulai 0.1.4.dmg` | 通用二进制（Apple 芯片和 Intel）。打开后把 `tulai.app` 拖进「应用程序」。 |
| Windows | `tulai Setup 0.1.4 amd64.exe`、`tulai Setup 0.1.4 arm64.exe` | 当前用户安装，不需要管理员。也有免安装的 `tulai-0.1.4-windows-amd64.exe` 和 `tulai-0.1.4-windows-arm64.exe`。 |
| Linux | `tulai_0.1.4_amd64.deb`、`tulai_0.1.4_arm64.deb` | `sudo dpkg -i`。包依赖 GTK 3 和 glibc，不依赖 WebKitGTK。 |
| Linux | `tulai-0.1.4-linux-amd64.tar.gz`、`tulai-0.1.4-linux-arm64.tar.gz`、`install.sh` | 把脚本和本机架构的压缩包放在同一目录，执行 `sh install.sh`。装到 `~/.local/tulai.app`，并在 `~/.local/bin/tulai` 放命令。 |
| 扩展 | `tulai-extension-0.3.8.zip` | 解压后用 Chrome「加载已解压的扩展程序」选择里面的 `tulai-extension` 目录。 |

`SHA256SUMS` 覆盖上面这些文件，文件名是 `SHA256SUMS`（没有 `.txt`）。格式是 `sha256sum` 的两空格文本行，名字用上传前的原名（磁盘映像和 Windows 安装器带空格）。每个 Actions artifact 里另有一份只覆盖该系统文件的 `SHA256SUMS.txt`，汇总进 Release 时会丢掉，避免和总校验文件撞名。标签任务在上传前检查这份 `SHA256SUMS` 非空，并且下面「从 GitHub 更新」里更新器会选的每个附件都有一行。缺了就不发布。

`dpkg` 把程序装到 `/opt/tulai`，并在 `/usr/bin/tulai` 放命令。配置库位置见 README 的「运行」和「Windows 支持」。

## 签名限制

- **macOS**：ad-hoc 签名，没有公证。本机打出来的包可以在那台 Mac 上跑；拷到别的 Mac，Gatekeeper 会拦。右键打开，或 `xattr -dr com.apple.quarantine /Applications/tulai.app`。要去掉这个限制，需要 Apple Developer ID 和公证配置，这次没有。
- **Windows**：没有证书。SmartScreen 会提示未知发布者。安装器仍然是当前用户安装。
- **Linux**：deb 和压缩包都没有签名。

## 从 GitHub 更新

桌面端自己检查更新，不使用 MyGo 的签名更新（`mygo.Updater` / `updates.publicKey`）。公开仓库的正式版不需要 token，程序也不会读取 `GITHUB_TOKEN` 或 `GH_TOKEN`。

检查地址是：

```text
GET https://api.github.com/repos/xxww0098/tulai/releases/latest
```

`User-Agent` 为 `tulai/<当前版本>`。只接受这个 API 和 `github.com` 上 `xxww0098/tulai` 的发布附件；下载时允许 GitHub 跳到它自己的附件域名。草稿和预发布不算。镜像是在这些地址前面加前缀，所以检查和下载的镜像地址也跟着仓库路径走。

入口：

| 入口 | 行为 |
|---|---|
| 启动后，以及大约每 6 小时 | 静默检查。设置里的「自动检查更新」默认开着。发现新版本时只显示一条横幅，不弹窗。关掉之后不再后台检查。 |
| 横幅「发现新版本 x.y.z」 | 「立即更新」下载、校验、安装并重新打开。「稍后」先关掉这条。「跳过此版本」记在配置库里，这个版本不再提示，更新的版本仍会提示。说明是 Release 正文的前一段。 |
| 任务还在跑 | 不打断生成。点「立即更新」会等到队列空闲再装，横幅写「任务进行中，结束后将自动更新」。 |
| 菜单「帮助 → 检查更新…」、标题栏、设置页、设置向导 | 手动检查。已是最新或失败时才弹窗。设置页和向导显示当前版本。 |
| `tulai update` | 终端里检查并安装。GitHub 连不上时同样走镜像。 |
| `tulai update --check` | 只打印当前版本和最新版本。 |
| `tulai version` | 打印桌面端版本（`ProductVersion`，打包后与 `mygo.App.Version()` 相同）。 |

下载先请求 GitHub。失败、或下完后 SHA256 对不上时，再试镜像。镜像是 ghproxy 那种前缀：`前缀 + 原来的 https 地址`。设置里的「更新镜像」留空时用内置的两个前缀；填 `off` 则只用 GitHub；填自己的前缀时先用它，再试内置的。没下完的文件会接着传，失败则删掉临时文件。

装好之后会先跑一次 `version`。跑不起来就放回上一版。Windows 免安装 exe 由退出后的 PowerShell 替换，新进程几秒内退出就从 `.previous` 拷回来。安装器加 `/S` 静默运行，失败再打开窗口。macOS 换掉 `.app` 并去掉隔离属性。Linux 用户目录安装和 AppImage / 单个二进制在原处替换；deb 不申请管理员权限，改为打开下载好的包，并印出 `sudo dpkg -i`。新进程写了一个启动标记之后，上一版备份才会删掉。

发布附件里的 `SHA256SUMS` 由标签流水线在上传前生成（每个系统的 artifact 里另有 `SHA256SUMS.txt`）。没有这份校验文件就拒绝安装。

版本号只比较桌面端的 `x.y.z`。扩展 zip（现在是 `tulai-extension-0.3.8.zip`）不会被选成桌面安装包，也不会被装进 Chrome。

按当前系统选择附件。GitHub 会把上传名里的空格改成点，`SHA256SUMS` 里仍是原来的带空格名字；两边都认。

| 系统 | 怎么判断 | 下载 | 装到哪里 |
|---|---|---|---|
| Linux，普通或 `~/.local` | 不是下面的 deb 或 AppImage | `tulai-<版本>-linux-<架构>.tar.gz` | `~/.local/tulai.app`，并在 `~/.local/bin/tulai` 放命令。和 `install.sh` 同一位置。 |
| Linux，AppImage | 正在跑的文件名以 `.AppImage` 结尾 | 先找 `tulai-<版本>-linux-<架构>.AppImage`，没有就用上一行的 tar.gz | 这份发布不附带 AppImage，所以走 tar.gz，装到 `~/.local`。 |
| Linux，deb | 正在跑 `/usr/bin/tulai` 或 `/opt/tulai/` | `tulai_<版本>_<架构>.deb` | 不申请管理员权限。校验后把 `sudo dpkg -i …` 印出来，并打开已下载的包。 |
| Windows，安装器或开发运行 | 路径里有 `\Programs\`、`Program Files` 或 `go-build` | `tulai.Setup.<版本>.<架构>.exe`（构建时的名字是 `tulai Setup <版本> <架构>.exe`） | 退出后打开 NSIS 安装器。 |
| Windows，免安装 exe | 其他路径 | `tulai-<版本>-windows-<架构>.exe` | 退出后用 PowerShell 替换当前 exe 再打开。 |
| macOS，正在跑 `.app` | `….app/Contents/MacOS/tulai` | `tulai.<版本>.dmg`（通用二进制；构建时的名字是 `tulai <版本>.dmg`） | 换掉当前的 `tulai.app`，并去掉隔离属性。 |
| macOS，其他 | 开发运行 | 同一个 dmg | 复制到 `~/Applications/tulai.app`。 |

应用内更新和 `tulai update` 装到 `~/.local` 时，桌面项文件名是 `tulai.desktop`，启动命令是 `tulai`。`install.sh`、deb、NSIS 和 macOS 包名跟 `mygo.json` 的 `name` 走，也是 tulai。

下载后核对两处摘要，对不上就删除文件、不安装：

1. 附件 API 里的 `digest`（`sha256:…`），有的话必须和 `SHA256SUMS` 同一行一致。
2. 文件本身的 SHA256 必须等于 `SHA256SUMS` 里的记录。`SHA256SUMS` 自己的 `digest` 也会先核对。

没有 `SHA256SUMS` 就拒绝安装。

### 未签名

校验和只能证明文件和这次 Release 相同，不能代替开发者签名。

- **macOS**：ad-hoc 签名，没有公证。更新会执行 `xattr -dr com.apple.quarantine`。别的 Mac 上 Gatekeeper 仍可能拦截，需要在访达里右键打开。
- **Windows**：没有证书。安装器和替换后的 exe 都可能被 SmartScreen 拦住，需要选择仍要运行。
- **Linux**：deb 和 tar.gz 都没有签名。用户目录安装不需要 root；deb 必须自己执行印出来的 `sudo dpkg -i`。

Release 说明里的「检查更新」一段由 `.github/workflows/ci.yml` 写上。从 0.1.1 起安装包里有这个入口。还在用 0.1.0 的人要先手动装这一版。

## 下一次版本

桌面端要升版本时，同时改 `mygo.json` 的 `version` 和 `ProductVersion`，提交后再打 `vX.Y.Z`。只改一边的话测试过不了；标签对不上的话发布任务不会开始。

扩展继续单独改 `extension/manifest.json` 和 `extension/protocol.js` 的 hello 版本。它可以停在 0.3.x，也可以往后走，不必等于桌面端版本。
