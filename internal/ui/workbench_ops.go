package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

const (
	DefaultWashPrompt    = prompts.DefaultWashPrompt
	DefaultFissionPrompt = prompts.DefaultFissionPrompt
	verificationHint     = "在浏览器中完成验证后重新开始任务"
)

// ---- Output size & wash styles ----

const (
	outputOriginal = "原比例"
	outputRatio    = "宽高比"
)

const washStyleOriginal = "保持原风格"

type washStylePreset struct {
	label       string
	instruction string
}

var washStyles = []washStylePreset{
	{washStyleOriginal, ""},
	{"像素风格", "把选中的整幅图重新画成低分辨率的二维游戏像素画：以粗粒度逻辑像素块组织主体和背景，保持锐利清晰的像素边缘。画面各处都要有清楚可见的方形色块，主体轮廓、曲线和镶嵌线都呈粗方块阶梯，不能保留平滑矢量曲线。色块紧密无缝、内部纯色，不能给每个像素画边框、留缝或加圆点高光。去除原图的细格线、填色数字、印刷网点、珠粒与纸面纹理；不要在高清原图上叠加网格，也不要做成拼豆或钻石画。保留主体主要形状和颜色，图案内部必要的深色轮廓可以由方块组成。"},
	{"水彩插画", "使用透明水彩叠色、柔和晕染和轻微纸面肌理，呈现二维水彩插画。"},
	{"油画风格", "使用可见的油画笔触、丰富的色彩层次和适度画布肌理，呈现二维油画。"},
	{"动漫插画", "使用清晰线条、概括的色块和柔和赛璐璐光影，呈现二维动漫插画。"},
	{"扁平插画", "使用简洁轮廓、平整色块和少量阴影，呈现二维扁平插画。"},
	{"国风水墨", "使用水墨笔触、墨色浓淡和适度留白，呈现二维中国风水墨画。"},
	{"黑白线稿", "转换为纯黑白线稿：白底、黑色轮廓和必要细节线条，不使用任何彩色、灰度阴影或彩色填充，适合印刷和上色。"},
	{"复古版画", "使用版画式刻线、有限色彩和轻微复古纸感，呈现二维复古版画。"},
}

var outputRatios = []string{"1:1", "4:3", "3:4", "16:9", "9:16", "3:2", "2:3"}
var geminiOutputRatios = []string{"1:1", "4:3", "3:4", "16:9", "9:16"}

type outputPresetSpec struct {
	label string
	ratio string
}

var outputPresets = []outputPresetSpec{
	{label: "原比例"},
	{label: "1:1（正方形）", ratio: "1:1"},
	{label: "4:3（标准横）", ratio: "4:3"},
	{label: "3:4（标准竖）", ratio: "3:4"},
	{label: "16:9（宽屏）", ratio: "16:9"},
	{label: "9:16（竖屏）", ratio: "9:16"},
	{label: "3:2（相机横）", ratio: "3:2"},
	{label: "2:3（海报竖）", ratio: "2:3"},
}

var geminiOutputPresets = []outputPresetSpec{
	{label: "原比例"},
	{label: "1:1（正方形）", ratio: "1:1"},
	{label: "4:3（标准横）", ratio: "4:3"},
	{label: "3:4（标准竖）", ratio: "3:4"},
	{label: "16:9（宽屏）", ratio: "16:9"},
	{label: "9:16（竖屏）", ratio: "9:16"},
}

type outputSizePrefs struct {
	mode   string
	preset string
	ratio  string
}

func outputRatiosFor(platform string) []string {
	if platform == "Gemini" {
		return geminiOutputRatios
	}
	return outputRatios
}

func outputPresetsFor(platform string) []outputPresetSpec {
	if platform == "Gemini" {
		return geminiOutputPresets
	}
	return outputPresets
}

func outputPresetLabels() []string {
	return outputPresetLabelsFor("")
}

func outputPresetLabelsFor(platform string) []string {
	specs := outputPresetsFor(platform)
	labels := make([]string, 0, len(specs))
	for _, p := range specs {
		labels = append(labels, p.label)
	}
	return labels
}

func washStyleLabels() []string {
	labels := make([]string, 0, len(washStyles))
	for _, style := range washStyles {
		labels = append(labels, style.label)
	}
	return labels
}

func washStyleInstruction(label string) string {
	for _, style := range washStyles {
		if style.label == label && style.instruction != "" {
			return "本次洗图风格：" + style.label + "。" + style.instruction +
				"只对选中的单幅主图应用此风格，封面拼版的其他格和分隔线一律不要；风格转换优先于先前的其他风格要求，包括保留原画风、笔触和纹理。尽量保留所选单幅内主体的数量、位置与关键特征；彩色风格保留画面原有背景的颜色和明暗，不将深色背景自动替换为白色，白底线稿按风格要求处理。只输出平面图案。画面四角、边缘及内部不得新增签名、花押、作者印章、版权符号、水印或类似字母的手写笔迹；去除原有署名和水印后，用周围图案与纹理自然补全，不添加新文字或实物载体。"
		}
	}
	return ""
}

func (a *App) loadWashStyle() {
	if a.store == nil {
		a.washStyle = washStyleOriginal
		return
	}
	a.washStyle = pickStr(a.store.Get("wash_style"), washStyleOriginal, washStyleLabels())
}

func (a *App) saveWashStyle() {
	if a.store == nil {
		return
	}
	a.store.Set("wash_style", a.washStyle)
}

func (a *App) outputPrefsFor(platform string) *outputSizePrefs {
	switch platform {
	case "千问":
		a.qianwenOutput.ensureInit()
		return &a.qianwenOutput
	case "千问国际":
		a.qwenIntlOutput.ensureInit()
		return &a.qwenIntlOutput
	case "Gemini":
		a.geminiOutput.ensureInitFor("Gemini")
		return &a.geminiOutput
	default:
		a.doubaoOutput.ensureInit()
		return &a.doubaoOutput
	}
}

func (a *App) activeOutputPrefs() *outputSizePrefs {
	return a.outputPrefsFor(a.selectedPlatform())
}

func outputStorePrefix(platform string) string {
	switch platform {
	case "千问":
		return "qianwen_output_"
	case "千问国际":
		return "qwen_intl_output_"
	case "Gemini":
		return "gemini_output_"
	default:
		return "doubao_output_"
	}
}

func (p *outputSizePrefs) ensureInit() {
	p.ensureInitFor("")
}

func (p *outputSizePrefs) ensureInitFor(platform string) {
	if p.mode == "" {
		p.mode = outputOriginal
	}
	if p.preset == "" {
		p.syncPresetFor(platform)
	}
}

func normalizeOutputRatio(raw string) string {
	return normalizeOutputRatioFor("", raw)
}

func normalizeOutputRatioFor(platform, raw string) string {
	norm := strings.TrimSpace(strings.ReplaceAll(raw, "：", ":"))
	return pickStr(norm, "", outputRatiosFor(platform))
}

func loadPlatformOutputPrefs(s *store.Store, platform string) outputSizePrefs {
	if s == nil {
		return outputSizePrefs{mode: outputOriginal, preset: outputOriginal}
	}
	prefix := outputStorePrefix(platform)
	rawMode := strings.TrimSpace(s.Get(prefix + "size_mode"))
	ratio := normalizeOutputRatioFor(platform, s.Get(prefix+"ratio"))
	// Migrate legacy qianwen_output_ratio when qianwen_output_size_mode has not been stored yet.
	if platform == "千问" && rawMode == "" && ratio != "" {
		rawMode = outputRatio
	}
	if rawMode == outputRatio && ratio != "" {
		p := outputSizePrefs{mode: outputRatio, ratio: ratio}
		p.syncPresetFor(platform)
		return p
	}
	return outputSizePrefs{mode: outputOriginal, preset: outputOriginal}
}

func savePlatformOutputPrefs(s *store.Store, platform string, p outputSizePrefs) {
	if s == nil {
		return
	}
	prefix := outputStorePrefix(platform)
	p.ensureInitFor(platform)
	p.syncPresetFor(platform)
	s.Set(prefix+"size_mode", p.mode)
	s.Set(prefix+"ratio", p.ratio)
	s.Set(prefix+"width", "")
	s.Set(prefix+"height", "")
	s.SetInt(prefix+"crop", 0)
}

func (a *App) loadOutputSize() {
	a.doubaoOutput = loadPlatformOutputPrefs(a.store, "豆包")
	a.qianwenOutput = loadPlatformOutputPrefs(a.store, "千问")
	a.qwenIntlOutput = loadPlatformOutputPrefs(a.store, "千问国际")
	a.geminiOutput = loadPlatformOutputPrefs(a.store, "Gemini")
}

func (a *App) saveOutputSize() {
	savePlatformOutputPrefs(a.store, "豆包", a.doubaoOutput)
	savePlatformOutputPrefs(a.store, "千问", a.qianwenOutput)
	savePlatformOutputPrefs(a.store, "千问国际", a.qwenIntlOutput)
	savePlatformOutputPrefs(a.store, "Gemini", a.geminiOutput)
}

