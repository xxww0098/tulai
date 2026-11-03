package download

import "strings"

// Template 用条目的元数据生成相对路径。返回空字符串时改用地址的最后一段。
type Template interface {
	Render(metadata map[string]string) string
}

// BraceTemplate 把 {字段名} 换成元数据里的值。未知字段保留原样。
type BraceTemplate string

// Render 替换模板里的字段。
func (t BraceTemplate) Render(metadata map[string]string) string {
	out := string(t)
	for key, value := range metadata {
		out = strings.ReplaceAll(out, "{"+key+"}", value)
	}
	return out
}
