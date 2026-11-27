package ui

import (
	"context"
	"strings"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/tasks"
)

// SettingsService exposes application settings and preferences IPCs.
type SettingsService struct {
	app *App
}

// SettingsStateDTO represents all configurable application preferences.
type SettingsStateDTO struct {
	Tab                int    `json:"tab"` // 0: 常规与输出 1: 账号与任务 2: 网络与高级 3: 关于与更新
	OutputPath         string `json:"outputPath"`
	ResolvedOutput     string `json:"resolvedOutput"`
	GenAutoNumber      bool   `json:"genAutoNumber"`
	WashAutoNumber     bool   `json:"washAutoNumber"`
	NumPrefix          string `json:"numPrefix"`
	NumStart           int    `json:"numStart"`
	NumWidth           int    `json:"numWidth"`
	DlDir              string `json:"dlDir"`
	ResolvedDlDir      string `json:"resolvedDlDir"`
	DownloadConc       int    `json:"downloadConc"`
	DownloadRetries    int    `json:"downloadRetries"`
	DownloadRateKB     int    `json:"downloadRateKb"`
	RemoveWatermark    bool   `json:"removeWatermark"`
	AutoDeleteHistory  bool   `json:"autoDeleteHistory"`
	ConcurrencyAuto         bool   `json:"concurrencyAuto"`
	ConcurrencyMax          int    `json:"concurrencyMax"`
	ConcurrencyAutoQianwen  bool   `json:"concurrencyAutoQianwen"`
	ConcurrencyMaxQianwen   int    `json:"concurrencyMaxQianwen"`
	ConcurrencyAutoQwenIntl bool   `json:"concurrencyAutoQwenIntl"`
	ConcurrencyMaxQwenIntl  int    `json:"concurrencyMaxQwenIntl"`
	ConcurrencyAutoGemini   bool   `json:"concurrencyAutoGemini"`
	ConcurrencyMaxGemini    int    `json:"concurrencyMaxGemini"`
	ConcurrencyAutoJimeng   bool   `json:"concurrencyAutoJimeng"`
	ConcurrencyMaxJimeng    int    `json:"concurrencyMaxJimeng"`
	SendStrategy       string `json:"sendStrategy"`
	RetryCount         int    `json:"retryCount"`
	SendWait           int    `json:"sendWait"`
	DailyCapDoubao     int    `json:"dailyCapDoubao"`
	DailyCapQianwen    int    `json:"dailyCapQianwen"`
	DailyCapQwenIntl   int    `json:"dailyCapQwenIntl"`
	DailyCapGemini     int    `json:"dailyCapGemini"`
	DailyCapJimeng     int    `json:"dailyCapJimeng"`
	WaitBefore         int    `json:"waitBefore"`
	WaitResult         bool   `json:"waitResult"`
	WaitResultSeconds  int    `json:"waitResultSeconds"`
	TaskSwitch         bool   `json:"taskSwitch"`
	SwitchCount        int    `json:"switchCount"`
	StartBrowserIdx    int    `json:"startBrowserIdx"`
	TaskBrowserVisible bool   `json:"taskBrowserVisible"`
	RiskyWords         string `json:"riskyWords"`
	ProxyCustom        bool   `json:"proxyCustom"`
	ProxyURL           string `json:"proxyUrl"`
	CaptchaEnabled     bool   `json:"captchaEnabled"`
	CaptchaBackend     string `json:"captchaBackend"`
	CustomURL          string `json:"customUrl"`
	CustomKey          string `json:"customKey"`
	CustomModel        string `json:"customModel"`
	IkunocrKey         string `json:"ikunocrKey"`
	KernelAutoUpdate   bool   `json:"kernelAutoUpdate"`
	AutoCheck          bool   `json:"autoCheck"`
	UpdateMirror       string `json:"updateMirror"`
	Version            string `json:"version"`
	UpdateBusy         bool   `json:"updateBusy"`
	UpdateErr          string `json:"updateErr"`
	UpdateNotice       string `json:"updateNotice"`
	DotMatrixEffect    string `json:"dotMatrixEffect"`
}