func (p *outputSizePrefs) syncPreset() {
	p.syncPresetFor("")
}

func (p *outputSizePrefs) syncPresetFor(platform string) {
	if p.mode == outputRatio {
		if picked := normalizeOutputRatioFor(platform, p.ratio); picked != "" {
			p.ratio = picked
			for _, spec := range outputPresetsFor(platform) {
				if spec.ratio == picked {
					p.preset = spec.label
					return
				}
			}
		}
	}
	p.mode = outputOriginal
	p.ratio = ""
	p.preset = outputOriginal
}

func (p *outputSizePrefs) applyPreset(label string) bool {
	return p.applyPresetFor("", label)
}

func (p *outputSizePrefs) applyPresetFor(platform, label string) bool {
	for _, spec := range outputPresetsFor(platform) {
		if spec.label != label {
			continue
		}
		if spec.ratio != "" {
			p.mode = outputRatio
			p.ratio = spec.ratio
			p.preset = spec.label
		} else {
			p.mode = outputOriginal
			p.ratio = ""
			p.preset = outputOriginal
		}
		return true
	}
	return false
}

func (p *outputSizePrefs) update(preset, mode, ratio string) {
	p.updateFor("", preset, mode, ratio)
}

func (p *outputSizePrefs) updateFor(platform, preset, mode, ratio string) {
	p.ensureInitFor(platform)
	preset = strings.TrimSpace(preset)
	if preset != "" {
		if p.applyPresetFor(platform, preset) {
			return
		}
		if picked := normalizeOutputRatioFor(platform, preset); picked != "" {
			p.mode = outputRatio
			p.ratio = picked
			p.syncPresetFor(platform)
			return
		}
		// Reject unsupported or custom preset labels; keep existing valid selection.
		p.syncPresetFor(platform)
		return
	}
	switch strings.TrimSpace(mode) {
	case "original", outputOriginal:
		p.mode = outputOriginal
		p.ratio = ""
		p.preset = outputOriginal
		return
	case "ratio", outputRatio, "":
		trimmed := strings.TrimSpace(ratio)
		if trimmed == "" {
			p.mode = outputOriginal
			p.ratio = ""
			p.preset = outputOriginal
			return
		}
		if picked := normalizeOutputRatioFor(platform, trimmed); picked != "" {
			p.mode = outputRatio
			p.ratio = picked
			p.syncPresetFor(platform)
			return
		}
	}
	p.syncPresetFor(platform)
}

func (p outputSizePrefs) taskOutputSize() tasks.OutputSize {
	return p.taskOutputSizeFor("")
}

func (p outputSizePrefs) taskOutputSizeFor(platform string) tasks.OutputSize {
	if p.mode == outputRatio {
		if picked := normalizeOutputRatioFor(platform, p.ratio); picked != "" {
			return tasks.OutputSize{Ratio: picked}
		}
	}
	return tasks.OutputSize{}
}

func (a *App) outputSizeFor(platform string) tasks.OutputSize {
	return a.outputPrefsFor(platform).taskOutputSizeFor(platform)
}

// ---- Page switching & session management ----

func platformSettingKey(platform, base string) string {
	switch platform {
	case "千问":
		return base + "_qianwen"
	case "千问国际":
		return base + "_qwen_intl"
	case "Gemini":
		return base + "_gemini"
	default:
		return base
	}
}

func defaultWashPromptFor(platform string) string {
	if platform == "豆包" {
		return prompts.DoubaoWashPrompt
	}
	return DefaultWashPrompt
}

func defaultPageStateFor(platform string, page int) pageState {
	st := pageState{srcSel: -1}
	if page == pageWash {
		st.prompts = defaultWashPromptFor(platform)
	} else if page == pageFission {
		st.prompts = DefaultFissionPrompt
	}
	return st
}

func defaultPlatformWorkspace(platform string) platformWorkspace {
	return platformWorkspace{
		pages: map[int]pageState{
			pageHome:    defaultPageStateFor(platform, pageHome),
			pageWash:    defaultPageStateFor(platform, pageWash),
			pageFission: defaultPageStateFor(platform, pageFission),
		},
		batchMode:           batchText,
		genPromptChoice:     choosePromptLabel,
		imgGenPromptChoice:  choosePromptLabel,
		washPromptChoice:    choosePromptLabel,
		fissionPromptChoice: choosePromptLabel,
		textRepeat:          1,
		imageRepeat:         1,
		imagePromptMode:     "unified",
	}
}

func workspacePageState(ws platformWorkspace, platform string, page int) pageState {
	if st, ok := ws.pages[page]; ok {
		return st
	}
	return defaultPageStateFor(platform, page)
}

func (a *App) capturePage() pageState {
	return pageState{
		sources: a.sources, srcSel: a.srcSel, prompts: a.prompts, queue: a.queue,
		progress: a.progress, progText: a.progText, stageTab: a.stageTab, stagePicked: a.stagePicked,
		flowPin: a.flowPin, skipImages: a.skipImages,
	}
}

func (a *App) restorePage(s pageState) {
	a.sources, a.srcSel, a.prompts, a.queue = s.sources, s.srcSel, s.prompts, s.queue
	a.progress, a.progText, a.stageTab, a.stagePicked = s.progress, s.progText, s.stageTab, s.stagePicked
	a.flowPin, a.skipImages = s.flowPin, s.skipImages
}

func (a *App) capturePlatformWorkspace() platformWorkspace {
	pages := make(map[int]pageState, len(a.parked)+1)
	for p, st := range a.parked {
		pages[p] = st
	}
	pages[a.statePage] = a.capturePage()
	return platformWorkspace{
		pages:               pages,
		batchMode:           a.batchMode,
		textPrompts:         a.resolvedTextPrompts(),
		imgPrompts:          a.resolvedImagePrompts(),
		freeformPrompts:     a.freeformPrompts,
		imgFreeformPrompts:  a.imgFreeformPrompts,
		promptPick:          a.promptPick,
		imgPromptPick:       a.imgPromptPick,
		promptChoice:        a.promptChoice,
		genPromptChoice:     a.genPromptChoice,
		imgGenPromptChoice:  a.imgGenPromptChoice,
		washPromptChoice:    a.washPromptChoice,
		fissionPromptChoice: a.fissionPromptChoice,
		textRepeat:          clampRepeat(a.textRepeat),
		imageRepeat:         clampRepeat(a.imageRepeat),
		imagePromptMode:     normalizeImagePromptMode(a.imagePromptMode),
		genSession:          a.genSession,
		washSession:         a.washSession,
		fissionSession:      a.fissionSession,
		rewashCounts:        a.rewashCounts,
		rewashUndo:          a.rewashUndo,
		resultsDoneOpen:     a.resultsDoneOpen,
		gallerySelected:     a.gallerySelected,
		fissionRetryBase:    a.fissionRetryBase,
	}
}

func (a *App) restorePlatformWorkspace(ws platformWorkspace, platform string) {
	parked := make(map[int]pageState, len(ws.pages))
	for p, st := range ws.pages {
		if p != a.statePage {
			parked[p] = st
		}
	}
	a.parked = parked
	a.restorePage(workspacePageState(ws, platform, a.statePage))
	a.batchMode = ws.batchMode
	a.textPrompts = ws.textPrompts
	a.imgPrompts = ws.imgPrompts
	a.freeformPrompts = ws.freeformPrompts
	a.imgFreeformPrompts = ws.imgFreeformPrompts
	a.promptPick = ws.promptPick
	a.imgPromptPick = ws.imgPromptPick
	a.promptChoice = ws.promptChoice
	a.genPromptChoice = ws.genPromptChoice
	a.imgGenPromptChoice = ws.imgGenPromptChoice
	a.washPromptChoice = ws.washPromptChoice
	a.fissionPromptChoice = ws.fissionPromptChoice
	a.textRepeat = clampRepeat(ws.textRepeat)
	a.imageRepeat = clampRepeat(ws.imageRepeat)
	a.imagePromptMode = normalizeImagePromptMode(ws.imagePromptMode)
	a.genSession = ws.genSession
	a.washSession = ws.washSession
	a.fissionSession = ws.fissionSession
	a.rewashCounts = ws.rewashCounts
	a.rewashUndo = ws.rewashUndo
	a.resultsDoneOpen = ws.resultsDoneOpen
	a.gallerySelected = ws.gallerySelected
	a.fissionRetryBase = ws.fissionRetryBase
}

func clampRepeat(n int) int {
	if n < 1 {
		return 1
	}
	if n > 10 {
		return 10
	}
	return n
}

func normalizeImagePromptMode(mode string) string {
	if strings.TrimSpace(mode) == "per_line" {
		return "per_line"
	}
	return "unified"
}

func (a *App) homePrompts() string {
	if a.statePage == pageHome {
		return a.prompts
	}
	if gen, ok := a.parked[pageHome]; ok {
		return gen.prompts
	}
	return ""
}

