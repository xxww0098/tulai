# 发布 xbs 0.1.0

第一次对外打包的桌面端版本是 **0.1.0**，标签 **`v0.1.0`**。页面桥扩展已经是 **0.3.4**，不要为了这次发布把它改回 0.1.0。

## 三个编号

| 东西 | 当前版本 | 写在哪 | 作用 |
|---|---|---|---|
| 桌面端 | `0.1.0` | `mygo.json` 的 `version`，以及 `cmd/xbs/version.go` 的 `ProductVersion` | 安装包文件名、macOS / Windows 版本信息、`mygo.App.Version()`。两边必须相同，`TestProductVersionMatchesConfig` 会检查。 |
| Go 模块 | `v0.1.0` | git 标签，不写在 `go.mod` | 推送标签 `v0.1.0` 之后，模块版本就是这个标签。 |
| 页面桥扩展 | `0.3.4` | `extension/manifest.json`，以及 `extension/protocol.js` 里 hello 的 `payload.version` | Chrome 扩展自己的版本。hello 里的字符串必须和清单一致。 |
| 协议信封 | `1` | `internal/extbridge` 的 `Version`，扩展里的 `XBS.version` | WebSocket 信封的 `v`。和上面两个产品编号都不是一回事。 |

扩展在桌面端第一次打安装包之前已经迭代到 0.3.4。0.1.0 只表示桌面端这次开始发安装包。同一份 Release 里会同时放上桌面安装包和 `xbs-extension-0.3.4.zip`。

## 流水线

`.github/workflows/ci.yml`：

- pull request 和 `main`：跑 `go test ./... -short`、`go tool mygo vet`、扩展的 node 测试，并打出三个系统的安装包。产物在 Actions 的 artifacts 里，保留 14 天：`xbs-linux`、`xbs-windows`、`xbs-macos`、`xbs-extension`。
- 标签 `v*`：同一套构建。标签必须等于 `v` 加上 `mygo.json` 的版本，否则一开始就失败，不会发布。通过之后用 `GITHUB_TOKEN` 创建 GitHub Release，并上传安装包、扩展 zip 和 `SHA256SUMS`。

没有自动更新密钥，也没有 Apple / Windows 签名证书。不要在仓库里造这些 secret。`-upload` 也不用：它要配 `updates` 和签名私钥。Release 由 `gh release create` 上传。

macOS 包在 `macos-latest` 上构建，才能做出磁盘映像并做 ad-hoc 签名。Windows 安装器在 Ubuntu 上用 NSIS 交叉制作。Linux 同时打 amd64 和 arm64。这些都是 `CGO_ENABLED=0` 的 `go tool mygo build`，和本机的 `./build.sh` 同一条命令。

## 切 0.1.0

等这套流水线在 `main` 上是绿的，并且 `mygo.json` 与 `ProductVersion` 都是 `0.1.0`：

```sh
git checkout main
git pull origin main
git tag v0.1.0
git push origin v0.1.0
```

推送标签会跑 CI。完成后 Release 页面出现 **xbs 0.1.0**。标签和 `mygo.json` 不一致时，构建不会开始。

再跑一次同一个标签的 workflow 会覆盖同名附件，不会另开一个 Release。

## 附件

桌面版本记为 `0.1.0` 时，文件名是：

| 系统 | 文件 | 怎么用 |
|---|---|---|
| macOS | `xbs 0.1.0.dmg` | 通用二进制（Apple 芯片和 Intel）。打开后把 `xbs.app` 拖进「应用程序」。 |
| Windows | `xbs Setup 0.1.0 amd64.exe`、`xbs Setup 0.1.0 arm64.exe` | 当前用户安装，不需要管理员。也有免安装的 `xbs-0.1.0-windows-amd64.exe` 和 `xbs-0.1.0-windows-arm64.exe`。 |
| Linux | `xbs_0.1.0_amd64.deb`、`xbs_0.1.0_arm64.deb` | `sudo dpkg -i`。包依赖 GTK 3 和 glibc，不依赖 WebKitGTK。 |
| Linux | `xbs-0.1.0-linux-amd64.tar.gz`、`xbs-0.1.0-linux-arm64.tar.gz`、`install.sh` | 把脚本和本机架构的压缩包放在同一目录，执行 `sh install.sh`。装到 `~/.local/xbs.app`，并在 `~/.local/bin/xbs` 放命令。 |
| 扩展 | `xbs-extension-0.3.4.zip` | 解压后用 Chrome「加载已解压的扩展程序」选择里面的 `xbs-extension` 目录。 |

`SHA256SUMS` 覆盖上面这些文件。每个 Actions artifact 里另有一份只覆盖该系统文件的 `SHA256SUMS.txt`。

`dpkg` 把程序装到 `/opt/xbs`，并在 `/usr/bin/xbs` 放命令。配置库位置见 README 的「运行」和「Windows 支持」。

## 签名限制

- **macOS**：ad-hoc 签名，没有公证。本机打出来的包可以在那台 Mac 上跑；拷到别的 Mac，Gatekeeper 会拦。右键打开，或 `xattr -dr com.apple.quarantine /Applications/xbs.app`。要去掉这个限制，需要 Apple Developer ID 和公证配置，这次没有。
- **Windows**：没有证书。SmartScreen 会提示未知发布者。安装器仍然是当前用户安装。
- **Linux**：deb 和压缩包都没有签名。

## 下一次版本

桌面端要升版本时，同时改 `mygo.json` 的 `version` 和 `ProductVersion`，提交后再打 `vX.Y.Z`。只改一边的话测试过不了；标签对不上的话发布任务不会开始。

扩展继续单独改 `extension/manifest.json` 和 `extension/protocol.js` 的 hello 版本。它可以停在 0.3.x，也可以往后走，不必等于桌面端版本。