// GetSettingsState returns the current application settings snapshot.
func (s *SettingsService) GetSettingsState(ctx context.Context) SettingsStateDTO {
	if s.app == nil {
		return SettingsStateDTO{Version: currentVersion(), DotMatrixEffect: defaultDotMatrixEffect}
	}
	out, err := callValue(s.app, ctx, func() SettingsStateDTO {
		a := s.app
		taskVisible := false
		if a.store != nil {
			taskVisible = a.store.GetBool(settingTaskBrowserVisible)
		}
		return SettingsStateDTO{
			Tab:                clampInt(a.settingsTab, 0, 3),
			OutputPath:         a.outputPath,
			ResolvedOutput:     a.resolvedOutputDir(),
			GenAutoNumber:      a.genAutoNumber,
			WashAutoNumber:     a.washAutoNumber,
			NumPrefix:          a.numPrefix,
			NumStart:           int(a.numStart),
			NumWidth:           int(a.numWidth),
			DlDir:              a.dlDir,
			ResolvedDlDir:      a.downloadDir(),
			DownloadConc:       int(a.downloadConc),
			DownloadRetries:    int(a.downloadRetries),
			DownloadRateKB:     int(a.downloadRateKB),
			RemoveWatermark:    a.removeWatermark,
			AutoDeleteHistory:  a.autoDeleteHistory,
			ConcurrencyAuto:         a.concurrencyAutoFor("豆包"),
			ConcurrencyMax:          a.concurrencyMaxFor("豆包"),
			ConcurrencyAutoQianwen:  a.concurrencyAutoFor("千问"),
			ConcurrencyMaxQianwen:   a.concurrencyMaxFor("千问"),
			ConcurrencyAutoQwenIntl: a.concurrencyAutoFor("千问国际"),
			ConcurrencyMaxQwenIntl:  a.concurrencyMaxFor("千问国际"),
			ConcurrencyAutoGemini:   a.concurrencyAutoFor("Gemini"),
			ConcurrencyMaxGemini:    a.concurrencyMaxFor("Gemini"),
			ConcurrencyAutoJimeng:   a.concurrencyAutoFor("即梦"),
			ConcurrencyMaxJimeng:    a.concurrencyMaxFor("即梦"),
			SendStrategy:       a.sendStrategy,
			RetryCount:         int(a.retryCount),
			SendWait:           int(a.sendWait),
			DailyCapDoubao:     a.shownDailyCap(),
			DailyCapQianwen:    a.shownDailyCapFor("千问"),
			DailyCapQwenIntl:   a.shownDailyCapFor("千问国际"),
			DailyCapGemini:     a.shownDailyCapFor("Gemini"),
			DailyCapJimeng:     a.shownDailyCapFor("即梦"),
			WaitBefore:         int(a.waitBefore),
			WaitResult:         a.waitResult,
			WaitResultSeconds:  int(a.waitResultS),
			TaskSwitch:         a.taskSwitch,
			SwitchCount:        int(a.switchCount),
			StartBrowserIdx:    int(a.startBrowserIdx),
			TaskBrowserVisible: taskVisible,
			RiskyWords:         a.riskyWords,
			ProxyCustom:        a.proxyCustom,
			ProxyURL:           a.proxyURL,
			CaptchaEnabled:     a.captchaEnabled,
			CaptchaBackend:     a.captchaBackend,
			CustomURL:          a.customURL,
			CustomKey:          a.customKey,
			CustomModel:        a.customModel,
			IkunocrKey:         a.ikunocrKey,
			KernelAutoUpdate:   a.store != nil && a.store.GetBool(settingKernelAutoUpdate),
			AutoCheck:          a.autoCheck,
			UpdateMirror:       a.updateMirror,
			Version:            currentVersion(),
			UpdateBusy:         a.updateBusy,
			UpdateErr:          a.updateErr,
			UpdateNotice:       a.updateNotice,
			DotMatrixEffect:    a.resolvedDotMatrixEffect(),
		}
	})
	if err != nil {
		return SettingsStateDTO{Version: currentVersion(), DotMatrixEffect: defaultDotMatrixEffect}
	}
	return out
}

