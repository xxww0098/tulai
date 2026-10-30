package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

// agyChooserState 是「添加账号」的两种方式。授权码和凭证内容都不进这里。
type agyChooserState struct {
	hint      string
	home      string
	email     string
	canImport bool
	keychain  bool
	busy      bool
	retry     bool
	err       string
	status    string
}

// agyLoginState 是添加账号时的授权面板。授权码只留在输入框，提交后清掉，不进日志。
type agyLoginState struct {
	id     int64
	home   string
	name   string
	url    string
	code   string
	err    string
	busy   bool
	retry  bool
	copied bool
	sess   *agy.Login
	gen    int
}

func (a *App) agySignature() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.agyBin) + "\n" + strings.TrimSpace(a.agyHomeRoot) + "\n" + strings.TrimSpace(a.agyModel) + "\n" + fmt.Sprintf("%d", a.agyTimeoutSec())
}

func (a *App) agyTimeoutSec() int {
	min := 6.0
	if a != nil && a.agyTimeoutMin > 0 {
		min = a.agyTimeoutMin
	}
	sec := int(min * 60)
	if sec < 60 {
		sec = 60
	}
	if sec > 3600 {
		sec = 3600
	}
	return sec
}

func (a *App) persistAgySettings() {
	if a == nil || a.store == nil {
		return
	}
	sig := a.agySignature()
	if sig == a.agySaved {
		return
	}
	a.agySaved = sig
	bin := strings.TrimSpace(a.agyBin)
	if bin == agy.DefaultBin() {
		bin = ""
	}
	root := strings.TrimSpace(a.agyHomeRoot)
	if root == agy.HomeRoot("", a.store.File()) {
		root = ""
	}
	model := strings.TrimSpace(a.agyModel)
	if model == "" {
		model = agy.DefaultModel
	}
	a.store.Set("agy_bin", bin)
	a.store.Set("agy_home_root", root)
	a.store.Set("agy_model", model)
	a.store.SetInt("agy_timeout_seconds", a.agyTimeoutSec())
}

func (a *App) agySettings(c *ui.Context) {
	ui.Form(c, func() {
		ui.Text(c, "在生图或洗图右侧选中 Antigravity 后，任务只走这里的账号。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "agy 路径", func() {
			ui.TextInput(c, &a.agyBin).Placeholder(agy.DefaultBin()).Disabled(a.running)
		})
		ui.Field(c, "账号目录", func() {
			ui.TextInput(c, &a.agyHomeRoot).Placeholder("每个子目录是一个账号").Disabled(a.running)
		})
		ui.Field(c, "模型", func() {
			ui.TextInput(c, &a.agyModel).Placeholder(agy.DefaultModel).Disabled(a.running)
		})
		ui.Field(c, "单任务超时(分钟)", func() {
			ui.NumberInput(c, &a.agyTimeoutMin, 1, 60, 1).Disabled(a.running)
		})
	}).FillWidth().Gap(8)
	a.persistAgySettings()
}

func (a *App) attachAgyLanes(board tasks.Board) tasks.Board {
	if a == nil || len(board.Lanes) == 0 {
		return board
	}
	byID := map[int64]store.Browser{}
	for _, b := range a.configuredBrowsers() {
		byID[b.ID] = b
	}
	for i := range board.Lanes {
		ln := &board.Lanes[i]
		b, ok := byID[ln.ID]
		if !ok || b.Platform != agy.Platform {
			continue
		}
		if strings.TrimSpace(b.UserDataDir) != "" {
			ln.Email = agy.Email(b.UserDataDir)
		}
		ln.CoolUntil = b.CoolUntil
		if strings.TrimSpace(b.UserDataDir) != "" && !b.IsLoggedIn && ln.State != tasks.LaneRunning {
			ln.State = tasks.LaneLogin
			ln.Action = "去登录"
		}
	}
	return board
}

