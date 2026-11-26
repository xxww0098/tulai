---
name: "tulai"
description: "Multi-provider, multi-account desktop AI batch image dispatch console built with MyGo v0.10.0 + React 19 + Vite 6 + Tailwind CSS v4 using a modular rounded-rectangle Bento card aesthetic with Light/Dark operational parity."
colors:
  primary-blue-light: "#2563EB"
  primary-blue-hover-light: "#1D4ED8"
  primary-blue-pressed-light: "#1E40AF"
  on-primary-light: "#FFFFFF"
  cool-paper-light: "#F2F4F7"
  rail-light: "#ECEEF2"
  rail-hover-light: "#E2E5EB"
  rail-active-light: "#FFFFFF"
  rail-border-light: "#E2E6EC"
  rail-text-light: "#0F172A"
  rail-muted-light: "#475569"
  rail-accent-light: "#2563EB"
  surface-card-light: "#FFFFFF"
  card-header-light: "#FAFBFD"
  sunken-track-light: "#F5F7FA"
  sunken-hover-light: "#EBEEF3"
  border-hairline-light: "#E6EAF0"
  border-divider-light: "#EEF1F6"
  border-strong-light: "#CBD5E1"
  ink-slate-light: "#0F172A"
  muted-slate-light: "#526075"
  subtle-slate-light: "#8492A6"
  signal-emerald-light: "#15803D"
  amber-alert-light: "#B45309"
  crimson-fault-light: "#DC2626"
  primary-blue-dark: "#60A5FA"
  primary-blue-hover-dark: "#93C5FD"
  primary-blue-pressed-dark: "#3B82F6"
  on-primary-dark: "#090D16"
  cool-paper-dark: "#0C1017"
  rail-dark: "#10151F"
  rail-hover-dark: "#171E2C"
  rail-active-dark: "#1D2536"
  rail-border-dark: "#1D2536"
  rail-text-dark: "#F1F5F9"
  rail-muted-dark: "#94A3B8"
  rail-accent-dark: "#60A5FA"
  surface-card-dark: "#131924"
  card-header-dark: "#161D2B"
  sunken-track-dark: "#182030"
  sunken-hover-dark: "#222C3F"
  border-hairline-dark: "#222B3C"
  border-divider-dark: "#1E2636"
  border-strong-dark: "#334155"
  ink-slate-dark: "#F1F5F9"
  muted-slate-dark: "#94A3B8"
  subtle-slate-dark: "#64748B"
  signal-emerald-dark: "#34D399"
  amber-alert-dark: "#FBBF24"
  crimson-fault-dark: "#F87171"
typography:
  display:
    fontFamily: "system-ui"
    fontSize: "18px"
    fontWeight: 600
  title:
    fontFamily: "system-ui"
    fontSize: "13px"
    fontWeight: 600
  body:
    fontFamily: "system-ui"
    fontSize: "12px"
    fontWeight: 400
    fontFeature: "tnum"
  caption:
    fontFamily: "system-ui"
    fontSize: "11px"
    fontWeight: 500
    fontFeature: "tnum"
rounded:
  sm: "8px"
  md: "12px"
  lg: "16px"
  pill: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "20px"
---

# Design System: tulai

## Overview

**Creative North Star: "Modular Bento Dispatch Console"**

`tulai` is a dense, high-trust desktop operational workbench running in MyGo v0.10.0 WebView (React 19 + Vite 6 + Tailwind CSS v4 + TypeScript 5). Every functional module lives inside an isolated rounded-rectangle Bento card (`rounded-[16px]`) floating over a soft cool-slate canvas (`#F2F4F7` Light / `#0C1017` Dark). A macOS-inspired left navigation rail (`#ECEEF2` Light / `#10151F` Dark) anchors navigation with rounded-pill items (`rounded-[10px]`) and a `3px` cobalt vertical indicator bar on the active item.

## Source of Truth Files

