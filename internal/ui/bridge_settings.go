package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/tsx"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/tasks"
)

func init() {
	tsx.RegisterFunctions("tulai/backend", map[string]any{
		"getSettingsState": getSettingsStateBridge,
		"setSettingField":  setSettingFieldBridge,
		"pickSettingDir":   pickSettingDirBridge,
		"pickSettingFile":  pickSettingFileBridge,
		"saveSettingsNow":  saveSettingsNowBridge,
	})
}

type SettingsStateDTO struct {
	Tab                int // 0: 常规与输出 1: 账号与任务 2: 网络与高级 3: 关于与更新
	OutputPath         string
	ResolvedOutput     string
	GenAutoNumber      bool
	WashAutoNumber     bool
	NumPrefix          string
	NumStart           int
	NumWidth           int
	DlDir              string
	ResolvedDlDir      string
	DownloadConc       int
	DownloadRetries    int
	DownloadRateKB     int
	RemoveWatermark    bool
	ConcurrencyAuto    bool
	SendStrategy       string
	RetryCount         int
	SendWait           int
	DailyCapDoubao     int
	DailyCapQianwen    int
	DailyCapAgy        int
	DailyCapChatGPT    int
	ChatGPTModel       string
	WaitBefore         int
	WaitResult         bool
	WaitResultSeconds  int
	TaskSwitch         bool
	SwitchCount        int
	StartBrowserIdx    int
	TaskBrowserVisible bool
	RiskyWords         string
	AgyBin             string
	AgyHomeRoot        string
	AgyModel           string
	AgyTimeoutMin      float64
	ProxyCustom        bool
	ProxyURL           string
	CaptchaEnabled     bool
	CaptchaBackend     string
	CustomURL          string
	CustomKey          string
	CustomModel        string
	IkunocrKey         string
	AutoCheck          bool
	UpdateMirror       string
	Version            string
	UpdateBusy         bool
	UpdateErr          string
	UpdateNotice       string
}

func getSettingsStateBridge() SettingsStateDTO {
	a := getApp()
	if a == nil {
		return SettingsStateDTO{}
	}
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
		ConcurrencyAuto:    a.concurrencyAuto,
		SendStrategy:       a.sendStrategy,
		RetryCount:         int(a.retryCount),
		SendWait:           int(a.sendWait),
		DailyCapDoubao:     a.shownDailyCap(),
		DailyCapQianwen:    a.shownDailyCapFor("千问"),
		DailyCapAgy:        a.shownDailyCapFor(agy.Platform),
		DailyCapChatGPT:    a.shownDailyCapFor("ChatGPT"),
		ChatGPTModel:       a.chatGPTModel,
		WaitBefore:         int(a.waitBefore),
		WaitResult:         a.waitResult,
		WaitResultSeconds:  int(a.waitResultS),
		TaskSwitch:         a.taskSwitch,
		SwitchCount:        int(a.switchCount),
		StartBrowserIdx:    int(a.startBrowserIdx),
		TaskBrowserVisible: taskVisible,
		RiskyWords:         a.riskyWords,
		AgyBin:             a.agyBin,
		AgyHomeRoot:        a.agyHomeRoot,
		AgyModel:           a.agyModel,
		AgyTimeoutMin:      a.agyTimeoutMin,
		ProxyCustom:        a.proxyCustom,
		ProxyURL:           a.proxyURL,
		CaptchaEnabled:     a.captchaEnabled,
		CaptchaBackend:     a.captchaBackend,
		CustomURL:          a.customURL,
		CustomKey:          a.customKey,
		CustomModel:        a.customModel,
		IkunocrKey:         a.ikunocrKey,
		AutoCheck:          a.autoCheck,
		UpdateMirror:       a.updateMirror,
		Version:            currentVersion(),
		UpdateBusy:         a.updateBusy,
		UpdateErr:          a.updateErr,
		UpdateNotice:       a.updateNotice,
	}
}

func setSettingFieldBridge(field, value string) {
	a := getApp()
	if a == nil {
		return
	}
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
	case "downloadRateKB":
		a.downloadRateKB = float64(clampInt(parseIntDefault(value, 0), 0, 102400))
	case "removeWatermark":
		a.removeWatermark = value == "true" || value == "1"
	case "concurrencyAuto":
		a.concurrencyAuto = value == "true" || value == "1"
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
	case "dailyCapAgy":
		a.dailyCapAgy = float64(clampInt(parseIntDefault(value, 0), 0, 200))
		a.dailyCapAgySet = true
	case "dailyCapChatGPT":
		a.dailyCapChatGPT = float64(clampInt(parseIntDefault(value, 0), 0, 200))
		a.dailyCapChatGPTSet = true
	case "chatGPTModel":
		a.chatGPTModel = value
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
	case "agyBin":
		a.agyBin = strings.TrimSpace(value)
	case "agyHomeRoot":
		a.agyHomeRoot = strings.TrimSpace(value)
	case "agyModel":
		a.agyModel = strings.TrimSpace(value)
	case "agyTimeoutMin":
		if m, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			if m < 1 {
				m = 1
			}
			if m > 60 {
				m = 60
			}
			a.agyTimeoutMin = m
		}
	case "proxyCustom":
		a.proxyCustom = value == "true" || value == "1"
		a.persistProxy()
	case "proxyURL":
		a.proxyURL = strings.TrimSpace(value)
		a.persistProxy()
	case "captchaEnabled":
		a.captchaEnabled = value == "true" || value == "1"
	case "captchaBackend":
		a.captchaBackend = pickStr(value, "free", []string{"free", "custom", "ikunocr"})
		a.captchaChoice = a.captchaBackend
	case "customURL":
		a.customURL = value
	case "customKey":
		a.customKey = value
	case "customModel":
		a.customModel = value
	case "ikunocrKey":
		a.ikunocrKey = value
	case "autoCheck":
		a.autoCheck = value == "true" || value == "1"
		a.persistUpdatePrefs()
	case "updateMirror":
		a.updateMirror = strings.TrimSpace(value)
		a.persistUpdatePrefs()
	}
	a.saveSettings()
	a.emit("settings", "workbench")
}

func pickSettingDirBridge(ctx context.Context, field string) error {
	a := getApp()
	if a == nil {
		return nil
	}
	title := "选择目录"
	switch field {
	case "outputPath":
		title = "选择输出目录"
	case "dlDir":
		title = "选择下载目录"
	case "agyHomeRoot":
		title = "选择 Antigravity 账号根目录"
	}
	paths, err := a.pickDirsAsync(ctx, title, false)
	if err != nil {
		logging.Log.Errorf("选择目录失败: %v", err)
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	return a.onScope(ctx, func() {
		setSettingFieldBridge(field, paths[0])
	})
}

func pickSettingFileBridge(ctx context.Context, field string) error {
	a := getApp()
	if a == nil {
		return nil
	}
	paths, err := a.pickFilesAsync(ctx, "选择 agy 可执行文件", false, nil, "可执行文件")
	if err != nil || len(paths) == 0 {
		return err
	}
	return a.onScope(ctx, func() {
		setSettingFieldBridge(field, paths[0])
	})
}

func saveSettingsNowBridge() {
	if a := getApp(); a != nil {
		a.saveSettings()
		a.pushToast("success", "设置已保存")
		a.emit("settings", "workbench")
	}
}

var (
	_ = browser.KindCamoufox
	_ = agy.Platform
)
