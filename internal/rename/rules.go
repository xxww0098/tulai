package rename

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

func Created(path string) (time.Time, error) { return creationTime(path) }

const (
	Replace = iota
	Insert
	Number
	Manual
)

type Replacement struct {
	Find, With string
	Template   int
	Count      int
	Offset     int
	Text       string
}

type Numbering struct {
	Start          int64
	Width          int
	Format         string
	Prefix, Suffix string
}

type Options struct {
	Mode             int
	Replacements     []Replacement
	IncludeExtension bool
	Position         int // 0: beginning, 1: after Index characters, 2: end
	Index            int
	InsertKind       string
	Text             string
	Prefix, Suffix   string
	Numbering        Numbering
	Names            []string
}

// Preview calculates names only; it never changes files.
func Preview(paths []string, opts Options) ([]Change, error) {
	changes := make([]Change, 0, len(paths))
	if opts.Mode == Manual && len(opts.Names) != len(paths) {
		return nil, fmt.Errorf("名称数量与文件数量不一致")
	}
	if opts.Numbering.Start < 0 || opts.Numbering.Start > 9_000_000_000_000_000 || opts.Numbering.Width < 0 || opts.Numbering.Width > 16 {
		return nil, fmt.Errorf("编号起始值或位数超出范围")
	}
	for i, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("读取 %s：%w", path, err)
		}
		name := filepath.Base(path)
		base, ext := name, ""
		if !info.IsDir() {
			ext = filepath.Ext(name)
			if ext == name { // A leading dot alone does not start an extension.
				ext = ""
			}
			base = strings.TrimSuffix(name, ext)
		}
		switch opts.Mode {
		case Replace:
			if opts.IncludeExtension {
				base, ext = name, ""
			}
			for _, rule := range opts.Replacements {
				base, err = replace(base, rule)
				if err != nil {
					return nil, err
				}
			}
			name = base + ext
		case Insert:
			value := opts.Text
			switch opts.InsertKind {
			case "序号":
				value = number(opts.Numbering, i)
			case "修改时间":
				value = info.ModTime().Format("2006-01-02_15-04-05")
			case "创建时间":
				created, err := creationTime(path)
				if err != nil {
					return nil, fmt.Errorf("读取 %s 的创建时间：%w", name, err)
				}
				value = created.Format("2006-01-02_15-04-05")
			case "大小":
				value = strconv.FormatInt(info.Size(), 10)
			case "图片宽度", "图片高度":
				f, err := os.Open(path)
				if err != nil {
					return nil, fmt.Errorf("读取 %s：%w", name, err)
				}
				cfg, _, err := image.DecodeConfig(f)
				_ = f.Close()
				if err != nil {
					return nil, fmt.Errorf("无法读取 %s 的图片尺寸：%w", name, err)
				}
				value = strconv.Itoa(cfg.Width)
				if opts.InsertKind == "图片高度" {
					value = strconv.Itoa(cfg.Height)
				}
			case "照片拍摄时间":
				taken, err := photoTime(path)
				if err != nil {
					return nil, fmt.Errorf("读取 %s 的拍摄时间：%w", name, err)
				}
				value = taken.Format("2006-01-02_15-04-05")
			}
			value = opts.Prefix + value + opts.Suffix
			chars := []rune(base)
			at := len(chars)
			if opts.Position == 0 {
				at = 0
			} else if opts.Position == 1 {
				at = max(0, min(opts.Index, len(chars)))
			}
			name = string(chars[:at]) + value + string(chars[at:]) + ext
		case Number:
			name = number(opts.Numbering, i) + ext
		case Manual:
			name = opts.Names[i]
		default:
			return nil, fmt.Errorf("未知的重命名规则")
		}
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsRune(name, 0) {
			return nil, fmt.Errorf("无效的文件名：%q", name)
		}
		changes = append(changes, Change{From: path, To: filepath.Join(filepath.Dir(path), name)})
	}
	return changes, nil
}

func replace(text string, rule Replacement) (string, error) {
	if rule.Template != 0 {
		return replaceTemplate(text, rule), nil
	}
	if rule.Find == "" {
		return text, nil
	}
	if strings.HasPrefix(rule.Find, "/") {
		last := strings.LastIndex(rule.Find, "/")
		if last > 0 {
			flags := rule.Find[last+1:]
			if strings.Trim(flags, "gimuy") == "" {
				pattern := rule.Find[1:last]
				if strings.Contains(flags, "i") {
					pattern = "(?i)" + pattern
				}
				if strings.Contains(flags, "m") {
					pattern = "(?m)" + pattern
				}
				if strings.Contains(flags, "y") {
					pattern = "\\A(?:" + pattern + ")"
				}
				re, err := regexp.Compile(pattern)
				if err != nil {
					return "", fmt.Errorf("正则表达式不受支持：%w", err)
				}
				limit := 1
				if strings.Contains(flags, "g") {
					limit = -1
				}
				matches := re.FindAllStringSubmatchIndex(text, limit)
				var out strings.Builder
				end := 0
				for _, match := range matches {
					out.WriteString(text[end:match[0]])
					out.WriteString(expandReplacement(text, rule.With, match))
					end = match[1]
				}
				out.WriteString(text[end:])
				return out.String(), nil
			}
		}
	}
	return strings.ReplaceAll(text, rule.Find, rule.With), nil
}