| Layer | Path | Responsibility |
|---|---|---|
| Design Tokens | [`frontend/src/styles/tokens.css`](frontend/src/styles/tokens.css) | `:root` & `.dark` CSS variables + Tailwind v4 `@theme inline` semantic color bindings |
| Global Styles | [`frontend/src/styles/global.css`](frontend/src/styles/global.css) | Typography stack, `.tnum`, `.font-mono-console`, `.app-drag-region` / `.app-no-drag`, `.bento-shadow`, `.float-shadow`, `.bg-checkerboard`, thin scrollbars, motion keyframes & `prefers-reduced-motion` |
| Layout Primitives | [`frontend/src/components/primitives.tsx`](frontend/src/components/primitives.tsx) | Bento shell building blocks (`PageToolbar`, `BentoCard`, `FlushPane`, `PaneHeader`, `InsetPillRow`, `SegmentedPill`, `PillToggle`, `FormRow`, `EmptyNotice`, `StatusBadge`, `PageSkeleton`, `BrandLogo`) |
| UI Component Kit | [`frontend/src/components/ui/`](frontend/src/components/ui/index.ts) | Radix UI + Tailwind v4 interactive controls (`Badge`, `OutlineBadge`, `Button`, `IconButton`, `Checkbox`, `Dialog`, `DropZoneOverlay`, `Icon`, `Input`, `Textarea`, `DropdownMenu`, `ContextMenu`, `Progress`, `Select`, `Slider`, `Switch`, `Tabs`, `Toaster`, `Tooltip`, `VirtualList`) |
| App Shell & Navigation | [`frontend/src/App.tsx`](frontend/src/App.tsx) | Left navigation rail, provider submenu, floating Workbench topbar, page router, and global overlay portals (`BrowserDrawer`, `LogDrawer`, `WizardView`, `HoverPreviewHost`, `Toaster`) |
| Shortcuts & Escape Stack | [`frontend/src/lib/shortcuts.ts`](frontend/src/lib/shortcuts.ts) | Global `⌘/Ctrl` keybindings and prioritized `Esc` dismissal stack |
| Window Chrome (Go) | [`internal/ui/controller.go`](internal/ui/controller.go) | MyGo window geometry, `TitleBarHiddenInset`, theme sync, native macOS menus, and file-drop forwarding |

## Layout & Surface Tokens (`frontend/src/styles/tokens.css`)

- **Canvas (`--bg-paper` / `bg-paper`):** `#F2F4F7` (Light) / `#0C1017` (Dark), with `gap-3` (`12px`) separating adjacent Bento cards and `px-3 pb-3` outer canvas margins.
- **Sidebar Rail (`--bg-rail` / `bg-rail`):** `#ECEEF2` (Light) / `#10151F` (Dark), `160px` expanded / `52px` collapsed (`< 960px` auto-collapse unless manually toggled), `border-r border-rail-border` (`#E2E6EC` / `#1D2536`). Active item uses `bg-rail-active` (`#FFFFFF` / `#1D2536`) with a `3px` cobalt bar (`bg-accent`).
- **Bento Card (`--bg-card` / `bg-card`):** `#FFFFFF` (Light) / `#131924` (Dark), `rounded-[16px]`, `1px` hairline border (`--border-hairline`: `#E6EAF0` / `#222B3C`), `bento-shadow`, `p-3` (compact) or `p-4`. Card headers use `--bg-card-header` (`#FAFBFD` / `#161D2B`) with `--border-divider` (`#EEF1F6` / `#1E2636`).
- **Sunken Inner Pill / Row (`--bg-sunken` / `bg-sunken`):** `#F5F7FA` (Light) / `#182030` (Dark), hover `--bg-sunken-hover` (`#EBEEF3` / `#222C3F`), `rounded-[12px]`, used for account rows, status rows, and secondary inset containers inside Bento cards.
- **Accent & Semantic Status:** Primary cobalt `--accent` (`#2563EB` Light / `#60A5FA` Dark, foreground `--on-accent`: `#FFFFFF` / `#090D16`), soft tint `--accent-soft` (`#EFF6FF` / `#172540`), border `--accent-border` (`#BFDBFE` / `#2E4A7D`). Semantic status tokens cover `--status-ok`, `--status-warn`, `--status-danger` plus matched `-bg` and `-border` surfaces.
- **Typography & Numerals:** System CJK-aware sans stack (`-apple-system`, `SF Pro Text`, `PingFang SC`, `Microsoft YaHei UI`, `Segoe UI`), base `13px` / `1.45` line height; `.tnum` enforces `font-variant-numeric: tabular-nums` on all counters, quotas, timestamps, dimensions, and progress indicators; `.font-mono-console` formats paths, proxy URLs, and log entries.

