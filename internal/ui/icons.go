package ui

import "github.com/egoist/mygo/ui"

// 导航图标是 Lucide 的线性稿，统一 1.75 线宽。颜色跟文字走，悬停和选中只改颜色。
func mustIcon(body string) *ui.SVG {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`
	return ui.MustParseSVG([]byte(svg))
}

var (
	iconLogo = mustIcon(`
		<rect x="8.5" y="3.5" width="12" height="10" rx="2"/>
		<rect x="3.5" y="9.5" width="12" height="11" rx="2"/>
		<circle cx="7.2" cy="13.2" r="1.15"/>`)
	iconImages = mustIcon(`
		<path d="m22 11-1.296-1.296a2.4 2.4 0 0 0-3.408 0L11 16"/>
		<path d="M4 8a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2"/>
		<circle cx="13" cy="7" r="1" fill="currentColor"/>
		<rect x="8" y="2" width="14" height="14" rx="2"/>`)
	iconWash = mustIcon(`
		<path d="m7 21-4.3-4.3c-1-1-1-2.5 0-3.4l9.6-9.6c1-1 2.5-1 3.4 0l5.6 5.6c1 1 1 2.5 0 3.4L13 21"/>
		<path d="M22 21H7"/>
		<path d="m5 11 9 9"/>`)
	iconDedup = mustIcon(`
		<rect x="8" y="8" width="14" height="14" rx="2"/>
		<path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>
		<path d="m12 15 2 2 4-4"/>`)
	iconGlobe = mustIcon(`
		<circle cx="12" cy="12" r="10"/>
		<path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/>
		<path d="M2 12h20"/>`)
	iconPen = mustIcon(`
		<path d="M12 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
		<path d="M18.375 2.625a1 1 0 0 1 3 3l-9.013 9.014a2 2 0 0 1-.853.505l-2.873.84a.5.5 0 0 1-.62-.62l.84-2.873a2 2 0 0 1 .506-.852z"/>`)
	iconSettings = mustIcon(`
		<path d="M9.671 4.136a2.34 2.34 0 0 1 4.659 0 2.34 2.34 0 0 0 3.319 1.915 2.34 2.34 0 0 1 2.33 4.033 2.34 2.34 0 0 0 0 3.831 2.34 2.34 0 0 1-2.33 4.033 2.34 2.34 0 0 0-3.319 1.915 2.34 2.34 0 0 1-4.659 0 2.34 2.34 0 0 0-3.32-1.915 2.34 2.34 0 0 1-2.33-4.033 2.34 2.34 0 0 0 0-3.831A2.34 2.34 0 0 1 6.35 6.051a2.34 2.34 0 0 0 3.319-1.915"/>
		<circle cx="12" cy="12" r="3"/>`)
	iconCompass = mustIcon(`
		<circle cx="12" cy="12" r="10"/>
		<path d="m16.24 7.76-1.804 5.411a2 2 0 0 1-1.265 1.265L7.76 16.24l1.804-5.411a2 2 0 0 1 1.265-1.265z"/>`)
	iconRefresh = mustIcon(`
		<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/>
		<path d="M21 3v5h-5"/>
		<path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/>
		<path d="M8 16H3v5"/>`)
)