func (a *App) setHomePrompts(v string) {
	if a.statePage == pageHome {
		a.prompts = v
		return
	}
	if a.parked == nil {
		a.parked = map[int]pageState{}
	}
	gen, ok := a.parked[pageHome]
	if !ok {
		gen = defaultPageStateFor(a.selectedPlatform(), pageHome)
	}
	gen.prompts = v
	a.parked[pageHome] = gen
}

func (a *App) resolvedTextPrompts() string {
	if a.batchMode == batchText {
		return a.homePrompts()
	}
	return a.textPrompts
}

func (a *App) resolvedImagePrompts() string {
	if a.batchMode == batchImage {
		return a.homePrompts()
	}
	return a.imgPrompts
}

func (a *App) switchBatchMode(next int, carryIfEmpty bool) bool {
	if next != batchImage {
		next = batchText
	}
	if a.batchMode == next {
		return false
	}
	cur := a.homePrompts()
	if a.batchMode == batchText {
		a.textPrompts = cur
		if carryIfEmpty && strings.TrimSpace(a.imgPrompts) == "" && strings.TrimSpace(cur) != "" {
			a.imgPrompts = cur
		}
		a.batchMode = batchImage
		a.setHomePrompts(a.imgPrompts)
	} else {
		a.imgPrompts = cur
		a.batchMode = batchText
		a.setHomePrompts(a.textPrompts)
	}
	if a.store != nil {
		plat := a.selectedPlatform()
		key := platformSettingKey(plat, "batch_mode")
		if a.batchMode == batchImage {
			a.store.Set(key, "image")
		} else {
			a.store.Set(key, "text")
		}
		a.store.Set(platformSettingKey(plat, "prompts"), a.resolvedTextPrompts())
		a.store.Set(platformSettingKey(plat, "img_prompts"), a.resolvedImagePrompts())
	}
	return true
}

func (a *App) currentRepeatCount() int {
	if a.statePage != pageHome {
		return 1
	}
	if a.batchMode == batchImage {
		return clampRepeat(a.imageRepeat)
	}
	return clampRepeat(a.textRepeat)
}

func (a *App) plannedTaskCount() (units, total int) {
	switch a.statePage {
	case pageWash:
		n := len(a.washPendingPaths())
		return n, n
	case pageFission:
		if a.skipImages {
			if len(a.taskPromptList()) == 4 {
				return 4, 4
			}
			return 0, 0
		}
		if len(a.batchImages()) == 1 {
			return 4, 4
		}
		return 0, 0
	default:
		repeat := a.currentRepeatCount()
		if a.batchMode == batchImage {
			n := len(a.batchImages())
			return n, n * repeat
		}
		n := len(a.taskPromptList())
		if n == 0 && strings.TrimSpace(a.promptExtras(pageHome, a.currentPromptParts())) != "" {
			n = 1
		}
		return n, n * repeat
	}
}

const maxPromptTextImportBytes = 2 << 20

func decodePromptTextBytes(data []byte) (string, error) {
	if len(data) >= 2 && ((data[0] == 0xFF && data[1] == 0xFE) || (data[0] == 0xFE && data[1] == 0xFF)) {
		decoded, err := unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder().Bytes(data)
		if err != nil || !utf8.Valid(decoded) {
			return "", fmt.Errorf("提示词文本 UTF-16 编码无效")
		}
		return string(decoded), nil
	}
	trimmed := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(trimmed) {
		return string(trimmed), nil
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	if err != nil || !utf8.Valid(decoded) || bytes.Contains(decoded, []byte("\xef\xbf\xbd")) {
		return "", fmt.Errorf("提示词文本编码无效，请保存为 UTF-8 或 GBK")
	}
	return string(decoded), nil
}

func readPromptTextFiles(paths []string) (string, int, error) {
	var allLines []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			return "", 0, fmt.Errorf("无法读取提示词文件: %w", err)
		}
		if info.IsDir() {
			return "", 0, fmt.Errorf("请选择文本文件，而不是文件夹")
		}
		if info.Size() > maxPromptTextImportBytes {
			return "", 0, fmt.Errorf("提示词文件超过 2 MB 上限")
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return "", 0, fmt.Errorf("读取提示词文件失败: %w", err)
		}
		text, err := decodePromptTextBytes(data)
		if err != nil {
			return "", 0, err
		}
		imported := prompts.ImportText(text)
		allLines = append(allLines, splitLines(imported)...)
	}
	if len(allLines) == 0 {
		return "", 0, fmt.Errorf("文件中没有可用提示词")
	}
	return strings.Join(allLines, "\n"), len(allLines), nil
}

func (a *App) applyImportedPrompts(platform, imported string, count int) {
	if a == nil || count <= 0 || strings.TrimSpace(imported) == "" {
		return
	}
	a.onWorkspace(platform, pageHome, func() {
		if a.batchMode != batchText {
			a.switchBatchMode(batchText, false)
		}
		a.promptPick = ""
		a.promptChoice = ""
		a.genPromptChoice = choosePromptLabel
		existing := strings.TrimSpace(a.prompts)
		if existing == "" {
			a.prompts = imported
		} else {
			a.prompts = existing + "\n" + imported
		}
		a.textPrompts = a.prompts
		a.freeformPrompts = a.prompts
		if a.store != nil {
			a.store.Set(platformSettingKey(platform, "prompts"), a.textPrompts)
			a.store.Set(platformSettingKey(platform, "prompts_freeform"), a.freeformPrompts)
			a.store.Set(platformSettingKey(platform, "prompt_pick"), "")
		}
	})
	a.persistWorkspaceFor(platform, pageHome)
	a.pushToast("success", fmt.Sprintf("已导入 %d 条提示词到文生图", count))
	a.emit("workbench")
}

func (a *App) importPromptTextFiles(platform string, paths []string) error {
	imported, count, err := readPromptTextFiles(paths)
	if err != nil {
		return err
	}
	a.applyImportedPrompts(platform, imported, count)
	return nil
}

func (a *App) importPromptTextFile(platform, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return a.importPromptTextFiles(platform, []string{path})
}

func (a *App) workspaceSnapshotIfPresent(platform string) (platformWorkspace, bool) {
	if platform == a.selectedPlatform() {
		a.syncPage()
		return a.capturePlatformWorkspace(), true
	}
	ws, ok := a.platformSpaces[platform]
	return ws, ok
}

func (a *App) syncPage() {
	if !isWorkPage(a.tab) || a.statePage == a.tab {
		return
	}
	if a.parked == nil {
		a.parked = map[int]pageState{}
	}
	a.parked[a.statePage] = a.capturePage()
	next, ok := a.parked[a.tab]
	if !ok {
		next = defaultPageStateFor(a.selectedPlatform(), a.tab)
	}
	a.statePage = a.tab
	a.restorePage(next)
}

func (a *App) stateOf(page int) pageState {
	return a.stateOfFor(a.selectedPlatform(), page)
}

func (a *App) stateOfFor(platform string, page int) pageState {
	a.syncPage()
	if platform == a.selectedPlatform() {
		if page == a.statePage {
			return a.capturePage()
		}
		if s, ok := a.parked[page]; ok {
			return s
		}
		return defaultPageStateFor(platform, page)
	}
	if ws, ok := a.platformSpaces[platform]; ok {
		return workspacePageState(ws, platform, page)
	}
	return defaultPageStateFor(platform, page)
}

func (a *App) onPage(page int, fn func()) {
	if fn == nil {
		return
	}
	a.syncPage()
	if page == a.statePage {
		fn()
		return
	}
	prev := a.statePage
	if a.parked == nil {
		a.parked = map[int]pageState{}
	}
	a.parked[prev] = a.capturePage()
	cur, ok := a.parked[page]
	if !ok {
		cur = defaultPageStateFor(a.selectedPlatform(), page)
	}
	a.statePage = page
	a.restorePage(cur)
	fn()
	a.parked[page] = a.capturePage()
	a.statePage = prev
	a.restorePage(a.parked[prev])
}

func (a *App) onWorkspace(platform string, page int, fn func()) {
	if fn == nil {
		return
	}
	if platform == "" || platform == a.selectedPlatform() {
		a.onPage(page, fn)
		return
	}
	a.syncPage()
	prevPlatform := a.selectedPlatform()
	prevDoubao, prevQianwen, prevQwenIntl, prevGemini := a.platDoubao, a.platQianwen, a.platQwenIntl, a.platGemini
	if a.platformSpaces == nil {
		a.platformSpaces = map[string]platformWorkspace{}
	}
	a.platformSpaces[prevPlatform] = a.capturePlatformWorkspace()
	targetWS, ok := a.platformSpaces[platform]
	if !ok {
		targetWS = defaultPlatformWorkspace(platform)
	}
	a.platDoubao = platform == "豆包"
	a.platQianwen = platform == "千问"
	a.platQwenIntl = platform == "千问国际"
	a.platGemini = platform == "Gemini"
	a.restorePlatformWorkspace(targetWS, platform)
	a.onPage(page, fn)
	a.platformSpaces[platform] = a.capturePlatformWorkspace()
	a.platDoubao, a.platQianwen, a.platQwenIntl, a.platGemini = prevDoubao, prevQianwen, prevQwenIntl, prevGemini
	a.restorePlatformWorkspace(a.platformSpaces[prevPlatform], prevPlatform)
}