## Desktop Chrome & Titlebar Drag Regions

- **Window Configuration (`internal/ui/controller.go`):** `1160×740` default (`MinWidth: 900`, `MinHeight: 620`), `StateKey: "main"`, `BackgroundColor: "light-dark(#F2F4F7, #0C1017)"`, `Hidden: true` + `OnReadyToShow` to avoid startup flash, `BackgroundThrottling: ThrottlingOff`.
- **macOS Hidden-Inset Titlebar:** `TitleBarStyle: TitleBarHiddenInset`, `TrafficLightPosition: {X: 14, Y: 14}`, `TitleBarHeight: 38`.
  - When `shell.os === "darwin"`, the left sidebar renders a top `.app-drag-region` spacer sized to `calc(var(--mygo-titlebar-controls-bottom, 28px) + 4px)` so traffic lights never collide with the brand header.
  - Both `PageToolbar` and the Workbench floating topbar apply `.app-drag-region` (`--app-region: drag` / `-webkit-app-region: drag`), reserve right-side window-control inset via `paddingRight: calc(14px + var(--mygo-titlebar-inset-right, 0px))`, and wrap interactive controls in `.app-no-drag`.
- **Responsive Breakpoints:**
  - `< 960px`: Left sidebar auto-folds to `52px` icon rail (and exposes an inline platform selector in the Workbench topbar).
  - `< 1040px`: ImageEdit filmstrip compacts to `68px` thumbnail strip.
  - `< 1120px`: Settings left Bento rail switches to segmented tabs in `PageToolbar`.

## Global Keyboard Shortcuts & Priority Escape Stack (`frontend/src/lib/shortcuts.ts`)

