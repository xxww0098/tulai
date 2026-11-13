package tasks

// CapFor 返回某个供应商每个账号每天的出图上限。0 表示不限制。
type CapFor func(platform string) int

// FlatCap 让所有供应商共用一个上限。
func FlatCap(n int) CapFor {
	return func(string) int { return n }
}

// CapsFrom 按供应商查上限。表里有的值原样返回，0 或负数表示不限制；
// 表里没有的供应商用 fallback；fallback 为 0 时豆包不限、其他供应商用默认值。
func CapsFrom(byPlatform map[string]int, fallback int) CapFor {
	unlimitedDoubao := fallback == 0
	if fallback <= 0 {
		fallback = DefaultDailyCap
	}
	return func(platform string) int {
		if n, ok := byPlatform[platform]; ok {
			if n <= 0 {
				return 0
			}
			return n
		}
		if platform == "豆包" && unlimitedDoubao {
			return 0
		}
		return fallback
	}
}

// For 取这个供应商的上限。nil 按 DefaultDailyCap；返回 0 表示不限制。
func (c CapFor) For(platform string) int {
	if c == nil {
		return DefaultDailyCap
	}
	if n := c(platform); n > 0 {
		return n
	}
	return 0
}