func isWorkPage(tab int) bool {
	return tab == pageHome || tab == pageWash || tab == pageFission
}

func (a *App) washing() bool    { return a.statePage == pageWash }
func (a *App) fissioning() bool { return a.statePage == pageFission }

func pageLabel(page int) string {
	switch page {
	case pageWash:
		return "批量洗图"
	case pageFission:
		return "裂变图片"
	default:
		return "自由生图"
	}
}

func (a *App) noteWorkspacePage() {
	if a == nil {
		return
	}
	switch a.tab {
	case pageHome:
		a.workspacePage = store.PageGen
	case pageWash:
		a.workspacePage = store.PageWash
	case pageFission:
		a.workspacePage = store.PageFission
	}
}

func workspacePageKey(page int) string {
	if page == pageFission {
		return store.PageFission
	}
	if page == pageWash {
		return store.PageWash
	}
	return store.PageGen
}

func workspacePageKeyFor(platform string, page int) string {
	switch platform {
	case "千问":
		if page == pageFission {
			return store.PageFissionQianwen
		}
		if page == pageWash {
			return store.PageWashQianwen
		}
		return store.PageGenQianwen
	case "千问国际":
		if page == pageFission {
			return store.PageFissionQwenIntl
		}
		if page == pageWash {
			return store.PageWashQwenIntl
		}
		return store.PageGenQwenIntl
	case "Gemini":
		if page == pageFission {
			return store.PageFissionGemini
		}
		if page == pageWash {
			return store.PageWashGemini
		}
		return store.PageGenGemini
	}
	return workspacePageKey(page)
}

func (a *App) sessionID(page int) int64 {
	return a.sessionIDFor(a.selectedPlatform(), page)
}

func (a *App) sessionIDFor(platform string, page int) int64 {
	if platform != a.selectedPlatform() {
		ws := a.platformSpaces[platform]
		if page == pageFission {
			return ws.fissionSession
		}
		if page == pageWash {
			return ws.washSession
		}
		return ws.genSession
	}
	if page == pageFission {
		return a.fissionSession
	}
	if page == pageWash {
		return a.washSession
	}
	return a.genSession
}

func (a *App) setSessionID(page int, id int64) {
	a.setSessionIDFor(a.selectedPlatform(), page, id)
}

func (a *App) setSessionIDFor(platform string, page int, id int64) {
	if platform != a.selectedPlatform() {
		if a.platformSpaces == nil {
			a.platformSpaces = map[string]platformWorkspace{}
		}
		ws, ok := a.platformSpaces[platform]
		if !ok {
			ws = defaultPlatformWorkspace(platform)
		}
		if page == pageFission {
			ws.fissionSession = id
		} else if page == pageWash {
			ws.washSession = id
		} else {
			ws.genSession = id
		}
		a.platformSpaces[platform] = ws
		return
	}
	if page == pageFission {
		a.fissionSession = id
		return
	}
	if page == pageWash {
		a.washSession = id
		return
	}
	a.genSession = id
}

func (a *App) persistWorkspace(page int) bool {
	return a.persistWorkspaceFor(a.selectedPlatform(), page)
}

func (a *App) persistWorkspaceFor(platform string, page int) bool {
	if a == nil || a.store == nil {
		return false
	}
	if a.persistDeferral > 0 {
		if a.dirtyWorkspace == nil {
			a.dirtyWorkspace = map[workspaceSlot]struct{}{}
		}
		a.dirtyWorkspace[workspaceSlot{platform: platform, page: page}] = struct{}{}
		return true
	}
	return a.writeWorkspaceFor(platform, page)
}

func (a *App) writeDirtyWorkspaces() {
	if a == nil || a.store == nil || len(a.dirtyWorkspace) == 0 {
		return
	}
	slots := make([]workspaceSlot, 0, len(a.dirtyWorkspace))
	for sl := range a.dirtyWorkspace {
		slots = append(slots, sl)
	}
	clear(a.dirtyWorkspace)
	for _, sl := range slots {
		a.writeWorkspaceFor(sl.platform, sl.page)
	}
}

func (a *App) stopRunPersistTimer() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.runPersistTimer != nil {
		a.runPersistTimer.Stop()
		a.runPersistTimer = nil
	}
	a.mu.Unlock()
}

func (a *App) ensureRunPersistTimer() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.quitting || a.runPersistTimer != nil {
		a.mu.Unlock()
		return
	}
	wait := runPersistCoalesceWindow
	if wait <= 0 {
		wait = time.Millisecond
	}
	var timer *time.Timer
	timer = time.AfterFunc(wait, func() {
		a.mu.Lock()
		if a.runPersistTimer == timer {
			a.runPersistTimer = nil
		}
		quitting := a.quitting
		a.mu.Unlock()
		if !quitting {
			a.postUI(a.flushRunWorkspaceUpdate)
		}
	})
	a.runPersistTimer = timer
	a.mu.Unlock()
}

func (a *App) scheduleRunWorkspaceUpdate(page int) {
	a.scheduleRunWorkspaceUpdateFor(a.selectedPlatform(), page)
}

func (a *App) scheduleRunWorkspaceUpdateFor(platform string, page int) {
	if a == nil {
		return
	}
	if a.runPersistDirty == nil {
		a.runPersistDirty = map[workspaceSlot]struct{}{}
	}
	a.runPersistDirty[workspaceSlot{platform: platform, page: page}] = struct{}{}
	a.runEmitPending = true
	if a.inFlush.Load() {
		a.flushEmitted.Store(true)
	}
}

func (a *App) flushRunWorkspaceUpdate() {
	if a == nil {
		return
	}
	a.stopRunPersistTimer()
	if a.isQuitting() {
		return
	}
	if len(a.runPersistDirty) > 0 {
		slots := make([]workspaceSlot, 0, len(a.runPersistDirty))
		for sl := range a.runPersistDirty {
			slots = append(slots, sl)
		}
		slices.SortFunc(slots, func(x, y workspaceSlot) int {
			if x.platform != y.platform {
				return strings.Compare(x.platform, y.platform)
			}
			return x.page - y.page
		})
		clear(a.runPersistDirty)
		for _, sl := range slots {
			a.persistWorkspaceFor(sl.platform, sl.page)
		}
	}
	if a.runEmitPending {
		a.runEmitPending = false
		a.emit("workbench")
	}
}

func (a *App) cancelRunWorkspaceUpdate() {
	if a == nil {
		return
	}
	a.stopRunPersistTimer()
	clear(a.runPersistDirty)
	a.runEmitPending = false
}

func (a *App) writeWorkspace(page int) bool {
	return a.writeWorkspaceFor(a.selectedPlatform(), page)
}

func (a *App) writeWorkspaceFor(platform string, page int) bool {
	a.workspaceWrites++
	st := a.stateOfFor(platform, page)
	sess := store.Session{
		ID:       a.sessionIDFor(platform, page),
		Page:     workspacePageKeyFor(platform, page),
		Prompts:  st.prompts,
		Sources:  a.washSourcesForSave(page, st),
		Progress: st.progress,
		ProgText: st.progText,
		Updated:  time.Now().Unix(),
	}
	items := a.tasksForSession(page, a.tasksToSaveFor(platform, page))
	id, err := a.store.SaveActive(sess, items)
	if err != nil {
		logging.Log.Warning("保存工作区失败: %v", err)
		return false
	}
	a.setSessionIDFor(platform, page, id)
	return true
}

func (a *App) washSourcesForSave(page int, st pageState) []store.SessionSource {
	if page != pageWash {
		out := make([]store.SessionSource, 0, len(st.sources))
		for _, si := range st.sources {
			out = append(out, store.SessionSource{Path: si.Path})
		}
		return out
	}
	out := make([]store.SessionSource, 0, len(st.sources))
	for _, si := range st.sources {
		out = append(out, store.SessionSource{
			Path:   si.Path,
			Washed: si.Washed,
			Output: si.WashOutput,
		})
	}
	return out
}

func (a *App) restoreWorkspace(page int) {
	a.restoreWorkspaceFor(a.selectedPlatform(), page)
}

func (a *App) restoreWorkspaceFor(platform string, page int) {
	if a == nil || a.store == nil {
		return
	}
	sess, items, ok, err := a.store.ActiveSession(workspacePageKeyFor(platform, page))
	if err != nil || !ok {
		return
	}
	a.applySessionFor(platform, page, sess, items)
}

func (a *App) applySession(page int, sess store.Session, items []store.SessionTask) {
	a.applySessionFor(a.selectedPlatform(), page, sess, items)
}

