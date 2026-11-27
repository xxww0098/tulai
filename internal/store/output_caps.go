package store

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ProviderOutputCaps 描述单个供应商网页端实际支持的输出规格（自动/智能比例、支持的比例集合、
// 可选分辨率/清晰度与最近探测到的网页默认状态）。
// 实时探测结果按供应商持久化在 settings 表（key: output_caps_<slug>），未探测时由各供应商内置默认规格兜底。
type ProviderOutputCaps struct {
	Platform          string   `json:"platform"`
	Source            string   `json:"source,omitempty"`            // "live" | "default"
	SyncedAt          int64    `json:"syncedAt,omitempty"`          // Unix 秒，0 表示尚未从网页实时探测
	Model             string   `json:"model,omitempty"`             // 如即梦当前模型 "Seedream 4.7"
	SupportsAuto      bool     `json:"supportsAuto"`                // 网页比例菜单是否原生支持自动/智能/默认
	AutoValue         string   `json:"autoValue,omitempty"`         // 点击网页原生自动项的值，如 "自动"、"智能"、"auto"、""
	AutoLabel         string   `json:"autoLabel,omitempty"`         // 网页原生命名，如 "自动"、"智能比例"、"默认比例"
	Ratios            []string `json:"ratios"`                      // 网页原生支持的具体比例列表（W:H），按菜单顺序去重
	DefaultRatio      string   `json:"defaultRatio,omitempty"`      // 不支持自动时的网页默认比例（如千问 "3:4"、千问国际 "16:9"）
	Resolutions       []string `json:"resolutions,omitempty"`       // 网页原生支持的分辨率/清晰度列表（如即梦 ["1.5K","2K","4K"]）
	DefaultResolution string   `json:"defaultResolution,omitempty"` // 网页默认分辨率（如即梦 "2K"）
	CurrentRatio      string   `json:"currentRatio,omitempty"`      // 最近探测时网页当前选中的比例
	CurrentResolution string   `json:"currentResolution,omitempty"` // 最近探测时网页当前选中的分辨率
}

// OutputCapsSupportedPlatform 报告该平台是否支持网页端原生输出尺寸探测与配置。
func OutputCapsSupportedPlatform(platform string) bool {
	switch strings.TrimSpace(platform) {
	case "豆包", "千问", "千问国际", "Gemini", "即梦":
		return true
	default:
		return false
	}
}

func outputCapsSettingKey(platform string) string {
	switch strings.TrimSpace(platform) {
	case "豆包":
		return "output_caps_doubao"
	case "千问":
		return "output_caps_qianwen"
	case "千问国际":
		return "output_caps_qwen_intl"
	case "Gemini":
		return "output_caps_gemini"
	case "即梦":
		return "output_caps_jimeng"
	default:
		return ""
	}
}