func (a *App) agyPauseLine(now time.Time) string {
	if a == nil || a.stagePlatform() != agy.Platform {
		return ""
	}
	var rows []store.Browser
	for _, b := range a.configuredBrowsers() {
		if b.Platform == agy.Platform {
			rows = append(rows, b)
		}
	}
	until, ok := tasksAgyPaused(rows, now, a.shownDailyCap())
	if !ok {
		return ""
	}
	return agy.PauseMessage(until, now)
}

func tasksAgyPaused(rows []store.Browser, now time.Time, cap int) (time.Time, bool) {
	return pausedAgy(rows, now, cap)
}

func pausedAgy(rows []store.Browser, now time.Time, cap int) (time.Time, bool) {
	if len(rows) == 0 {
		return time.Time{}, false
	}
	var until time.Time
	saw := false
	for _, b := range rows {
		if !b.IsLoggedIn {
			return time.Time{}, false
		}
		if b.Usable(now) && (cap <= 0 || b.UsedToday(now) < cap) {
			return time.Time{}, false
		}
		msg := strings.ToLower(b.LastError)
		if !strings.Contains(b.LastError, "额度") && !strings.Contains(msg, "quota") {
			return time.Time{}, false
		}
		saw = true
		if b.CoolUntil <= 0 {
			continue
		}
		t := time.Unix(b.CoolUntil, 0)
		if until.IsZero() || t.Before(until) {
			until = t
		}
	}
	return until, saw
}

func (a *App) agyChooserPanel(c *ui.Context) {
	if a == nil || a.agyChooser == nil || a.agyLogin != nil || a.stagePlatform() != agy.Platform {
		return
	}
	s := a.agyChooser
	tok := a.look
	ui.Column(c.Key("agy-add")).Label("agy-add-chooser").FillWidth().Gap(8).Padding(10, 12).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border).Children(func() {
		a.agyMethodRow(c, "Google 登录（OAuth）", "在这个窗口里粘贴授权码，不会写入日志", "登录", "agy-oauth-go", s.busy, a.startAgyOAuth)
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, "导入本地 Antigravity CLI 账号").FontSize(tok.body).Bold().SingleLine().MinWidth(0)
				hint := s.hint
				if hint == "" {
					hint = agy.MsgNotFound
				}
				ui.Text(c, hint).FontSize(tok.caption).TextColor(tok.muted).SingleLine().MinWidth(0).Tooltip(hint)
			})
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Button(c, "选择文件夹…").Label("agy-pick-folder").Shrink(0).Height(36).AlignSelf(ui.Center).
					Disabled(s.busy || a.running).OnClick(func() { a.pickAgyFolder() })
				ui.Button(c, "导入").Label("agy-import-go").Shrink(0).Height(36).AlignSelf(ui.Center).
					Disabled(s.busy || a.running || !s.canImport).OnClick(func() { a.startAgyImport() })
			})
		})
		if s.busy {
			ui.Text(c, agy.MsgVerifying).FontSize(tok.caption).TextColor(tok.muted)
		}
		if s.status != "" && !s.busy {
			ui.Text(c, s.status).FontSize(tok.caption).TextColor(tok.success)
		}
		if s.err != "" {
			ui.Text(c, s.err).FontSize(tok.caption).TextColor(tok.danger)
		}
		if s.retry && !s.busy {
			ui.Row(c).Children(func() {
				ui.Button(c, "重试").Shrink(0).Height(36).AlignSelf(ui.Center).Disabled(a.running).OnClick(func() { a.startAgyImport() })
			})
		}
	})
}

func (a *App) agyMethodRow(c *ui.Context, title, hint, button, label string, busy bool, click func()) {
	tok := a.look
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(tok.body).Bold().SingleLine().MinWidth(0)
			ui.Text(c, hint).FontSize(tok.caption).TextColor(tok.muted).SingleLine().MinWidth(0).Tooltip(hint)
		})
		ui.Button(c, button).Label(label).Shrink(0).Height(36).AlignSelf(ui.Center).
			Disabled(busy || a.running).OnClick(click)
	})
}