func (a *App) applySessionFor(platform string, page int, sess store.Session, items []store.SessionTask) {
	a.setSessionIDFor(platform, page, sess.ID)
	if page == pageWash {
		a.onWorkspace(platform, page, func() { a.applyWashSession(sess, items) })
		return
	}
	q := queueFromSession(items)
	a.onWorkspace(platform, page, func() {
		a.prompts = sess.Prompts
		if page == pageFission {
			a.fissionPromptChoice = choosePromptLabel
			if !a.skipImages && strings.TrimSpace(a.prompts) == "" {
				a.prompts = DefaultFissionPrompt
			}
		}
		if len(sess.Sources) > 0 {
			a.sources = make([]sourceImage, 0, len(sess.Sources))
			for _, src := range sess.Sources {
				if page == pageHome {
					if hash, ok := hashImageFile(src.Path); ok {
						a.sources = append(a.sources, sourceImage{Path: src.Path, Hash: hash})
						continue
					}
				}
				a.sources = append(a.sources, sourceImage{Path: src.Path})
			}
		}
		a.srcSel = -1
		a.queue = q
		a.progress = sess.Progress
		a.progText = sess.ProgText
	})
}

func (a *App) applyWashSession(sess store.Session, items []store.SessionTask) {
	a.prompts = sess.Prompts
	if strings.TrimSpace(a.prompts) == "" {
		a.prompts = a.defaultWashPrompt()
	}
	a.washPromptChoice = choosePromptLabel
	if len(sess.Sources) > 0 {
		a.sources = make([]sourceImage, 0, len(sess.Sources))
		for _, src := range sess.Sources {
			a.sources = append(a.sources, sourceImage{
				Path:       src.Path,
				Washed:     src.Washed,
				WashOutput: src.Output,
			})
		}
	}
	a.srcSel = -1
	a.queue = queueFromSession(items)
	a.applyWashQueue(a.queue)
	a.progress = sess.Progress
	a.progText = sess.ProgText
}

func (a *App) tasksToSave(page int) []tasks.Task {
	return a.tasksToSaveFor(a.selectedPlatform(), page)
}

func (a *App) tasksToSaveFor(platform string, page int) []tasks.Task {
	if a.running && a.runPage == page && (a.runPlatform == "" || a.runPlatform == platform) && a.engine != nil {
		if view := a.engine.View(); view != nil {
			items := view.Snapshot()
			if page == pageFission && len(a.fissionRetryBase) > 0 {
				return mergeFissionRetry(a.fissionRetryBase, items)
			}
			return items
		}
	}
	return a.stateOfFor(platform, page).queue
}

func (a *App) tasksForSession(page int, items []tasks.Task) []store.SessionTask {
	out := make([]store.SessionTask, 0, len(items))
	for ord, t := range items {
		outputs := joinPaths(t.Results)
		source := taskSource(t)
		key := stampKey(page, ord, string(t.Status), source, outputs)
		finishedAt := a.stampAt(key)
		acct := a.sessionAccountName(t)
		out = append(out, store.SessionTask{
			Ord:          ord,
			Platform:     t.Platform,
			AccountID:    t.BrowserID,
			AccountName:  acct,
			Prompt:       t.Prompt,
			Source:       source,
			Status:       string(t.Status),
			Outputs:      outputs,
			Error:        t.Error,
			ElapsedMS:    t.Elapsed.Milliseconds(),
			At:           finishedAt,
			Undispatched: t.Undispatched,
			QualityNote:  t.QualityNote,
		})
	}
	a.rememberStamps(page, out)
	return out
}

func (a *App) sessionAccountName(task tasks.Task) string {
	if name := strings.TrimSpace(task.AccountName); name != "" {
		return name
	}
	if task.BrowserID > 0 {
		for _, b := range a.configuredBrowsers() {
			if b.ID == task.BrowserID {
				return accountLabel(b)
			}
		}
	}
	return ""
}

func queueFromSession(items []store.SessionTask) []tasks.Task {
	out := make([]tasks.Task, 0, len(items))
	for i, item := range items {
		t := tasks.Task{
			Index:        i + 1,
			Platform:     item.Platform,
			BrowserID:    item.AccountID,
			AccountName:  item.AccountName,
			Prompt:       item.Prompt,
			Status:       tasks.Status(item.Status),
			Results:      splitLines(item.Outputs),
			Error:        item.Error,
			Elapsed:      time.Duration(item.ElapsedMS) * time.Millisecond,
			Undispatched: item.Undispatched,
			QualityNote:  item.QualityNote,
		}
		if item.Source != "" {
			t.Images = []string{item.Source}
		}
		out = append(out, t)
	}
	return out
}

func stampKey(page, ord int, status, source, outputs string) string {
	return fmt.Sprintf("%d:%d:%s:%s:%s", page, ord, status, source, outputs)
}

func (a *App) stampAt(key string) int64 {
	if a.stamps != nil {
		if ts, ok := a.stamps[key]; ok {
			return ts
		}
	}
	return time.Now().Unix()
}

func (a *App) rememberStamps(page int, items []store.SessionTask) {
	if a.stamps == nil {
		a.stamps = map[string]int64{}
	}
	for _, it := range items {
		key := stampKey(page, it.Ord, it.Status, it.Source, it.Outputs)
		a.stamps[key] = it.At
	}
}

// ---- Wash & Fission helpers ----

func (a *App) defaultWashPrompt() string {
	if a.selectedPlatform() == "豆包" {
		return prompts.DoubaoWashPrompt
	}
	return DefaultWashPrompt
}

func (a *App) washPrompt() string {
	p := strings.TrimSpace(a.prompts)
	if p == "" {
		return a.defaultWashPrompt()
	}
	return p
}

func (a *App) fissionPrompt() string {
	p := strings.TrimSpace(a.prompts)
	if p == "" {
		return DefaultFissionPrompt
	}
	return p
}

func (a *App) washSoloPaths() []string {
	st := a.stateOf(pageWash)
	lastModeration := make(map[string]bool, len(st.queue))
	for _, item := range st.queue {
		if len(item.Images) == 1 {
			lastModeration[filepath.Clean(item.Images[0])] = tasks.ClassifyFailure(item.Status, item.Error).Class == tasks.FailModeration
		}
	}
	var paths []string
	for _, si := range st.sources {
		if !si.Washed && si.Path != "" && lastModeration[filepath.Clean(si.Path)] {
			paths = append(paths, si.Path)
		}
	}
	return paths
}

func (a *App) washPendingPaths() []string {
	var paths []string
	for _, si := range a.sources {
		if !si.Washed && si.Path != "" {
			paths = append(paths, si.Path)
		}
	}
	return paths
}

func (a *App) beginWashRun() {
	for i := range a.sources {
		if !a.sources[i].Washed {
			a.sources[i].Washing = true
		}
	}
}

func (a *App) applyWashTask(task tasks.Task) {
	src := taskSource(task)
	if src == "" {
		return
	}
	for i := range a.sources {
		if filepath.Clean(a.sources[i].Path) != filepath.Clean(src) {
			continue
		}
		switch {
		case isTaskActiveStatus(task.Status):
			a.sources[i].Washing = true
		case task.Status == tasks.StatusSuccess:
			a.sources[i].Washing = false
			a.sources[i].Washed = true
			if len(task.Results) > 0 {
				a.sources[i].WashOutput = task.Results[0]
			}
		default:
			a.sources[i].Washing = false
		}
	}
}

func (a *App) applyWashQueue(queue []tasks.Task) {
	byPath := map[string]tasks.Task{}
	for _, t := range queue {
		if src := taskSource(t); src != "" {
			byPath[filepath.Clean(src)] = t
		}
	}
	for i := range a.sources {
		t, ok := byPath[filepath.Clean(a.sources[i].Path)]
		if !ok {
			a.sources[i].Washing = false
			continue
		}
		a.applyWashTask(t)
	}
}

func mergeFissionRetry(base, retry []tasks.Task) []tasks.Task {
	out := append([]tasks.Task(nil), base...)
	for _, next := range retry {
		for i := range out {
			if out[i].Platform == next.Platform && out[i].Prompt == next.Prompt {
				next.Index = out[i].Index
				out[i] = next
				break
			}
		}
	}
	return out
}

func sameFissionSource(a, b string) bool {
	return filepath.Clean(strings.TrimSpace(a)) == filepath.Clean(strings.TrimSpace(b))
}

func (a *App) promptExtras(page int, parts *promptParts) string {
	extra := ""
	if parts != nil {
		extra = strings.TrimSpace(parts.text())
	}
	if page == pageWash {
		style := washStyleInstruction(a.washStyle)
		if style != "" && extra != "" {
			return extra + "\n" + style
		}
		if style != "" {
			return style
		}
	}
	return extra
}

// ---- Platform & Quota helpers ----

func (a *App) selectedPlatform() string {
	switch {
	case a.platDoubao:
		return "豆包"
	case a.platQianwen:
		return "千问"
	case a.platQwenIntl:
		return "千问国际"
	case a.platGemini:
		return "Gemini"
	default:
		return "豆包"
	}
}