// DefaultProviderOutputCaps 返回指定供应商网页端在尚未实时探测前的原生默认规格兜底。
// 各平台严格对应其网页真实控件能力，不再共用一份固定配置。
func DefaultProviderOutputCaps(platform string) ProviderOutputCaps {
	switch strings.TrimSpace(platform) {
	case "千问":
		// 千问网页「AI生图」比例菜单无自动项，默认 3:4。
		return ProviderOutputCaps{
			Platform:     "千问",
			Source:       "default",
			SupportsAuto: false,
			Ratios:       []string{"3:4", "1:1", "4:3", "16:9", "9:16", "3:2", "2:3"},
			DefaultRatio: "3:4",
			Resolutions:  []string{},
		}
	case "千问国际":
		// 千问国际网页「Create Image / 生成图像」比例下拉默认 16:9；若实时探测到 Auto/自动会自动启用自动项。
		return ProviderOutputCaps{
			Platform:     "千问国际",
			Source:       "default",
			SupportsAuto: false,
			Ratios:       []string{"16:9", "1:1", "4:3", "3:4", "9:16", "3:2", "2:3"},
			DefaultRatio: "16:9",
			Resolutions:  []string{},
		}
	case "Gemini":
		// Gemini 网页「制作图片 → 宽高比」默认未指定比例（由网页自动决定），原生菜单支持 5 个比例。
		return ProviderOutputCaps{
			Platform:     "Gemini",
			Source:       "default",
			SupportsAuto: true,
			AutoValue:    "",
			AutoLabel:    "默认比例",
			Ratios:       []string{"1:1", "4:3", "3:4", "16:9", "9:16"},
			DefaultRatio: "",
			Resolutions:  []string{},
		}
	case "即梦":
		// 即梦网页 Seedream 4.7 比例与分辨率浮层原生支持「智能比例」+ 8 种比例 + 1.5K/2K/4K 分辨率。
		return ProviderOutputCaps{
			Platform:          "即梦",
			Source:            "default",
			Model:             "Seedream 4.7",
			SupportsAuto:      true,
			AutoValue:         "智能",
			AutoLabel:         "智能比例",
			Ratios:            []string{"21:9", "16:9", "3:2", "4:3", "1:1", "3:4", "2:3", "9:16"},
			DefaultRatio:      "",
			Resolutions:       []string{"1.5K", "2K", "4K"},
			DefaultResolution: "2K",
		}
	default:
		// 豆包网页「图像生成 → 比例」原生提供「自动」与 7 种比例。
		return ProviderOutputCaps{
			Platform:     "豆包",
			Source:       "default",
			SupportsAuto: true,
			AutoValue:    "自动",
			AutoLabel:    "自动",
			Ratios:       []string{"1:1", "4:3", "3:4", "16:9", "9:16", "3:2", "2:3"},
			DefaultRatio: "",
			Resolutions:  []string{},
		}
	}
}