func (a *App) setSettingFieldInternal(field, value string) {
	switch field {
	case "tab":
		a.settingsTab = clampInt(parseIntDefault(value, 0), 0, 3)
	case "outputPath":
		a.outputPath = strings.TrimSpace(value)
	case "genAutoNumber":
		a.genAutoNumber = value == "true" || value == "1"
	case "washAutoNumber":
		a.washAutoNumber = value == "true" || value == "1"
	case "numPrefix":
		a.numPrefix = value
	case "numStart":
		a.numStart = float64(clampInt(parseIntDefault(value, 1), 0, 9_000_000_000_000_000))
	case "numWidth":
		a.numWidth = float64(clampInt(parseIntDefault(value, 0), 0, 16))
	case "dlDir":
		a.dlDir = strings.TrimSpace(value)
	case "downloadConc":
		a.downloadConc = float64(clampInt(parseIntDefault(value, 4), 1, 8))
	case "downloadRetries":
		a.downloadRetries = float64(clampInt(parseIntDefault(value, 1), 0, 10))
	case "downloadRateKB", "downloadRateKb":
		a.downloadRateKB = float64(clampInt(parseIntDefault(value, 0), 0, 102400))
	case "removeWatermark":
		a.removeWatermark = value == "true" || value == "1"
	case "autoDeleteHistory":
		a.autoDeleteHistory = value == "true" || value == "1"
	case "concurrencyAuto", "concurrencyAutoDoubao":
		a.concurrencyAuto = value == "true" || value == "1"
		if a.engine != nil {
			a.engine.SetConcurrency("豆包", a.taskConcurrencyFor("豆包"))
		}
	case "concurrencyMax", "concurrencyMaxDoubao":
		a.concurrencyMax = float64(clampInt(parseIntDefault(value, tasks.PlatformInFlight), tasks.PlatformInFlight, tasks.MaxPlatformInFlight))
		if a.engine != nil {
			a.engine.SetConcurrency("豆包", a.taskConcurrencyFor("豆包"))
		}
	case "concurrencyAutoQianwen":
		a.concurrencyAutoQianwen = value == "true" || value == "1"
		if a.engine != nil {
			a.engine.SetConcurrency("千问", a.taskConcurrencyFor("千问"))
		}
	case "concurrencyMaxQianwen":
		a.concurrencyMaxQianwen = float64(clampInt(parseIntDefault(value, tasks.PlatformInFlight), tasks.PlatformInFlight, tasks.MaxPlatformInFlight))
		if a.engine != nil {
			a.engine.SetConcurrency("千问", a.taskConcurrencyFor("千问"))
		}
	case "concurrencyAutoQwenIntl":
		a.concurrencyAutoQwenIntl = value == "true" || value == "1"
		if a.engine != nil {
			a.engine.SetConcurrency("千问国际", a.taskConcurrencyFor("千问国际"))
		}
	case "concurrencyMaxQwenIntl":
		a.concurrencyMaxQwenIntl = float64(clampInt(parseIntDefault(value, tasks.PlatformInFlight), tasks.PlatformInFlight, tasks.MaxPlatformInFlight))
		if a.engine != nil {
			a.engine.SetConcurrency("千问国际", a.taskConcurrencyFor("千问国际"))
		}
	case "concurrencyAutoGemini":
		a.concurrencyAutoGemini = value == "true" || value == "1"
		if a.engine != nil {
			a.engine.SetConcurrency("Gemini", a.taskConcurrencyFor("Gemini"))
		}
	case "concurrencyMaxGemini":
		a.concurrencyMaxGemini = float64(clampInt(parseIntDefault(value, tasks.PlatformInFlight), tasks.PlatformInFlight, tasks.MaxPlatformInFlight))
		if a.engine != nil {
			a.engine.SetConcurrency("Gemini", a.taskConcurrencyFor("Gemini"))
		}
	case "concurrencyAutoJimeng":
		a.concurrencyAutoJimeng = value == "true" || value == "1"
		if a.engine != nil {
			a.engine.SetConcurrency("即梦", a.taskConcurrencyFor("即梦"))
		}
	case "concurrencyMaxJimeng":
		a.concurrencyMaxJimeng = float64(clampInt(parseIntDefault(value, tasks.PlatformInFlight), tasks.PlatformInFlight, tasks.MaxPlatformInFlight))
		if a.engine != nil {
			a.engine.SetConcurrency("即梦", a.taskConcurrencyFor("即梦"))
		}
	case "sendStrategy":
		a.sendStrategy = pickStr(value, "顺序循环", []string{"顺序循环", "随机循环"})
	case "retryCount":
		a.retryCount = float64(clampInt(parseIntDefault(value, 0), 0, 20))
	case "sendWait":
		a.sendWait = float64(clampInt(parseIntDefault(value, 0), 0, 600))
	case "dailyCapDoubao":
		a.dailyCap = float64(clampInt(parseIntDefault(value, 0), 0, 200))
		a.dailyCapSet = true
	case "dailyCapQianwen":
		a.dailyCapQianwen = float64(clampInt(parseIntDefault(value, 0), 0, 200))
		a.dailyCapQianwenSet = true
	case "dailyCapQwenIntl":
		a.dailyCapQwenIntl = float64(clampInt(parseIntDefault(value, 0), 0, 200))
		a.dailyCapQwenIntlSet = true
	case "dailyCapGemini":
		a.dailyCapGemini = float64(clampInt(parseIntDefault(value, 0), 0, 200))
		a.dailyCapGeminiSet = true
	case "dailyCapJimeng":
		a.dailyCapJimeng = float64(clampInt(parseIntDefault(value, 0), 0, 200))
		a.dailyCapJimengSet = true
	case "waitBefore":
		a.waitBefore = float64(clampInt(parseIntDefault(value, 0), 0, 60))
	case "waitResult":
		a.waitResult = value == "true" || value == "1"
	case "waitResultSeconds":
		a.waitResultS = float64(clampInt(parseIntDefault(value, 60), 10, 1800))
	case "taskSwitch":
		a.taskSwitch = value == "true" || value == "1"
	case "switchCount":
		a.switchCount = float64(clampInt(parseIntDefault(value, 1), 1, 9999))
	case "startBrowserIdx":
		a.startBrowserIdx = float64(clampInt(parseIntDefault(value, 1), 1, 999))
	case "taskBrowserVisible":
		vis := value == "true" || value == "1"
		if a.store != nil {
			a.store.SetInt(settingTaskBrowserVisible, boolInt(vis))
		}
		if a.manager != nil {
			a.manager.SetTaskVisible(vis)
		}
	case "riskyWords":
		a.riskyWords = value
	case "resetRiskyWords":
		a.riskyWords = tasks.RiskySetting()
	case "proxyCustom":
		a.proxyCustom = value == "true" || value == "1"
		a.persistProxy()
	case "proxyURL", "proxyUrl":
		a.proxyURL = strings.TrimSpace(value)
		a.persistProxy()
	case "captchaEnabled":
		a.captchaEnabled = value == "true" || value == "1"
	case "captchaBackend":
		a.captchaBackend = pickStr(value, "free", []string{"free", "custom", "ikunocr"})
		a.captchaChoice = a.captchaBackend
	case "customURL", "customUrl":
		a.customURL = value
	case "customKey":
		a.customKey = value
	case "customModel":
		a.customModel = value
	case "ikunocrKey":
		a.ikunocrKey = value
	case "kernelAutoUpdate":
		if a.store != nil {
			a.store.SetInt(settingKernelAutoUpdate, boolInt(value == "true" || value == "1"))
		}
	case "autoCheck":
		a.autoCheck = value == "true" || value == "1"
		a.persistUpdatePrefs()
	case "updateMirror":
		a.updateMirror = strings.TrimSpace(value)
		a.persistUpdatePrefs()
	case "dotMatrixEffect":
		a.dotMatrixEffect = normalizeDotMatrixEffect(value)
	}
	a.saveSettings()
	if field == "proxyCustom" || field == "proxyURL" || field == "proxyUrl" ||
		field == "concurrencyAuto" || field == "concurrencyAutoDoubao" || field == "concurrencyMax" || field == "concurrencyMaxDoubao" ||
		field == "concurrencyAutoQianwen" || field == "concurrencyMaxQianwen" ||
		field == "concurrencyAutoGemini" || field == "concurrencyMaxGemini" ||
		field == "concurrencyAutoJimeng" || field == "concurrencyMaxJimeng" ||
		field == "dailyCapDoubao" || field == "dailyCapQianwen" || field == "dailyCapQwenIntl" || field == "dailyCapGemini" || field == "dailyCapJimeng" {
		a.emit("settings", "shell", "workbench", "browsers")
		return
	}
	a.emit("settings", "shell", "workbench")
}