func (a *App) setSinglePlatform(platform string) bool {
	switch platform {
	case "豆包", "千问", "千问国际", "Gemini":
		hadPlatform := a.platDoubao || a.platQianwen || a.platQwenIntl || a.platGemini
		prevPlatform := a.selectedPlatform()
		if !hadPlatform {
			a.platDoubao = platform == "豆包"
			a.platQianwen = platform == "千问"
			a.platQwenIntl = platform == "千问国际"
			a.platGemini = platform == "Gemini"
			a.platformAuto = false
			return true
		}
		if prevPlatform == platform {
			a.platformAuto = false
			return true
		}
		a.syncPage()
		if a.platformSpaces == nil {
			a.platformSpaces = map[string]platformWorkspace{}
		}
		a.platformSpaces[prevPlatform] = a.capturePlatformWorkspace()
		a.platDoubao = platform == "豆包"
		a.platQianwen = platform == "千问"
		a.platQwenIntl = platform == "千问国际"
		a.platGemini = platform == "Gemini"
		a.platformAuto = false
		a.galleryOn = false
		a.previewOn = false
		a.previewPath = ""
		nextWS, ok := a.platformSpaces[platform]
		if !ok {
			nextWS = defaultPlatformWorkspace(platform)
		}
		a.restorePlatformWorkspace(nextWS, platform)
		return true
	default:
		return false
	}
}

func (a *App) selectedPlatforms() []string {
	return []string{a.selectedPlatform()}
}

func (a *App) activePlatforms() []string {
	return a.selectedPlatforms()
}

func (a *App) toggleStage(platform string) {
	if a.running {
		return
	}
	a.setSinglePlatform(platform)
}

func (a *App) stageEnabled(platform string) bool {
	switch platform {
	case "豆包":
		return a.platDoubao
	case "千问":
		return a.platQianwen
	case "千问国际":
		return a.platQwenIntl
	case "Gemini":
		return a.platGemini
	}
	return false
}

func (a *App) shownDailyCap() int {
	return clampInt(int(a.dailyCap), 0, 200)
}

func (a *App) shownDailyCapFor(platform string) int {
	switch platform {
	case "千问":
		if a.dailyCapQianwenSet {
			return clampInt(int(a.dailyCapQianwen), 0, 200)
		}
	case "千问国际":
		if a.dailyCapQwenIntlSet {
			return clampInt(int(a.dailyCapQwenIntl), 0, 200)
		}
	case "Gemini":
		if a.dailyCapGeminiSet {
			return clampInt(int(a.dailyCapGemini), 0, 200)
		}
	}
	return a.shownDailyCap()
}

func (a *App) dailyCaps() map[string]int {
	return map[string]int{
		"豆包":     a.shownDailyCap(),
		"千问":     a.shownDailyCapFor("千问"),
		"千问国际":   a.shownDailyCapFor("千问国际"),
		"Gemini": a.shownDailyCapFor("Gemini"),
	}
}

func (a *App) concurrencyAutoFor(platform string) bool {
	switch platform {
	case "千问":
		return a.concurrencyAutoQianwen
	case "千问国际":
		return a.concurrencyAutoQwenIntl
	case "Gemini":
		return a.concurrencyAutoGemini
	default:
		return a.concurrencyAuto
	}
}

func (a *App) concurrencyMaxFor(platform string) int {
	raw := a.concurrencyMax
	switch platform {
	case "千问":
		raw = a.concurrencyMaxQianwen
	case "千问国际":
		raw = a.concurrencyMaxQwenIntl
	case "Gemini":
		raw = a.concurrencyMaxGemini
	}
	return clampInt(int(raw), tasks.PlatformInFlight, tasks.MaxPlatformInFlight)
}

func (a *App) taskConcurrencyFor(platform string) int {
	if !a.concurrencyAutoFor(platform) {
		return 1
	}
	if maxN := a.concurrencyMaxFor(platform); maxN > tasks.PlatformInFlight {
		return maxN
	}
	return 0
}

func (a *App) taskConcurrency() int {
	return a.taskConcurrencyFor(a.selectedPlatform())
}

// ---- Workbench task execution ----

func (a *App) workbenchPlan(rows []store.Browser) WorkbenchPlan {
	promptList := a.taskPromptList()
	var sources []string
	hasImages := false
	hasPrompts := false
	switch a.statePage {
	case pageWash:
		sources = a.washPendingPaths()
		hasImages = len(sources) > 0
	case pageFission:
		if a.skipImages {
			hasPrompts = len(promptList) == 4
		} else {
			sources = a.batchImages()
			hasImages = len(sources) == 1
		}
	default:
		if a.batchMode == batchImage {
			sources = a.batchImages()
			hasImages = len(sources) > 0
		} else {
			hasPrompts = len(promptList) > 0 || strings.TrimSpace(a.promptExtras(pageHome, a.currentPromptParts())) != ""
		}
	}
	var snaps []AccountSnap
	for _, b := range rows {
		snaps = append(snaps, AccountSnap{
			Platform:  b.Platform,
			LoggedIn:  b.IsLoggedIn,
			Disabled:  b.Disabled,
			Health:    b.Health,
			CoolUntil: b.CoolUntil,
		})
	}
	return PlanWorkbench(WorkbenchInput{
		Running:      a.running,
		HasImages:    hasImages,
		HasPrompts:   hasPrompts,
		AutoPlatform: false,
		OnlyPlatform: a.selectedPlatform(),
		WantDoubao:   a.platDoubao,
		WantQianwen:  a.platQianwen,
		WantQwenIntl: a.platQwenIntl,
		WantGemini:   a.platGemini,
		Accounts:     snaps,
		Now:          time.Now(),
	})
}

func (a *App) startTasks() {
	if a.renameUpdateActive() || len(a.browserPending) > 0 {
		return
	}
	plan := a.workbenchPlan(a.configuredBrowsers())
	if plan.Action != StartGenerate {
		return
	}
	paths := a.batchImages()
	if a.washing() {
		paths = a.washPendingPaths()
	} else if a.fissioning() {
		if a.skipImages {
			paths = nil
		} else if len(paths) > 1 {
			paths = paths[:1]
		}
	} else if a.statePage == pageHome && a.batchMode == batchText {
		paths = nil
	}
	a.runQueue(plan.Platforms, a.taskPromptList(), paths, nil)
}

func (a *App) retryTasks(items []tasks.Task) {
	if a.running || len(items) == 0 || a.engine == nil {
		return
	}
	var paths []string
	if a.fissioning() {
		hasImageTask := false
		for _, task := range items {
			if len(task.Images) > 0 {
				hasImageTask = true
				break
			}
		}
		if hasImageTask {
			if len(a.sources) != 1 {
				a.progText = "请先添加该结果对应的母图"
				return
			}
			for _, task := range items {
				if len(task.Images) != 1 || !sameFissionSource(task.Images[0], a.sources[0].Path) {
					a.progText = "请切换到该结果对应的母图再重新生成"
					return
				}
			}
			paths = []string{a.sources[0].Path}
		}
	}
	var platforms []string
	seenPlat := map[string]bool{}
	var promptList []string
	for _, t := range items {
		if !seenPlat[t.Platform] {
			seenPlat[t.Platform] = true
			platforms = append(platforms, t.Platform)
		}
		promptList = append(promptList, t.Prompt)
	}
	a.runQueue(platforms, promptList, paths, items)
}

