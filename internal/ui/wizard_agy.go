package ui

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/wizard"
)

// wizardAgyState 是向导里 Antigravity 这条路的状态。
type wizardAgyState struct {
	path    string // 检测到的 agy，或者最先检查的位置
	found   bool
	copied  bool  // 安装命令已经复制
	watch   bool  // 后台正在等用户装好
	created int64 // 登录步骤里这一轮新建的账号。没登录就离开时清掉
}

// selectedPlatforms 是勾选的平台，按豆包、千问、Antigravity 排。
func (a *App) selectedPlatforms() []string {
	var out []string
	if a.platDoubao {
		out = append(out, "豆包")
	}
	if a.platQianwen {
		out = append(out, "千问")
	}
	if a.platAntigravity {
		out = append(out, agy.Platform)
	}
	return out
}

// wizardApplyChoice 把第一步选的平台写进勾选。第一次设置只留这一个，
// 以后重开向导只是再加一个平台，已有的勾选不动。
func (a *App) wizardApplyChoice() {
	if !wizard.Valid(a.wizardChoice) {
		return
	}
	if a.wizardFirst {
		a.platDoubao, a.platQianwen, a.platAntigravity = false, false, false
	}
	switch a.wizardChoice {
	case "豆包":
		a.platDoubao = true
	case "千问":
		a.platQianwen = true
	case agy.Platform:
		a.platAntigravity = true
	}
}

// stageIndex 是平台在出图页右栏页签里的序号。
func stageIndex(platform string) int {
	switch platform {
	case "千问":
		return 1
	case agy.Platform:
		return 2
	default:
		return 0
	}
}

// wizardLandOnChoice 让出图页和洗图页进来就停在刚设置的平台上。
// 否则右栏停在豆包，Antigravity 用户会先看到「还没有可用账号」。
func (a *App) wizardLandOnChoice() {
	if !wizard.Valid(a.wizardChoice) {
		return
	}
	idx := stageIndex(a.wizardChoice)
	for _, page := range []int{pageHome, pageWash} {
		a.onPage(page, func() {
			a.stageTab = idx
			a.stagePicked = true
		})
	}
}

func (a *App) locateAgy() (string, bool) {
	if a.agyLocate != nil {
		return a.agyLocate(a.agyBin)
	}
	return agy.Locate(a.agyBin)
}

// checkAgyInstall 看本机装好了没有。找到的位置和设置里的不同时改用找到的：
// 登录和出图都只认设置里的路径，不会再去 PATH 里找。
func (a *App) checkAgyInstall() {
	path, ok := a.locateAgy()
	a.wizardAgy.path = path
	a.wizardAgy.found = ok
	if ok && strings.TrimSpace(a.agyBin) != path {
		a.agyBin = path
	}
}

func (a *App) enterInstallStep() {
	a.wizardAgy.copied = false
	a.checkAgyInstall()
	a.watchAgyInstall()
}

// watchAgyInstall 在用户去终端装的时候每两秒看一次，装好就刷新界面。
// 离开安装步骤、向导关掉或者找到了就停。没有窗口（测试）时不起后台任务。
func (a *App) watchAgyInstall() {
	if a.window == nil || a.wizardAgy.found || a.wizardAgy.watch {
		return
	}
	a.wizardAgy.watch = true
	go func() {
		defer a.wizardUI(func() { a.wizardAgy.watch = false })
		for range 900 {
			time.Sleep(2 * time.Second)
			stop := false
			a.wizardUI(func() {
				if !a.wizardOn || a.wizardStep != wizard.StepInstall {
					stop = true
					return
				}
				a.checkAgyInstall()
				stop = a.wizardAgy.found
			})
			if stop {
				return
			}
		}
	}()
}

// pickAgyBinary 给装在别处的人：选一个 agy 可执行文件，之后出图和登录都用它。
func (a *App) pickAgyBinary() {
	window := a.window
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: "选择 agy 可执行文件"})
		if err != nil {
			logging.Log.Errorf("选择 agy 失败: %v", err)
			return
		}
		if len(paths) == 0 {
			return
		}
		apply := func() {
			a.agyBin = paths[0]
			a.checkAgyInstall()
		}
		if window != nil {
			window.Update(apply)
			return
		}
		apply()
	}()
}

// wizardEnterAgyAccount 进入 Antigravity 的登录步骤：给出 Google 登录和导入本机账号两种方式。
// 已经有能用的 Antigravity 账号、又不是要再加一个时，直接到完成页。
func (a *App) wizardEnterAgyAccount(extra bool) {
	a.stopWizardLoop()
	a.wizardExtra = extra
	a.wizardErr = ""
	a.wizardAgy.created = 0
	if !extra && !a.platformNeedsLogin(agy.Platform) {
		a.enterDoneStep()
		return
	}
	a.closeAgyLogin()
	a.agyChooser = newAgyChooser()
	a.goWizard(wizard.StepAccount)
}

func (a *App) closeAgyLogin() {
	if a.agyLogin != nil && a.agyLogin.sess != nil {
		a.agyLogin.sess.Close()
	}
	a.agyLogin = nil
}

// agyAccountAdded 在 Antigravity 账号登录或导入成功后调用。
// 向导的登录步骤里，这就是这一步的终点。
func (a *App) agyAccountAdded() {
	if a == nil || !a.wizardOn || a.wizardChoice != agy.Platform || a.wizardStep != wizard.StepAccount {
		return
	}
	a.wizardAgy.created = 0
	a.enterDoneStep()
}