func replaceTemplate(text string, rule Replacement) string {
	chars := []rune(text)
	count, offset := max(0, rule.Count), max(0, rule.Offset)
	start, end := 0, len(chars)
	switch rule.Template {
	case 1:
		end = count
	case 2:
		start = end - count
	case 3:
		start, end = offset, offset+count
	case 4, 5, 6, 7:
		if rule.Text == "" {
			return text
		}
		pos := strings.Index(text, rule.Text)
		if pos < 0 {
			return text
		}
		before := len([]rune(text[:pos]))
		after := before + len([]rune(rule.Text))
		switch rule.Template {
		case 4:
			start = after
		case 5:
			end = before
		case 6:
			start, end = after, after+count
		case 7:
			start, end = before-count, before
		}
	default:
		return text
	}
	if start < 0 || end < start || end > len(chars) {
		return text
	}
	prefix, match := string(chars[:start]), string(chars[start:end])
	indices := []int{len(prefix), len(prefix) + len(match)}
	return prefix + expandReplacement(text, rule.With, indices) + string(chars[end:])
}

func expandReplacement(text, replacement string, match []int) string {
	var out strings.Builder
	for i := 0; i < len(replacement); i++ {
		if replacement[i] != '$' || i+1 == len(replacement) {
			out.WriteByte(replacement[i])
			continue
		}
		switch replacement[i+1] {
		case '$':
			out.WriteByte('$')
			i++
		case '&':
			out.WriteString(text[match[0]:match[1]])
			i++
		case '`':
			out.WriteString(text[:match[0]])
			i++
		case '\'':
			out.WriteString(text[match[1]:])
			i++
		default:
			n := int(replacement[i+1] - '0')
			if n > 0 && n < 10 && n*2+1 < len(match) {
				i++
				if i+1 < len(replacement) && replacement[i+1] >= '0' && replacement[i+1] <= '9' {
					larger := n*10 + int(replacement[i+1]-'0')
					if larger*2+1 < len(match) {
						n = larger
						i++
					}
				}
				if match[n*2] >= 0 {
					out.WriteString(text[match[n*2]:match[n*2+1]])
				}
			} else {
				out.WriteByte('$')
			}
		}
	}
	return out.String()
}

func number(opts Numbering, index int) string {
	n := opts.Start + int64(index)
	value := strconv.FormatInt(n, 10)
	switch opts.Format {
	case "中文小写":
		value = chineseNumber(n, false)
	case "中文大写":
		value = chineseNumber(n, true)
	case "英文大写", "英文小写":
		value = ""
		for n > 0 {
			n--
			value = string(rune('A'+n%26)) + value
			n /= 26
		}
		if opts.Format == "英文小写" {
			value = strings.ToLower(value)
		}
	default:
		if opts.Width > len(value) {
			value = strings.Repeat("0", opts.Width-len(value)) + value
		}
	}
	return opts.Prefix + value + opts.Suffix
}

func chineseNumber(n int64, upper bool) string {
	digits := []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
	units := []string{"", "十", "百", "千"}
	groups := []string{"", "万", "亿", "兆"}
	if upper {
		digits = []string{"零", "壹", "贰", "叁", "肆", "伍", "陆", "柒", "捌", "玖"}
		units = []string{"", "拾", "佰", "仟"}
		groups = []string{"", "万", "亿", "兆"}
	}
	if n == 0 {
		return digits[0]
	}
	parts := make([]int, 0, 4)
	for n > 0 {
		parts = append(parts, int(n%10000))
		n /= 10000
	}
	var out strings.Builder
	zero := false
	for g := len(parts) - 1; g >= 0; g-- {
		part := parts[g]
		if part == 0 {
			if out.Len() > 0 {
				zero = true
			}
			continue
		}
		if out.Len() > 0 && (zero || part < 1000) {
			out.WriteString(digits[0])
		}
		zero = false
		started := false
		for p, divisor := 3, 1000; p >= 0; p, divisor = p-1, divisor/10 {
			digit := part / divisor % 10
			if digit == 0 {
				if part%divisor != 0 && started {
					zero = true
				}
				continue
			}
			if zero {
				out.WriteString(digits[0])
				zero = false
			}
			if upper || digit != 1 || p != 1 || out.Len() != 0 {
				out.WriteString(digits[digit])
			}
			out.WriteString(units[p])
			started = true
		}
		out.WriteString(groups[g])
	}
	return out.String()
}
