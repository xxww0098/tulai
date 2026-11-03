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
	iconFolder = mustIcon(`
		<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`)
	iconCheck = mustIcon(`
		<path d="M20 6 9 17l-5-5"/>`)
	iconPlay = mustIcon(`
		<polygon points="6 3 20 12 6 21 6 3"/>`)
	iconTerminal = mustIcon(`
		<path d="m4 17 6-6-6-6"/>
		<path d="M12 19h8"/>`)
	iconDownload = mustIcon(`
		<path d="M12 15V3"/>
		<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
		<path d="m7 10 5 5 5-5"/>`)
	iconRename = mustIcon(`
		<path d="M4 7h10M4 17h10M4 4v6M4 14v6"/>
		<path d="m15 17 4-4 2 2-4 4-3 1z"/>
		<path d="M18 14l2 2"/>`)
	iconArrowRight  = mustIcon(`<path d="M4 12h16m-6-6 6 6-6 6"/>`)
	iconChevronDown = mustIcon(`<path d="m6 9 6 6 6-6"/>`)
	iconSparkles    = mustIcon(`<path d="m12 3 1.4 4.6L18 9l-4.6 1.4L12 15l-1.4-4.6L6 9l4.6-1.4L12 3ZM19 14l.8 2.2L22 17l-2.2.8L19 20l-.8-2.2L16 17l2.2-.8L19 14ZM4 15l.6 1.4L6 17l-1.4.6L4 19l-.6-1.4L2 17l1.4-.6L4 15Z"/>`)
	iconListOrdered = mustIcon(`<path d="M10 6h10M10 12h10M10 18h10M4 5h1v3M4 8h2M4 12c0-1 2-1 2 0 0 1-2 1.5-2 2h2M4 18h2m-1-2v4"/>`)
	iconFile        = mustIcon(`<path d="M6 3h8l4 4v14H6z"/><path d="M14 3v5h4"/>`)
	iconFilePlus    = mustIcon(`<path d="M6 3h8l4 4v14H6z"/><path d="M14 3v5h4M12 11v7M8.5 14.5h7"/>`)
	iconCopy        = mustIcon(`<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/>`)
	iconSearch      = mustIcon(`
		<circle cx="11" cy="11" r="8"/>
		<path d="m21 21-4.3-4.3"/>`)
	iconPlus = mustIcon(`
		<path d="M5 12h14"/>
		<path d="M12 5v14"/>`)
	iconPause = mustIcon(`
		<rect x="6" y="4" width="4" height="16" rx="1"/>
		<rect x="14" y="4" width="4" height="16" rx="1"/>`)
	iconRotate = mustIcon(`
		<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/>
		<path d="M3 3v5h5"/>`)
	iconCircleX = mustIcon(`
		<circle cx="12" cy="12" r="10"/>
		<path d="m15 9-6 6"/>
		<path d="m9 9 6 6"/>`)
	iconTrash = mustIcon(`
		<path d="M3 6h18"/>
		<path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/>
		<path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>`)
	iconLayers = mustIcon(`
		<path d="M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z"/>
		<path d="m22 17.65-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65"/>
		<path d="m22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65"/>`)
	iconX = mustIcon(`
		<path d="M18 6 6 18"/>
		<path d="m6 6 12 12"/>`)
	iconChevronRight = mustIcon(`
		<path d="m9 18 6-6-6-6"/>`)
	iconFolderOpen = mustIcon(`
		<path d="m6 14 1.45-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.55 6a2 2 0 0 1-1.94 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.93a2 2 0 0 1 1.66.9l.82 1.2a2 2 0 0 0 1.66.9H18a2 2 0 0 1 2 2v2"/>`)
	iconAlert = mustIcon(`
		<circle cx="12" cy="12" r="10"/>
		<path d="M12 8v4"/>
		<path d="M12 16h.01"/>`)
	iconBan = mustIcon(`
		<circle cx="12" cy="12" r="10"/>
		<path d="m4.9 4.9 14.2 14.2"/>`)
	iconClock = mustIcon(`
		<circle cx="12" cy="12" r="10"/>
		<path d="M12 6v6l4 2"/>`)
	iconCrop = mustIcon(`
		<path d="M6 2v14a2 2 0 0 0 2 2h14"/>
		<path d="M18 22V8a2 2 0 0 0-2-2H2"/>`)
	iconFileOutput = mustIcon(`
		<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h5"/>
		<path d="M14 2v6h6"/>
		<path d="M15 18h6m-3-3 3 3-3 3"/>`)
	iconSliders = mustIcon(`
		<path d="M21 4h-7M10 4H3M21 12h-9M8 12H3M21 20h-5M12 20H3"/>
		<path d="M14 2v4M8 10v4M16 18v4"/>`)
	iconContrast = mustIcon(`
		<circle cx="12" cy="12" r="10"/>
		<path d="M12 18a6 6 0 0 0 0-12v12Z" fill="currentColor"/>`)
	iconFolderOutput = mustIcon(`
		<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>
		<path d="M12 11v6m-3-3 3 3 3-3"/>`)
	iconImagePlus = mustIcon(`
		<path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h7"/>
		<path d="m21 15-3.1-3.1a2 2 0 0 0-2.8 0L6 21"/>
		<circle cx="9" cy="9" r="1.6"/>
		<path d="M19 3v6M16 6h6"/>`)
	iconSave = mustIcon(`
		<path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z"/>
		<path d="M17 21v-8H7v8M7 3v5h8"/>`)
	iconLink = mustIcon(`
		<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/>
		<path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>`)
	iconRotateCcw = mustIcon(`
		<path d="M3 12a9 9 0 1 0 3-6.7L3 8"/>
		<path d="M3 3v5h5"/>`)
	iconRotateCw = mustIcon(`
		<path d="M21 12a9 9 0 1 1-3-6.7L21 8"/>
		<path d="M21 3v5h-5"/>`)
	iconFlipH = mustIcon(`
		<path d="M12 3v18"/>
		<path d="M8 7 4 12l4 5H3V7h5ZM16 7l4 5-4 5h5V7h-5Z"/>`)
	iconFlipV = mustIcon(`
		<path d="M3 12h18"/>
		<path d="M7 8 12 4l5 4v5H7V8ZM7 16l5 4 5-4v-5H7v5Z"/>`)
	iconFileImage = mustIcon(`
		<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"/>
		<path d="M14 2v6h6"/>
		<circle cx="9" cy="12" r="1.3"/>
		<path d="m20 17-3.5-3.5L9 21"/>`)
	iconChevronLeft = mustIcon(`<path d="m15 18-6-6 6-6"/>`)
	iconStop        = mustIcon(`<rect x="6" y="6" width="12" height="12" rx="2"/>`)
	iconChevronsLR  = mustIcon(`
		<path d="m9 7-5 5 5 5M15 7l5 5-5 5"/>`)
)