// CanonicalRatioToken 将比例文本清洗为标准 "W:H" 形式；非法返回空串。
func CanonicalRatioToken(raw string) string {
	s := strings.TrimSpace(strings.ReplaceAll(raw, "：", ":"))
	if s == "" {
		return ""
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return ""
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || w <= 0 {
		return ""
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || h <= 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d", w, h)
}

// CanonicalResolutionToken 将分辨率/清晰度文案归一为展示规范值（如 "2k" -> "2K"、"1.5k" -> "1.5K"）。
func CanonicalResolutionToken(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	upper := strings.ToUpper(s)
	if strings.HasSuffix(upper, "K") && len(upper) >= 2 && len(upper) <= 6 {
		num := upper[:len(upper)-1]
		if v, err := strconv.ParseFloat(num, 64); err == nil && v > 0 && v <= 16 {
			return upper
		}
	}
	switch s {
	case "标清", "高清", "超清", "原画":
		return s
	}
	if len(s) <= 24 {
		return s
	}
	return ""
}

// NormalizeProviderOutputCaps 校验并归一从网页实时探测到的输出规格。
// 至少包含 2 个有效比例，或 1 个有效比例且支持自动项时才视为有效探测结果。
func NormalizeProviderOutputCaps(platform string, raw ProviderOutputCaps) (ProviderOutputCaps, bool) {
	plat := strings.TrimSpace(platform)
	if plat == "" {
		plat = strings.TrimSpace(raw.Platform)
	}
	if !OutputCapsSupportedPlatform(plat) {
		return ProviderOutputCaps{}, false
	}
	def := DefaultProviderOutputCaps(plat)

	ratios := make([]string, 0, len(raw.Ratios))
	seenRatio := make(map[string]bool, len(raw.Ratios))
	for _, r := range raw.Ratios {
		norm := CanonicalRatioToken(r)
		if norm == "" || seenRatio[norm] {
			continue
		}
		seenRatio[norm] = true
		ratios = append(ratios, norm)
	}
	if len(ratios) == 0 || (len(ratios) < 2 && !raw.SupportsAuto) {
		return ProviderOutputCaps{}, false
	}

	resolutions := make([]string, 0, len(raw.Resolutions))
	seenRes := make(map[string]bool, len(raw.Resolutions))
	for _, res := range raw.Resolutions {
		norm := CanonicalResolutionToken(res)
		if norm == "" {
			continue
		}
		key := strings.ToLower(norm)
		if seenRes[key] {
			continue
		}
		seenRes[key] = true
		resolutions = append(resolutions, norm)
	}

	out := ProviderOutputCaps{
		Platform:     plat,
		Source:       "live",
		SyncedAt:     raw.SyncedAt,
		Model:        strings.TrimSpace(raw.Model),
		SupportsAuto: raw.SupportsAuto,
		AutoValue:    strings.TrimSpace(raw.AutoValue),
		AutoLabel:    strings.TrimSpace(raw.AutoLabel),
		Ratios:       ratios,
		Resolutions:  resolutions,
	}
	if out.Model == "" {
		out.Model = def.Model
	}
	if out.SupportsAuto {
		if out.AutoLabel == "" {
			if def.AutoLabel != "" {
				out.AutoLabel = def.AutoLabel
			} else {
				out.AutoLabel = "自动"
			}
		}
		if out.AutoValue == "" && def.SupportsAuto {
			out.AutoValue = def.AutoValue
		}
	} else {
		out.AutoValue = ""
		out.AutoLabel = ""
	}

	if curR := CanonicalRatioToken(raw.CurrentRatio); curR != "" && seenRatio[curR] {
		out.CurrentRatio = curR
	} else if out.SupportsAuto && strings.TrimSpace(raw.CurrentRatio) != "" {
		out.CurrentRatio = out.AutoLabel
	}

	if defR := CanonicalRatioToken(raw.DefaultRatio); defR != "" && seenRatio[defR] {
		out.DefaultRatio = defR
	} else if !out.SupportsAuto {
		switch {
		case out.CurrentRatio != "" && seenRatio[out.CurrentRatio]:
			out.DefaultRatio = out.CurrentRatio
		case def.DefaultRatio != "" && seenRatio[def.DefaultRatio]:
			out.DefaultRatio = def.DefaultRatio
		default:
			out.DefaultRatio = ratios[0]
		}
	}

	matchRes := func(candidate string) string {
		norm := CanonicalResolutionToken(candidate)
		if norm == "" {
			return ""
		}
		for _, r := range resolutions {
			if strings.EqualFold(r, norm) {
				return r
			}
		}
		return ""
	}
	if curRes := matchRes(raw.CurrentResolution); curRes != "" {
		out.CurrentResolution = curRes
	}
	if len(resolutions) > 0 {
		switch {
		case matchRes(raw.DefaultResolution) != "":
			out.DefaultResolution = matchRes(raw.DefaultResolution)
		case matchRes(def.DefaultResolution) != "":
			out.DefaultResolution = matchRes(def.DefaultResolution)
		case out.CurrentResolution != "":
			out.DefaultResolution = out.CurrentResolution
		default:
			out.DefaultResolution = resolutions[0]
		}
	}
	return out, true
}

// GetOutputCaps 读取指定供应商已缓存的实时网页输出规格；若无缓存或格式无效则返回 ok=false。
func (s *Store) GetOutputCaps(platform string) (ProviderOutputCaps, bool) {
	if s == nil {
		return ProviderOutputCaps{}, false
	}
	key := outputCapsSettingKey(platform)
	if key == "" {
		return ProviderOutputCaps{}, false
	}
	raw := strings.TrimSpace(s.Get(key))
	if raw == "" {
		return ProviderOutputCaps{}, false
	}
	var caps ProviderOutputCaps
	if err := json.Unmarshal([]byte(raw), &caps); err != nil {
		return ProviderOutputCaps{}, false
	}
	return NormalizeProviderOutputCaps(platform, caps)
}

// EffectiveOutputCaps 返回指定供应商当前生效的输出规格：优先返回网页实时探测缓存，否则返回该平台内置默认规格。
func (s *Store) EffectiveOutputCaps(platform string) ProviderOutputCaps {
	if caps, ok := s.GetOutputCaps(platform); ok {
		return caps
	}
	return DefaultProviderOutputCaps(platform)
}

// SaveOutputCaps 持久化保存指定供应商的实时网页输出规格缓存。
func (s *Store) SaveOutputCaps(platform string, raw ProviderOutputCaps) error {
	if s == nil {
		return nil
	}
	key := outputCapsSettingKey(platform)
	if key == "" {
		return fmt.Errorf("不支持的平台: %s", platform)
	}
	caps, ok := NormalizeProviderOutputCaps(platform, raw)
	if !ok {
		return fmt.Errorf("无效的%s网页输出规格", platform)
	}
	data, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	s.Set(key, string(data))
	return nil
}