func (a *App) agyLoginPanel(c *ui.Context) {
	if a == nil || a.agyLogin == nil || a.stagePlatform() != agy.Platform {
		return
	}
	s := a.agyLogin
	tok := a.look
	ui.Column(c.Key("agy-login")).FillWidth().Gap(8).Padding(10, 12).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border).Children(func() {
		ui.Text(c, "登录 Antigravity").FontSize(tok.body).Bold()
		ui.Text(c, "把浏览器给出的授权码粘贴到下面，不会写入日志。").FontSize(tok.caption).TextColor(tok.muted)
		ui.Field(c, "授权链接", func() {
			url := s.url
			if url == "" {
				url = "正在获取授权链接…"
			}
			link := ui.Text(c.Key("agy-url"), url).FontSize(tok.caption).TextColor(tok.accent).SingleLine().Tooltip(url).Cursor(ui.CursorPointer)
			if s.url != "" && link.Clicked() {
				c.OpenURL(s.url)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, "打开浏览器").Height(36).Disabled(s.url == "").OnClick(func() {
					if s.url != "" {
						c.OpenURL(s.url)
					}
				})
				ui.Button(c, "复制链接").Height(36).Disabled(s.url == "").OnClick(func() {
					if s.url == "" {
						return
					}
					c.Services().WriteClipboard(s.url)
					s.copied = true
				})
			})
		})
		if s.copied {
			ui.Text(c, "已复制链接").FontSize(tok.caption).TextColor(tok.muted)
		}
		ui.Field(c, "授权码", func() {
			ui.TextInput(c.Key("agy-code"), &s.code).Disabled(s.busy)
		})
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, "提交").Height(36).Disabled(s.busy || strings.TrimSpace(s.code) == "").OnClick(func() {
				a.submitAgyCode()
			})
			if s.retry {
				ui.Button(c, "重试").Height(36).Disabled(a.running).OnClick(func() { a.retryAgyLogin() })
			}
		})
		if s.err != "" {
			ui.Text(c, s.err).FontSize(tok.caption).TextColor(tok.danger)
		}
	})
}

func (a *App) submitAgyCode() {
	if a == nil || a.agyLogin == nil {
		return
	}
	s := a.agyLogin
	code := strings.TrimSpace(s.code)
	s.code = ""
	if code == "" {
		s.err = "请先粘贴授权码"
		return
	}
	if s.sess == nil {
		s.err = "登录还没开始，请点重试"
		s.retry = true
		return
	}
	if err := s.sess.Submit(code); err != nil {
		s.err = err.Error()
		s.retry = true
		return
	}
	s.busy = true
	s.err = ""
}

func (a *App) addAgyAccount() {
	if a == nil || a.running {
		return
	}
	if a.agyLogin != nil && a.agyLogin.sess != nil && !a.agyLogin.retry {
		logging.Log.Info("Antigravity 正在登录，请先完成或点重试")
		return
	}
	if a.agyChooser != nil && a.agyChooser.busy {
		return
	}
	if a.agyChooser != nil {
		a.agyChooser = nil
		return
	}
	found := agy.DetectLocal()
	a.agyChooser = &agyChooserState{
		hint:      found.Hint,
		home:      found.Home,
		email:     found.Email,
		canImport: found.CanImport(),
		keychain:  found.Keychain,
	}
}

func (a *App) startAgyOAuth() {
	if a == nil || a.running {
		return
	}
	if a.agyChooser != nil && a.agyChooser.busy {
		return
	}
	if a.store == nil {
		logging.Log.Errorf("还没有配置库，无法添加 Antigravity 账号")
		return
	}
	root := a.agyAccountRoot()
	dir, name, err := agy.NextAccountDir(root)
	if err != nil {
		logging.Log.Errorf("创建 Antigravity 账号目录失败: %v", err)
		return
	}
	row, err := a.store.AddBrowser(agy.Platform, name, dir)
	if err != nil {
		logging.Log.Errorf("添加 Antigravity 账号失败: %v", err)
		_ = os.RemoveAll(dir)
		return
	}
	a.agyChooser = nil
	a.refreshBrowsers()
	a.beginAgyLogin(row)
}

