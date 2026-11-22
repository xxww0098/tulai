package ui

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	tsxui "github.com/nsgo-dev/nsgo/tsx/ui"
)

func init() {
	tsxui.Icons(map[string]func() *core.SVG{
		"activity":           lucide.Activity,
		"arrow-down":         lucide.ArrowDown,
		"arrow-up":           lucide.ArrowUp,
		"arrow-up-down":      lucide.ArrowUpDown,
		"book-open":          lucide.BookOpen,
		"bot":                lucide.Bot,
		"brush":              lucide.Brush,
		"check":              lucide.Check,
		"check-check":        lucide.CheckCheck,
		"chevron-down":       lucide.ChevronDown,
		"chevron-left":       lucide.ChevronLeft,
		"chevron-right":      lucide.ChevronRight,
		"chevron-up":         lucide.ChevronUp,
		"circle-alert":       lucide.CircleAlert,
		"circle-check":       lucide.CircleCheck,
		"clipboard":          lucide.Clipboard,
		"columns-2":          lucide.Columns2,
		"copy":               lucide.Copy,
		"cpu":                lucide.Cpu,
		"crop":               lucide.Crop,
		"download":           lucide.Download,
		"ellipsis":           lucide.Ellipsis,
		"eraser":             lucide.Eraser,
		"external-link":      lucide.ExternalLink,
		"eye":                lucide.Eye,
		"eye-off":            lucide.EyeOff,
		"file-spreadsheet":   lucide.FileSpreadsheet,
		"file-text":          lucide.FileText,
		"flip-horizontal":    lucide.MoveHorizontal,
		"flip-vertical":      lucide.MoveVertical,
		"folder":             lucide.Folder,
		"folder-open":        lucide.FolderOpen,
		"globe":              lucide.Globe,
		"image":              lucide.Image,
		"info":               lucide.Info,
		"key":                lucide.Key,
		"layers":             lucide.Layers,
		"layout-grid":        lucide.LayoutGrid,
		"list":               lucide.List,
		"lock":               lucide.Lock,
		"maximize-2":         lucide.Maximize2,
		"menu":               lucide.Menu,
		"minimize-2":         lucide.Minimize2,
		"monitor":            lucide.Monitor,
		"moon":               lucide.Moon,
		"palette":            lucide.Palette,
		"panel-left":         lucide.PanelLeft,
		"panel-left-close":   lucide.PanelLeftClose,
		"panel-left-open":    lucide.PanelLeftOpen,
		"pause":              lucide.Pause,
		"pencil":             lucide.Pencil,
		"play":               lucide.Play,
		"plus":               lucide.Plus,
		"power":              lucide.Power,
		"refresh-ccw":        lucide.RefreshCcw,
		"refresh-cw":         lucide.RefreshCw,
		"rotate-ccw":         lucide.RotateCcw,
		"rotate-cw":          lucide.RotateCw,
		"save":               lucide.Save,
		"search":             lucide.Search,
		"settings":           lucide.Settings,
		"shield":             lucide.Shield,
		"shield-alert":       lucide.ShieldAlert,
		"sliders-horizontal": lucide.SlidersHorizontal,
		"sliders-vertical":   lucide.SlidersVertical,
		"sparkle":            lucide.Sparkle,
		"sparkles":           lucide.Sparkles,
		"square":             lucide.Square,
		"square-terminal":    lucide.SquareTerminal,
		"sun":                lucide.Sun,
		"tag":                lucide.Tag,
		"terminal":           lucide.Terminal,
		"trash":              lucide.Trash,
		"triangle-alert":     lucide.TriangleAlert,
		"undo-2":             lucide.Undo2,
		"upload":             lucide.Upload,
		"wand":               lucide.Wand,
		"wand-sparkles":      lucide.WandSparkles,
		"x":                  lucide.X,
		"zoom-in":            lucide.ZoomIn,
		"zoom-out":           lucide.ZoomOut,
	})
}

const brandMarkSVGDocument = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128" fill="none">
<defs>
<linearGradient id="bg" x1="16" y1="8" x2="112" y2="120" gradientUnits="userSpaceOnUse">
<stop offset="0" stop-color="#172554"/>
<stop offset="1" stop-color="#091024"/>
</linearGradient>
<linearGradient id="accent" x1="28" y1="22" x2="104" y2="104" gradientUnits="userSpaceOnUse">
<stop offset="0" stop-color="#3B82F6"/>
<stop offset="1" stop-color="#1D4ED8"/>
</linearGradient>
</defs>
<rect x="4" y="4" width="120" height="120" rx="28" fill="url(#bg)"/>
<rect x="6" y="6" width="116" height="116" rx="26" stroke="#3B82F6" stroke-opacity="0.35" stroke-width="2"/>
<path d="M36 42H92C95.3 42 98 44.7 98 48V84C98 87.3 95.3 90 92 90H36C32.7 90 30 87.3 30 84V48C30 44.7 32.7 42 36 42Z" fill="#0F172A" stroke="#60A5FA" stroke-width="4"/>
<path d="M38 78L54 61L67 74L79 62L92 78H38Z" fill="url(#accent)"/>
<circle cx="48" cy="56" r="5" fill="#F8FAFF"/>
<path d="M90 28L93 36L101 39L93 42L90 50L87 42L79 39L87 36L90 28Z" fill="#34D399"/>
</svg>`