func (a *App) runQueue(platforms, promptList, paths []string, retry []tasks.Task) {
	if a.engine == nil || a.running || a.renameUpdateActive() || len(a.browserPending) > 0 {
		return
	}
	if retry == nil && !a.fissioning() {
		if err := a.currentPromptParts().validate(); err != nil {
			a.progText = err.Error()
			logging.Log.Errorf("%s", a.progText)
			return
		}
	}
	for _, platform := range platforms {
		if platform == "豆包" || platform == "千问" || platform == "千问国际" || platform == "Gemini" {
			if err := a.outputSizeFor(platform).ValidateFor(platform); err != nil {
				a.progText = platform + "输出尺寸：" + err.Error()
				logging.Log.Errorf("%s", a.progText)
				return
			}
		}
	}
	rows := a.configuredBrowsers()
	for _, platform := range platforms {
		if platform != "豆包" && platform != "千问" && platform != "千问国际" && platform != "Gemini" {
			continue
		}
		kinds := browser.AccountEngines(rows, platform, a.browserBackend)
		ready := false
		for _, kind := range kinds {
			if _, ready = browser.InstalledRuntime(browser.ConfigDir(), kind); ready {
				break
			}
		}
		if !ready {
			a.refreshDetected()
			if !a.runtimeBusy && browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, a.browserBackend) {
				a.runtimeStatus = "请先安装 " + browser.CanonicalBackend(a.browserBackend) + "，再返回任务页重试"
			}
			a.tab = pageBrowser
			a.emit("shell", "browsers")
			return
		}
	}
	dir, err := EnsureOutputDir(DefaultOutputDir(a.outputPath, mustWd()))
	if err != nil {
		a.progText = HumanReason(err.Error())
		logging.Log.Errorf("创建输出文件夹失败: %v", err)
		return
	}
	if strings.TrimSpace(a.outputPath) == "" {
		a.outputPath = dir
	}
	a.saveSettings()
	opts := a.taskOptions(platforms, promptList, paths)
	opts.OutputPath = dir
	page := a.statePage
	previousView := a.engine.View()
	previous := a.stateOf(page)
	a.fissionRetryBase = nil
	if page == pageFission && retry != nil {
		a.fissionRetryBase = append([]tasks.Task(nil), previous.queue...)
		retrying := make(map[string]bool, len(retry))
		for _, item := range retry {
			retrying[item.Platform+"\x00"+item.Prompt] = true
		}
		for _, item := range previous.queue {
			if item.Status == tasks.StatusSuccess && len(item.Results) == 1 && !retrying[item.Platform+"\x00"+item.Prompt] {
				opts.FissionKeepPaths = append(opts.FissionKeepPaths, item.Results[0])
			}
		}
	}
	previousRunPage, previousRunPlatform, previousUseRunView := a.runPage, a.runPlatform, a.useRunView
	runPlatform := a.selectedPlatform()
	a.runPage = page
	a.runPlatform = runPlatform
	a.running = true
	a.useRunView = false
	a.resultsDoneOpen = true
	a.rewashUndo = nil
	if page == pageWash {
		a.onPage(page, func() { a.beginWashRun() })
	}
	if retry != nil {
		pending := tasks.RebuildRetry(retry)
		if page == pageFission {
			pending = mergeFissionRetry(previous.queue, pending)
		}
		a.onPage(page, func() {
			a.queue = pending
			a.progress = 0
			if len(pending) > 0 {
				a.progText = fmt.Sprintf("0 / %d", len(pending))
			} else {
				a.progText = ""
			}
		})
	} else {
		a.progress = 0
		a.progText = ""
	}
	if !a.persistWorkspaceFor(runPlatform, page) {
		a.onPage(page, func() {
			a.queue, a.progress = previous.queue, previous.progress
			a.progText = "保存任务记录失败，未开始任务，请检查配置数据库"
		})
		a.running = false
		a.fissionRetryBase = nil
		a.runPage, a.runPlatform, a.useRunView = previousRunPage, previousRunPlatform, previousUseRunView
		return
	}
	a.engine.SetSolver(captcha.Build(a.captchaConfig()))
	a.emit("workbench", "shell")
	go func() {
		onTask := func(task tasks.Task) {
			a.postUI(func() {
				a.useRunView = true
				if page == pageWash {
					a.onWorkspace(runPlatform, page, func() { a.applyWashTask(task) })
				}
				a.scheduleRunWorkspaceUpdateFor(runPlatform, page)
			})
		}
		onProgress := func(p tasks.Progress) {
			a.postUI(func() {
				a.useRunView = true
				a.onWorkspace(runPlatform, page, func() {
					if p.Total > 0 {
						a.progress = float64(p.Done) / float64(p.Total)
					}
					a.progText = fmt.Sprintf("%d / %d", p.Done, p.Total)
				})
				a.scheduleRunWorkspaceUpdateFor(runPlatform, page)
			})
		}
		var runErr error
		if retry != nil {
			runErr = a.engine.Retry(context.Background(), opts, retry, onTask, onProgress)
		} else {
			runErr = a.engine.Run(context.Background(), opts, onTask, onProgress)
		}
		var checked []tasks.Task
		if page == pageFission {
			if view := a.engine.View(); view != nil && view != previousView {
				checked = view.Snapshot()
				if retry != nil {
					checked = mergeFissionRetry(previous.queue, checked)
				}
				checked = tasks.ReviewFissionResults(checked, retry)
			}
		}
		a.postUI(func() {
			a.running = false
			a.useRunView = false
			a.fissionRetryBase = nil
			if a.browserDrawer != nil && a.manager != nil && !a.manager.IsRunning(a.browserDrawer.row.ID) {
				a.closeBrowserDrawer()
			}
			if a.manager != nil {
				for i := range a.browserRows {
					a.browserRows[i].IsRunning = a.manager.IsRunning(a.browserRows[i].ID)
				}
			}
			a.onWorkspace(runPlatform, page, func() {
				if page == pageFission && checked != nil {
					a.queue = checked
				} else if view := a.engine.View(); view != nil && view != previousView {
					a.queue = view.Snapshot()
				} else {
					a.queue, a.progress = previous.queue, previous.progress
				}
				if page == pageWash {
					a.applyWashQueue(a.queue)
				}
				if runErr != nil {
					a.progText = HumanReason(runErr.Error())
					logging.Log.Errorf("%s任务终止: %v", pageLabel(page), runErr)
				} else {
					a.progText = taskResultSummary(a.queue)
				}
			})
			a.cancelRunWorkspaceUpdate()
			a.persistWorkspaceFor(runPlatform, page)
			a.maybeInstallPending()
			a.emit("workbench", "shell", "browsers")
		})
	}()
}

func (a *App) batchImages() []string {
	if len(a.sources) == 0 {
		return nil
	}
	paths := make([]string, 0, len(a.sources))
	for _, si := range a.sources {
		paths = append(paths, si.Path)
	}
	return paths
}

func (a *App) addLoaded(page int, loaded []sourceImage) {
	a.addLoadedFor(a.selectedPlatform(), page, loaded)
}

func (a *App) addLoadedFor(platform string, page int, loaded []sourceImage) {
	if len(loaded) == 0 {
		return
	}
	switchedToImage := false
	a.onWorkspace(platform, page, func() {
		if page == pageFission {
			a.sources = loaded[:1]
			a.srcSel = 0
			a.skipImages = false
			if a.store != nil {
				a.store.SetInt(platformSettingKey(platform, "fission_skip_images"), 0)
			}
		} else {
			a.appendSources(loaded, page == pageWash)
			if page == pageHome {
				switchedToImage = a.switchBatchMode(batchImage, true)
				if !switchedToImage && a.store != nil {
					a.store.Set(platformSettingKey(platform, "batch_mode"), "image")
				}
			}
		}
	})
	a.persistWorkspaceFor(platform, page)
	if switchedToImage {
		a.pushToast("info", fmt.Sprintf("已导入 %d 张参考图，已切换至图生图", len(loaded)))
	}
	a.emit("workbench")
}

func taskResultSummary(queue []tasks.Task) string {
	tally := TallyTasks(queue)
	summary := fmt.Sprintf("成功 %d · 失败 %d · 待验证 %d", tally.Done, tally.Failed, tally.Needs)
	if tally.Review > 0 {
		summary += fmt.Sprintf(" · 需核对 %d", tally.Review)
	}
	return summary + fmt.Sprintf(" · 未完成 %d", tally.Incomplete())
}

// ---- Result grouping, lanes & status helpers ----

type resultTile struct {
	key  string
	task tasks.Task
	path string
}

type resultRow struct {
	key     string
	header  bool
	issue   bool
	title   string
	summary string
	tiles   []resultTile
}

type resultAccountBucket struct {
	key       string
	title     string
	running   int
	awaitUser int
	tiles     []resultTile
}

func (a *App) currentQueueItems() []tasks.Task {
	if a.running && a.runPage == a.statePage && (a.runPlatform == "" || a.runPlatform == a.selectedPlatform()) && a.useRunView && a.engine != nil {
		if view := a.engine.View(); view != nil {
			items := view.Snapshot()
			if a.statePage == pageFission && len(a.fissionRetryBase) > 0 {
				return mergeFissionRetry(a.fissionRetryBase, items)
			}
			return items
		}
	}
	return a.queue
}

func (a *App) queueItems() []tasks.Task {
	return a.currentQueueItems()
}

func (a *App) classifyResultRows(items []tasks.Task, cols int) (activeRows, issueRows []resultRow, doneTiles []resultTile, doneTasks int) {
	return a.classifyResultRowsWithBrowsers(items, cols, a.configuredBrowsers())
}