func (a *App) agyAccountRoot() string {
	root := ""
	if a != nil {
		root = strings.TrimSpace(a.agyHomeRoot)
	}
	if root == "" && a != nil && a.store != nil {
		root = agy.HomeRoot("", a.store.File())
	}
	if root == "" {
		root = "agy-homes"
	}
	return root
}

func (a *App) agyEmails() []string {
	if a == nil {
		return nil
	}
	var out []string
	for _, b := range a.configuredBrowsers() {
		if b.Platform != agy.Platform || strings.TrimSpace(b.UserDataDir) == "" {
			continue
		}
		if email := agy.Email(b.UserDataDir); email != "" {
			out = append(out, email)
		}
	}
	return out
}

func (a *App) agySourceBlocked(home string) bool {
	if agy.WithinAccountRoot(home, a.agyAccountRoot()) {
		return true
	}
	return agy.EmailUsed(agy.Email(home), a.agyEmails())
}

func (a *App) pickAgyFolder() {
	if a == nil || a.running || a.agyChooser == nil || a.agyChooser.busy {
		return
	}
	window := a.window
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: window, Title: "选择 Antigravity 目录", Directory: true,
		})
		if err != nil {
			logging.Log.Errorf("选择文件夹失败: %v", err)
			return
		}
		if len(paths) == 0 {
			return
		}
		home, nerr := agy.NormalizeHome(paths[0])
		apply := func() {
			if a.agyChooser == nil || a.agyChooser.busy {
				return
			}
			if nerr != nil {
				a.agyChooser.err = nerr.Error()
				return
			}
			if a.agySourceBlocked(home) {
				a.agyChooser.err = agy.ErrAlreadyImported.Error()
				return
			}
			email := agy.Email(home)
			a.agyChooser.home = home
			a.agyChooser.email = email
			a.agyChooser.canImport = true
			a.agyChooser.keychain = false
			a.agyChooser.err = ""
			a.agyChooser.retry = false
			if email != "" {
				a.agyChooser.hint = "已检测到 " + email
			} else {
				a.agyChooser.hint = "已检测到本机登录"
			}
		}
		if window != nil {
			window.Update(apply)
			return
		}
		apply()
	}()
}

func (a *App) startAgyImport() {
	if a == nil || a.running || a.agyChooser == nil || a.agyChooser.busy {
		return
	}
	s := a.agyChooser
	if !s.canImport || strings.TrimSpace(s.home) == "" {
		s.err = agy.MsgNotFound
		s.status = ""
		return
	}
	if a.store == nil {
		logging.Log.Errorf("还没有配置库，无法添加 Antigravity 账号")
		return
	}
	if a.agySourceBlocked(s.home) {
		s.err = agy.ErrAlreadyImported.Error()
		s.retry = false
		return
	}
	s.busy = true
	s.err = ""
	s.status = ""
	s.retry = false
	req := agy.ImportRequest{
		SourceHome:        s.home,
		AccountRoot:       a.agyAccountRoot(),
		ExistingEmails:    a.agyEmails(),
		Bin:               strings.TrimSpace(a.agyBin),
		Model:             strings.TrimSpace(a.agyModel),
		Timeout:           agy.VerifyTimeout,
		AllowMissingToken: s.keychain && !agy.LoggedIn(s.home),
	}
	window := a.window
	go func() {
		res, err := agy.Import(context.Background(), req)
		apply := func() { a.finishAgyImport(res, err) }
		if window != nil {
			window.Update(apply)
			return
		}
		apply()
	}()
}