// abandonAgyAccount 在登录步骤点上一步时调用：关掉授权，
// 并删掉这一轮新建、还没登录成功的账号和它的目录。已经登录的不动。
func (a *App) abandonAgyAccount() {
	a.closeAgyLogin()
	a.agyChooser = nil
	id := a.wizardAgy.created
	a.wizardAgy.created = 0
	if id <= 0 || a.store == nil {
		return
	}
	for _, row := range a.store.Browsers() {
		if row.ID != id {
			continue
		}
		if row.IsLoggedIn || agy.LoggedIn(row.UserDataDir) {
			return
		}
		a.store.DeleteBrowser(row.ID)
		if dir, root := row.UserDataDir, a.agyAccountRoot(); dir != "" && agy.WithinAccountRoot(dir, root) && !agy.WithinAccountRoot(root, dir) {
			if err := os.RemoveAll(dir); err != nil {
				logging.Log.Warning("删除未登录的 Antigravity 账号目录失败：%v", err)
			}
		}
		a.refreshBrowsers()
		return
	}
}

func (a *App) wizardIsAgy() bool { return a.wizardChoice == agy.Platform }

// wizardNeedsBrowser 为真时才用得着浏览器：只有豆包和千问在网页里登录。
func (a *App) wizardNeedsBrowser() bool { return !a.wizardIsAgy() }

// wizardNormalizeChoice 保证选中的是三个平台之一。没选过时取已勾选的第一个，再不行是豆包。
func (a *App) wizardNormalizeChoice() {
	if !wizard.Valid(a.wizardChoice) {
		a.wizardChoice = wizard.DefaultChoice(a.selectedPlatforms())
	}
}

func wizardBlurb(platform string) string {
	if platform == agy.Platform {
		return "用 Google 账号登录命令行 agy 出图。要先安装，不用浏览器。"
	}
	return "在" + platform + "里生图。在打开的网页里登录一个免费账号。"
}

// wizardInstallStep 引导安装 Antigravity 命令行：给官方命令、一键复制、官方下载页，
// 装在别处的可以手动选文件。装好后自动识别，下一步才亮。
func (a *App) wizardInstallStep(c *ui.Context) {
	tok := a.look
	st := a.wizardAgy
	title, detail, pill, pillColor := "还没有找到 agy", "装好后这里会自动识别，不用重启 tulai。", "未安装", tok.warning
	if st.found {
		title, detail, pill, pillColor = "已找到 agy", st.path, "已就绪", tok.success
	}
	wizardCard(ui.Row(c.Key("agy-card")).FillWidth().Gap(16).AlignItems(ui.Center).Padding(20), tok).Children(func() {
		wizardTile(c, "agy-tile", iconTerminal, 48, tok)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(18).Bold()
			ui.Text(c, detail).FontSize(13).TextColor(tok.muted).SingleLine().Tooltip(detail)
		})
		wizardPill(c, "agy-pill", pill, pillColor)
	})
	if st.found {
		return
	}
	cmd := agy.InstallCommand(runtime.GOOS)
	shell := agy.InstallShell(runtime.GOOS)
	wizardCard(ui.Column(c.Key("agy-install")).FillWidth().Gap(12).Padding(16, 20), tok).Children(func() {
		ui.Text(c, "在"+shell+"里运行这一行").FontSize(15).Bold()
		ui.Row(c.Key("agy-cmd")).FillWidth().Gap(10).AlignItems(ui.Center).Padding(10, 14).Radius(tok.radiusM).Background(tok.sunken).Children(func() {
			ui.Icon(c, iconTerminal).FontSize(16).TextColor(tok.muted).Shrink(0)
			ui.Text(c, cmd).Font("monospace").FontSize(13).Selectable().Grow(1).MinWidth(0)
		})
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Button(c, "复制命令").OnClick(func() {
				c.Services().WriteClipboard(cmd)
				a.wizardAgy.copied = true
			})
			if st.copied {
				ui.Text(c, "已复制，粘贴到"+shell+"后回车").FontSize(tok.caption).TextColor(tok.muted)
			}
		})
	})
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Button(c, "打开官方下载页").OnClick(func() { c.OpenURL(agy.InstallPage) })
		ui.Button(c, "我装在别处…").OnClick(func() { a.pickAgyBinary() })
		ui.Button(c, "重新检测").OnClick(func() { a.checkAgyInstall() })
	})
	ui.Text(c, "官方脚本把 agy 装到 "+agy.DefaultBin()+"。").FontSize(tok.caption).TextColor(tok.muted).Selectable()
}

// wizardAgyAccountStep 是 Antigravity 的登录步骤：Google 授权，或者导入本机已登录的账号。
func (a *App) wizardAgyAccountStep(c *ui.Context) {
	tok := a.look
	switch {
	case a.agyLogin != nil:
		a.agyLoginCard(c, true)
	case a.agyChooser != nil:
		a.agyChooserCard(c, true)
	default:
		ui.Button(c, "重新选择登录方式").OnClick(func() { a.wizardEnterAgyAccount(a.wizardExtra) })
	}
	if runtime.GOOS == "windows" {
		ui.Text(c, "Windows 上暂时不能在这里直接授权。先在 PowerShell 里运行 agy 登录一次，再选「导入」。").
			FontSize(tok.caption).TextColor(tok.muted)
	}
}
