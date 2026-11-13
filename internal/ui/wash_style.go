package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

type washStylePreset struct {
	label       string
	instruction string
}

const washStyleOriginal = "保持原风格"

var washStyles = []washStylePreset{
	{washStyleOriginal, ""},
	{"像素风格", "把选中的整幅图重新画成低分辨率的二维游戏像素画：以约 48×60 的逻辑像素组织主体和背景，再用最近邻方式放大到目标尺寸。画面各处都要有清楚可见的方形色块，主体轮廓、曲线和镶嵌线都呈粗方块阶梯，不能保留平滑矢量曲线。色块紧密无缝、内部纯色，不能给每个像素画边框、留缝或加圆点高光。去除原图的细格线、填色数字、印刷网点、珠粒与纸面纹理；不要在高清原图上叠加网格，也不要做成拼豆或钻石画。保留主体主要形状和颜色，图案内部必要的深色轮廓可以由方块组成。"},
	{"水彩插画", "使用透明水彩叠色、柔和晕染和轻微纸面肌理，呈现二维水彩插画。"},
	{"油画风格", "使用可见的油画笔触、丰富的色彩层次和适度画布肌理，呈现二维油画。"},
	{"动漫插画", "使用清晰线条、概括的色块和柔和赛璐璐光影，呈现二维动漫插画。"},
	{"扁平插画", "使用简洁轮廓、平整色块和少量阴影，呈现二维扁平插画。"},
	{"国风水墨", "使用水墨笔触、墨色浓淡和适度留白，呈现二维中国风水墨画。"},
	{"黑白线稿", "转换为纯黑白线稿：白底、黑色轮廓和必要细节线条，不使用任何彩色、灰度阴影或彩色填充，适合印刷和上色。"},
	{"复古版画", "使用版画式刻线、有限色彩和轻微复古纸感，呈现二维复古版画。"},
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
	a.washStyle = pickStr(a.store.Get("wash_style"), washStyleOriginal, washStyleLabels())
}

func (a *App) saveWashStyle() {
	if a.store != nil {
		a.store.Set("wash_style", pickStr(a.washStyle, washStyleOriginal, washStyleLabels()))
	}
}

// A selected preset replaces the optional style part; other user parameters stay intact.
func (a *App) effectivePromptParts(page int, parts *promptParts) promptParts {
	effective := *parts
	if page == pageWash && washStyleInstruction(a.washStyle) != "" {
		effective.Style.Enabled = false
	}
	return effective
}

func (a *App) promptExtras(page int, parts *promptParts) string {
	effective := a.effectivePromptParts(page, parts)
	extra := effective.text()
	if page == pageWash {
		if style := washStyleInstruction(a.washStyle); style != "" {
			extra = strings.TrimSpace(extra + "\n" + style)
		}
	}
	return extra
}

func (a *App) washStyleSelect(c *ui.Context) {
	if a.washStyle == "" {
		a.washStyle = washStyleOriginal
	}
	tip := "仅影响新任务；保持原风格不会额外添加要求，选择其他风格后将优先于洗图指令和组合参数中的风格。组合参数中可自定义风格。"
	ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "风格").FontSize(a.look.caption).TextColor(a.look.muted).Shrink(0)
		motionSelect(c.Key("wash-style"), &a.washStyle, washStyleLabels()).
			Grow(1).MinWidth(0).Label("洗图风格").Disabled(a.running).Tooltip(tip).
			OnChange(func() { a.saveWashStyle() })
		ui.Box(c).Shrink(0).Tooltip(tip).Children(func() {
			ui.Icon(c, iconInfo).FontSize(12).TextColor(a.look.muted)
		})
	})
}