func (a *App) finishAgyImport(res agy.ImportResult, err error) {
	if a == nil {
		if err == nil && res.Home != "" {
			_ = os.RemoveAll(res.Home)
		}
		return
	}
	if err != nil {
		if a.agyChooser != nil {
			a.agyChooser.busy = false
			a.agyChooser.retry = true
			a.agyChooser.status = ""
			a.agyChooser.err = err.Error()
		}
		return
	}
	if a.store == nil {
		_ = os.RemoveAll(res.Home)
		if a.agyChooser != nil {
			a.agyChooser.busy = false
			a.agyChooser.retry = true
			a.agyChooser.err = "还没有配置库，无法添加 Antigravity 账号"
		}
		return
	}
	row, addErr := a.store.AddBrowser(agy.Platform, res.Name, res.Home)
	if addErr != nil {
		_ = os.RemoveAll(res.Home)
		logging.Log.Errorf("添加 Antigravity 账号失败: %v", addErr)
		if a.agyChooser != nil {
			a.agyChooser.busy = false
			a.agyChooser.retry = true
			a.agyChooser.err = "添加 Antigravity 账号失败"
		}
		return
	}
	a.store.UpdateBrowser(row.ID, false, true)
	a.store.ResetBrowserHealth(row.ID)
	a.refreshBrowsers()
	who := strings.TrimSpace(res.Email)
	line := agy.MsgImported
	if who != "" {
		line = who + " " + agy.MsgImported
	}
	logging.Log.Success("%s", line)
	if a.agyChooser != nil {
		a.agyChooser.busy = false
		a.agyChooser.retry = false
		a.agyChooser.err = ""
		a.agyChooser.canImport = false
		a.agyChooser.status = line
	}
}

func (a *App) beginAgyLogin(row store.Browser) {
	if a == nil || row.ID <= 0 {
		return
	}
	if a.agyLogin != nil && a.agyLogin.sess != nil {
		a.agyLogin.sess.Close()
	}
	gen := 1
	if a.agyLogin != nil {
		gen = a.agyLogin.gen + 1
	}
	state := &agyLoginState{
		id:   row.ID,
		home: row.UserDataDir,
		name: row.Name,
		gen:  gen,
	}
	a.agyLogin = state
	bin := strings.TrimSpace(a.agyBin)
	model := strings.TrimSpace(a.agyModel)
	sess, err := agy.StartLogin(bin, row.UserDataDir, model)
	if err != nil {
		state.err = err.Error()
		state.retry = true
		return
	}
	state.sess = sess
	go a.watchAgyLogin(gen, sess, row.ID, row.Name, row.UserDataDir)
}

func (a *App) watchAgyLogin(gen int, sess *agy.Login, id int64, name, home string) {
	if sess == nil {
		return
	}
	deadline := time.Now().Add(agy.LoginTimeout)
	for time.Now().Before(deadline) {
		url := sess.URL()
		if url != "" {
			a.syncUI(func() {
				if a.agyLogin != nil && a.agyLogin.gen == gen && a.agyLogin.url == "" {
					a.agyLogin.url = url
				}
			})
			break
		}
		select {
		case <-sess.Done():
			deadline = time.Time{}
		case <-time.After(80 * time.Millisecond):
		}
		if deadline.IsZero() {
			break
		}
	}
	err := sess.Wait()
	ok := agy.LoggedIn(home)
	a.syncUI(func() {
		if a.agyLogin == nil || a.agyLogin.gen != gen {
			return
		}
		if ok {
			a.finishAgyLogin(id, name)
			return
		}
		a.agyLogin.busy = false
		a.agyLogin.retry = true
		if err != nil {
			a.agyLogin.err = err.Error()
		} else {
			a.agyLogin.err = "登录未完成，请点重试"
		}
	})
}

func (a *App) retryAgyLogin() {
	if a == nil || a.agyLogin == nil || a.running {
		return
	}
	s := a.agyLogin
	row := store.Browser{ID: s.id, Name: s.name, UserDataDir: s.home, Platform: agy.Platform}
	a.beginAgyLogin(row)
}

func (a *App) finishAgyLogin(id int64, name string) {
	if a != nil && a.store != nil && id > 0 {
		a.store.UpdateBrowser(id, false, true)
		a.store.ResetBrowserHealth(id)
		a.refreshBrowsers()
	}
	if name == "" {
		name = "账号"
	}
	logging.Log.Success("Antigravity 账号「%s」已登录", name)
	if a != nil {
		a.agyLogin = nil
	}
}

func (a *App) openAgyLane(row store.Browser) {
	if strings.TrimSpace(row.UserDataDir) == "" {
		a.addAgyAccount()
		return
	}
	a.beginAgyLogin(row)
}