// SetSettingField updates one preference field and persists settings to SQLite.
func (s *SettingsService) SetSettingField(ctx context.Context, field, value string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.setSettingFieldInternal(field, value)
	})
}

// PickSettingDir opens a directory picker and updates the target path setting field.
func (s *SettingsService) PickSettingDir(ctx context.Context, field string) error {
	if s.app == nil {
		return nil
	}
	title := "选择目录"
	switch field {
	case "outputPath":
		title = "选择输出目录"
	case "dlDir":
		title = "选择下载目录"
	}
	paths, err := s.app.pickDirsAsync(ctx, title, false)
	if err != nil {
		logging.Log.Errorf("选择目录失败: %v", err)
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	return s.app.call(ctx, func() {
		s.app.setSettingFieldInternal(field, paths[0])
	})
}

// PickSettingFile opens a file picker and updates the target file setting field.
func (s *SettingsService) PickSettingFile(ctx context.Context, field string) error {
	if s.app == nil {
		return nil
	}
	paths, err := s.app.pickFilesAsync(ctx, "选择文件", false, nil, "文件")
	if err != nil || len(paths) == 0 {
		return err
	}
	return s.app.call(ctx, func() {
		s.app.setSettingFieldInternal(field, paths[0])
	})
}

// SaveSettingsNow explicitly flushes settings to SQLite and shows a confirmation toast.
func (s *SettingsService) SaveSettingsNow(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.saveSettings()
		s.app.pushToast("success", "设置已保存")
		s.app.emit("settings", "workbench")
	})
}

// ---- Test bridge wrappers ----

func getSettingsStateBridge() SettingsStateDTO {
	return (&SettingsService{app: getApp()}).GetSettingsState(context.Background())
}

func setSettingFieldBridge(field, value string) {
	(&SettingsService{app: getApp()}).SetSettingField(context.Background(), field, value)
}

func saveSettingsNowBridge() {
	(&SettingsService{app: getApp()}).SaveSettingsNow(context.Background())
}