| Shortcut | Action | Notes |
|---|---|---|
| `⌘/Ctrl + 1` … `8` | Switch primary navigation pages (`1` 图片生成, `2` 浏览器, `3` 提示词, `4` 下载, `5` 去重, `6` 批量重命名, `7` 图片编辑, `8` 设置) | Ignored while typing inside editable fields |
| `⌘/Ctrl + ,` | Jump to 设置 | Works globally |
| `⌘/Ctrl + \` | Toggle sidebar rail collapse/expand | Works globally |
| `⌘/Ctrl + J` | Toggle floating `LogDrawer` | Ignored while typing inside editable fields |
| `⌘/Ctrl + Enter` | Start Workbench run (or submit URLs in `DownloadPage` input) | Context-scoped |
| `Esc` | Dismiss highest-priority active overlay/mode | Defers to open Radix popovers/menus first |

**Priority `Esc` Stack (highest closes first):**
- `50`: Workbench Lightbox (`WorkbenchDialogs.tsx`)
- `40`: Modal Dialogs (`WizardView`, gallery/structured prompt/import/guide/recipe/result dialogs)
- `30`: `BrowserDrawer` (`BrowserDrawer.tsx`)
- `20`: `LogDrawer` (`LogDrawer.tsx`)
- `10`: Page-level modes (e.g., Workbench completed-gallery multi-select in `ResultGrid.tsx`)

## Shared Primitives (`frontend/src/components/primitives.tsx`)

- `PageToolbar`: Floating `rounded-[16px]` `.app-drag-region` toolbar card inside `px-3 pt-2.5 pb-2.5` with `left`, `right`, optional `bottom` banner, and automatic `ShellActions` (running badge on utility pages).
- `BentoCard`: Modular `rounded-[16px]` card with `compact` (`p-3`) or default (`p-4`) padding, optional header (`icon`, `title`, `subtitle`, `right`), and `flex` mode.
- `FlushPane` & `PaneHeader`: Full-height `rounded-[16px]` `overflow-hidden` container with a `border-b border-divider` header.
- `InsetPillRow`: `rounded-[12px]` sunken row (`bg-sunken`, interactive hover `bg-sunken-hover`, active `bg-accent-soft` + `border-accent-border`).
- `SegmentedPill` & `PillToggle`: Active cobalt pill controls (`bg-accent text-on-accent`).
- `FormRow`, `EmptyNotice`, `StatusBadge` (`toneVariant`), `PageSkeleton`, `BrandLogo`, plus re-exported `AccountListFrame`, `AccountViewButton`, and `OutlineBadge`.

## Upgraded Operational Surfaces

### Workbench Pipeline, Multi-Select & Lightbox
- **4-Stage Result Pipeline ([`ResultGrid.tsx`](frontend/src/components/workbench/ResultGrid.tsx)):** `正在加工` renders HTML5 Canvas `WashBotCard`（「小白洗图君」in [`WashBotStation.tsx`](frontend/src/components/workbench/WashBotStation.tsx), honoring `prefers-reduced-motion` and offering a manual pause/resume toggle), followed by `等待加工`, `需处理`, and `已完成` (with `小图`/`中图` density switch, `HoverPreviewTarget`, and right-click `ContextMenu`).
- **Shift Multi-Select & Secondary Rewash:** Clicking `批量选择` or `Shift+点击` on completed tiles supports contiguous range selection, batch `放回待洗 · 二次洗`, and one-click `撤销放回` (`Esc` priority `10` exits selection).
- **Lightbox Inspection ([`WorkbenchDialogs.tsx`](frontend/src/components/workbench/WorkbenchDialogs.tsx)):** Loads full-resolution images over `tulaimedia`, supports `100%–400%` wheel/button/double-click zoom + pointer pan, draggable `5%–95%` before/after original-vs-output split comparison (`对比原图` / `C`), and keyboard shortcuts (`←`/`→` navigate, `+`/`-`/`0` zoom/reset, `C` compare, `Esc` close).

### ImageEdit Darkroom ([`ImageEditPage.tsx`](frontend/src/pages/ImageEditPage.tsx), [`ImageEditStage.tsx`](frontend/src/components/imageedit/ImageEditStage.tsx))
- **3-Column Darkroom Layout:** Left virtualized collapsible filmstrip (`Shift`/`⌘`/`Ctrl` multi-select), center darkroom viewport (`#0F172A` canvas + `#131924` toolbar + `.bg-checkerboard` alpha matte), right 3-card Bento inspector (`裁剪、尺寸与旋转`, `色彩与清晰度`, `输出格式与目录`).
- **8-Handle Interactive Crop Overlay:** Handles `nw`, `n`, `ne`, `e`, `se`, `s`, `sw`, `w` plus body drag and free-draw creation in source-pixel space, clamping within `[0..srcW, 0..srcH]` (`>= 8px`), honoring locked aspect ratios, and committing to Go on pointer release.
- **60fps CSS Filter Live Preview:** Dragging `亮度`/`对比度`/`饱和度`/`模糊` sliders or toggling `黑白灰度` applies a real-time CSS `filter` delta locally while throttling Go render commits (`120ms`) and double-buffering server preview images for zero flicker.
- **Draggable Split Curtain & Darkroom Navigation:** Bidirectional `5%–95%` original-vs-processed split curtain synced with the toolbar slider, cursor-anchored wheel zoom (`10%–800%`), `Space`+drag / middle-click pan, and darkroom shortcuts (`+`/`-`/`0`, `C` compare, `[`/`]` 90° rotate, `↑`/`↓` filmstrip switch).

### Floating Drawers & Hover Preview
- **[`BrowserDrawer.tsx`](frontend/src/components/BrowserDrawer.tsx):** Draggable, resizable floating window (`z-[45]`) that auto-positions beside `AccountListFrame`, streams live frames over `tulaimedia`, and forwards pointer/wheel/keyboard/CJK-IME/paste inputs (`BrowserStage.tsx`).
- **[`LogDrawer.tsx`](frontend/src/components/LogDrawer.tsx):** Virtualized dark console (`#0B0E14`, `z-40`) with level count chips, search, source cycling, pause/follow, click-to-expand details, copy, and native export.
- **[`HoverPreview.tsx`](frontend/src/components/HoverPreview.tsx):** Viewport-clamped `280px` floating preview card (`z-[65]`, `140ms` open delay, auto-hides on scroll/blur/click) shared across Workbench, Dedup, and ImageEdit.