func (a *App) classifyResultRowsWithBrowsers(items []tasks.Task, cols int, browsers []store.Browser) (activeRows, issueRows []resultRow, doneTiles []resultTile, doneTasks int) {
	if cols < 1 {
		cols = 1
	}
	byID := make(map[int64]string, len(browsers))
	for _, b := range browsers {
		byID[b.ID] = accountLabel(b)
	}
	var order []string
	buckets := map[string]*resultAccountBucket{}
	var issueTiles []resultTile
	for pos, task := range items {
		switch {
		case isTaskActiveStatus(task.Status):
			key, title := a.resultAccountGroup(task, byID)
			b := buckets[key]
			if b == nil {
				b = &resultAccountBucket{key: key, title: title}
				buckets[key] = b
				order = append(order, key)
			}
			b.running++
			if task.Status == tasks.StatusAwaitUser {
				b.awaitUser++
			}
			b.tiles = append(b.tiles, taskActiveTiles(task, pos)...)
		case task.Status == tasks.StatusSuccess:
			doneTasks++
			doneTiles = append(doneTiles, taskDoneTiles(task, pos)...)
		case task.Status == tasks.StatusFailed || task.Status == tasks.StatusNeedsUser || task.Status == tasks.StatusNeedsReview || (task.Status == tasks.StatusSkipped && task.Worked()):
			issueTiles = append(issueTiles, taskIssueTile(task, pos))
		}
	}
	if len(buckets) > 0 {
		ordered := make([]string, 0, len(buckets))
		added := make(map[string]bool, len(buckets))
		for _, b := range browsers {
			key := fmt.Sprintf("acct:%d", b.ID)
			if buckets[key] != nil && !added[key] {
				ordered = append(ordered, key)
				added[key] = true
			}
		}
		for _, key := range order {
			if !added[key] {
				ordered = append(ordered, key)
				added[key] = true
			}
		}
		for _, key := range ordered {
			b := buckets[key]
			summary := fmt.Sprintf("进行中 %d", b.running)
			if b.awaitUser > 0 {
				summary += fmt.Sprintf(" · 需要人工验证 %d", b.awaitUser)
			}
			activeRows = append(activeRows, resultRow{key: "h:" + key, header: true, title: b.title, summary: summary})
			for i := 0; i < len(b.tiles); i += cols {
				end := min(i+cols, len(b.tiles))
				activeRows = append(activeRows, resultRow{key: fmt.Sprintf("r:%s:%d", key, i), tiles: b.tiles[i:end]})
			}
		}
	}
	if len(issueTiles) > 0 {
		issueRows = append(issueRows, resultRow{key: "h:issues", header: true, title: "待处理", summary: fmt.Sprintf("%d 项", len(issueTiles))})
		for _, tile := range issueTiles {
			issueRows = append(issueRows, resultRow{key: "i:" + tile.key, issue: true, tiles: []resultTile{tile}})
		}
	}
	return activeRows, issueRows, doneTiles, doneTasks
}

func (a *App) resultAccountGroup(task tasks.Task, byID map[int64]string) (key, title string) {
	if task.BrowserID > 0 {
		title = strings.TrimSpace(byID[task.BrowserID])
		if title == "" {
			title = strings.TrimSpace(task.AccountName)
		}
		if title == "" {
			if plat := strings.TrimSpace(task.Platform); plat != "" {
				title = fmt.Sprintf("%s账号%d", plat, task.BrowserID)
			} else {
				title = fmt.Sprintf("账号%d", task.BrowserID)
			}
		}
		return fmt.Sprintf("acct:%d", task.BrowserID), title
	}
	if name := strings.TrimSpace(task.AccountName); name != "" {
		return "acct-name:" + name, name
	}
	plat := strings.TrimSpace(task.Platform)
	if plat == "" {
		return "plat:", "工作账号"
	}
	return "plat:" + plat, plat + "账号"
}

func taskActiveTiles(task tasks.Task, pos int) []resultTile {
	path := ""
	if len(task.Results) > 0 {
		path = task.Results[0]
	}
	return []resultTile{{key: fmt.Sprintf("a-%d-%d-%s", task.Index, pos, task.Status), task: task, path: path}}
}

func taskIssueTile(task tasks.Task, pos int) resultTile {
	path := ""
	if len(task.Results) > 0 {
		path = task.Results[0]
	}
	return resultTile{key: fmt.Sprintf("e-%d-%d-%s", task.Index, pos, task.Status), task: task, path: path}
}

func taskDoneTiles(task tasks.Task, pos int) []resultTile {
	if len(task.Results) == 0 {
		return []resultTile{{key: fmt.Sprintf("d-%d-%d", task.Index, pos), task: task}}
	}
	tiles := make([]resultTile, 0, len(task.Results))
	for i, path := range task.Results {
		tiles = append(tiles, resultTile{key: fmt.Sprintf("p-%d-%d-%d", task.Index, pos, i), task: task, path: path})
	}
	return tiles
}

func isTaskActiveStatus(status tasks.Status) bool {
	return status == tasks.StatusRunning || status == tasks.StatusRetry || status == tasks.StatusAwaitUser
}

func taskElapsedText(task tasks.Task, now time.Time) string {
	if isTaskActiveStatus(task.Status) {
		if task.StartedAt.IsZero() {
			return "耗时 0秒"
		}
		d := now.Sub(task.StartedAt)
		if d < 0 {
			d = 0
		}
		return "耗时 " + formatElapsedDuration(d, false)
	}
	if task.Elapsed > 0 {
		return "耗时 " + formatElapsedDuration(task.Elapsed, true)
	}
	return "耗时 --"
}

func formatElapsedDuration(d time.Duration, round bool) string {
	if d < 0 {
		d = 0
	}
	var sec int
	if round {
		sec = int((d + time.Second/2) / time.Second)
		if sec < 1 {
			sec = 1
		}
	} else {
		sec = int(d / time.Second)
	}
	if sec < 60 {
		return fmt.Sprintf("%d秒", sec)
	}
	return fmt.Sprintf("%d分%02d秒", sec/60, sec%60)
}

func taskSource(task tasks.Task) string {
	if len(task.Images) == 0 {
		return ""
	}
	return strings.TrimSpace(task.Images[0])
}

func tileTitle(tile resultTile) string {
	if tile.path != "" {
		return filepath.Base(tile.path)
	}
	if src := taskSource(tile.task); src != "" {
		return filepath.Base(src)
	}
	if line := oneLine(tile.task.Prompt); line != "" {
		return shortLine(line, 28)
	}
	return "未命名"
}

func tileStatus(task tasks.Task) string {
	switch task.Status {
	case tasks.StatusRunning, tasks.StatusRetry:
		return "进行中"
	case tasks.StatusSuccess:
		return "完成"
	case tasks.StatusFailed:
		return "失败"
	case tasks.StatusSkipped:
		return "已停止"
	case tasks.StatusPending:
		return "排队中"
	case tasks.StatusNeedsUser:
		return "待验证"
	case tasks.StatusAwaitUser:
		return "需要人工验证"
	default:
		if task.Status == "" {
			return "排队中"
		}
		return string(task.Status)
	}
}

func tileFailure(task tasks.Task) (reason, action string, retry []tasks.Task) {
	switch task.Status {
	case tasks.StatusFailed, tasks.StatusSkipped, tasks.StatusNeedsUser, tasks.StatusNeedsReview:
		hint := tasks.ClassifyFailure(task.Status, task.Error)
		label := hint.Button
		if label == "" {
			label = "重试"
		}
		return hint.Sentence, label, []tasks.Task{task}
	case tasks.StatusAwaitUser:
		return "点击「去验证」在内置浏览器中完成验证", "", nil
	default:
		return "", "", nil
	}
}

func filePresent(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (a *App) accountBoard(rows []store.Browser) tasks.Board {
	if a == nil {
		return tasks.Board{}
	}
	if rows == nil {
		rows = a.configuredBrowsers()
	}
	caps := tasks.CapsFrom(a.dailyCaps(), tasks.DefaultDailyCap)
	if a.engine != nil {
		return a.engine.AccountBoardRows(rows, time.Now(), caps)
	}
	return tasks.BoardFromCaps(rows, a.queue, caps, time.Now())
}

func (a *App) laneBrowsers() []store.Browser {
	return a.configuredBrowsers()
}

func laneAccountTitle(ln tasks.AccountLane) string {
	if name := strings.TrimSpace(ln.Name); name != "" {
		return name
	}
	return ln.Platform
}

func (a *App) replaceCardImage(taskIndex int, replacementPath string) error {
	replacementPath = strings.TrimSpace(replacementPath)
	if replacementPath == "" || a.running {
		return nil
	}
	items := a.queue
	for i := range items {
		if items[i].Index != taskIndex {
			continue
		}
		src := taskSource(items[i])
		if src == "" {
			continue
		}
		info, err := os.Stat(replacementPath)
		if err != nil || info.IsDir() {
			return fmt.Errorf("找不到替换图片")
		}
		dst := src
		if len(items[i].Results) > 0 && filePresent(items[i].Results[0]) {
			dst = items[i].Results[0]
		} else {
			outDir, err := EnsureOutputDir(DefaultOutputDir(a.outputPath, mustWd()))
			if err != nil {
				return err
			}
			dst = filepath.Join(outDir, filepath.Base(src))
		}
		data, err := os.ReadFile(replacementPath)
		if err != nil {
			return err
		}
		if !images.IsValidImageBytes(data) {
			return fmt.Errorf("所选文件不是有效图片")
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		items[i].Results = []string{dst}
		items[i].Status = tasks.StatusSuccess
		items[i].Error = ""
		items[i].QualityNote = ""
		if a.statePage == pageWash {
			a.applyWashTask(items[i])
		}
		a.persistWorkspace(a.statePage)
		a.emit("workbench")
		return nil
	}
	return nil
}
