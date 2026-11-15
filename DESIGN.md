---
name: "tulai"
description: "Multi-provider, multi-account desktop AI batch image dispatch console with Light/Dark operational parity."
colors:
  primary-blue-light: "#2563EB"
  primary-blue-hover-light: "#1D4ED8"
  primary-blue-pressed-light: "#1E40AF"
  on-primary-light: "#FFFFFF"
  cool-paper-light: "#F8FAFC"
  rail-light: "#111622"
  rail-hover-light: "#1B2333"
  rail-active-light: "#253047"
  rail-border-light: "#222B3C"
  rail-text-light: "#F8FAFC"
  rail-muted-light: "#94A3B8"
  rail-accent-light: "#60A5FA"
  surface-card-light: "#FFFFFF"
  sunken-track-light: "#F1F5F9"
  sunken-hover-light: "#E2E8F0"
  border-hairline-light: "#E5E9F0"
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
  cool-paper-dark: "#090C12"
  rail-dark: "#090C12"
  rail-hover-dark: "#131926"
  rail-active-dark: "#1C2538"
  rail-border-dark: "#1A2232"
  rail-text-dark: "#F8FAFC"
  rail-muted-dark: "#8B9BB4"
  rail-accent-dark: "#60A5FA"
  surface-card-dark: "#131924"
  sunken-track-dark: "#1A2230"
  sunken-hover-dark: "#242E42"
  border-hairline-dark: "#232E40"
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
    fontSize: "15px"
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
  ext-headline:
    fontFamily: "Inter, PingFang SC, Hiragino Sans GB, Microsoft YaHei, WenQuanYi Micro Hei, sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: "-0.015em"
  ext-title:
    fontFamily: "Inter, PingFang SC, Hiragino Sans GB, Microsoft YaHei, WenQuanYi Micro Hei, sans-serif"
    fontSize: "16.5px"
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: "-0.01em"
  ext-body:
    fontFamily: "Inter, PingFang SC, Hiragino Sans GB, Microsoft YaHei, WenQuanYi Micro Hei, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
    fontFeature: "tnum"
  ext-mono:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace"
    fontSize: "12.5px"
    fontWeight: 400
    fontFeature: "tnum"
rounded:
  sm: "5px"
  md: "6px"
  lg: "8px"
  pill: "999px"
spacing:
  xs: "2px"
  sm: "4px"
  md: "6px"
  lg: "8px"
  xl: "12px"
  xxl: "16px"
components:
  button-primary:
    backgroundColor: "{colors.primary-blue-light}"
    textColor: "{colors.on-primary-light}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    height: "24px"
    padding: "0 8px"
  button-primary-hover:
    backgroundColor: "{colors.primary-blue-hover-light}"
  button-primary-active:
    backgroundColor: "{colors.primary-blue-pressed-light}"
  button-primary-dark:
    backgroundColor: "{colors.primary-blue-dark}"
    textColor: "{colors.on-primary-dark}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    height: "24px"
    padding: "0 8px"
  button-primary-dark-hover:
    backgroundColor: "{colors.primary-blue-hover-dark}"
  button-primary-dark-active:
    backgroundColor: "{colors.primary-blue-pressed-dark}"
  button-secondary:
    backgroundColor: "{colors.sunken-track-light}"
    textColor: "{colors.ink-slate-light}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    height: "24px"
    padding: "0 8px"
  button-secondary-hover:
    backgroundColor: "{colors.sunken-hover-light}"
  button-icon:
    backgroundColor: "{colors.sunken-track-light}"
    textColor: "{colors.ink-slate-light}"
    rounded: "{rounded.sm}"
    size: "24px"
    padding: "0"
  pill-filter:
    backgroundColor: "{colors.sunken-track-light}"
    textColor: "{colors.muted-slate-light}"
    typography: "{typography.caption}"
    rounded: "{rounded.pill}"
    height: "20px"
    padding: "0 8px"
  pill-filter-hover:
    backgroundColor: "{colors.sunken-hover-light}"
    textColor: "{colors.ink-slate-light}"
  badge-neutral:
    backgroundColor: "{colors.sunken-track-light}"
    textColor: "{colors.muted-slate-light}"
    typography: "{typography.caption}"
    rounded: "{rounded.pill}"
    height: "18px"
    padding: "0 6px"
  card-surface:
    backgroundColor: "{colors.surface-card-light}"
    textColor: "{colors.ink-slate-light}"
    rounded: "{rounded.md}"
    padding: "7px 9px"
  card-surface-dark:
    backgroundColor: "{colors.surface-card-dark}"
    textColor: "{colors.ink-slate-dark}"
    rounded: "{rounded.md}"
    padding: "7px 9px"
  input-field:
    backgroundColor: "{colors.sunken-track-light}"
    textColor: "{colors.ink-slate-light}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    height: "28px"
    padding: "0 8px"
  input-field-focus:
    backgroundColor: "{colors.surface-card-light}"
  topbar-shell:
    backgroundColor: "{colors.surface-card-light}"
    textColor: "{colors.ink-slate-light}"
    height: "34px"
    padding: "0 12px"
  nav-rail:
    backgroundColor: "{colors.rail-light}"
    textColor: "{colors.rail-muted-light}"
    width: "116px"
    padding: "0"
  nav-rail-collapsed:
    backgroundColor: "{colors.rail-light}"
    width: "44px"
    padding: "0"
  nav-item:
    backgroundColor: "{colors.rail-light}"
    textColor: "{colors.rail-muted-light}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    height: "28px"
    padding: "0 8px"
  nav-item-hover:
    backgroundColor: "{colors.rail-hover-light}"
    textColor: "{colors.rail-text-light}"
---

# Design System: tulai

## Overview

**Creative North Star: "The Multi-Tenant Dispatch Console"**

`tulai` is built as a dense, high-trust desktop operational workbench where window chrome steps back behind live generation queues, multi-account health telemetry, and image output grids. Every surface across the Go/MyGo desktop application and its companion WebExtension page-bridge views operates under unified Light and Dark parity, pairing cool-slate tonal layering with crisp hairline borders so operators can track concurrent jobs across `豆包`, `千问`, `Antigravity`, and `ChatGPT` without visual fatigue.

In Operate mode, density and legibility are deliberate engineering constraints rather than compromises. Fixed-height global bars, compact four-step typography, tabular numerals (`tnum`), and unambiguous semantic states (`空闲`, `出图中`, `等待间隔`, `需要人工验证`, `需登录`, `额度用尽`) keep multi-account pools observable at a glance even at `1024×700`. The system explicitly rejects fragmented Material or Xunlei-style per-page palettes, decorative gradients or glassmorphism blur backdrops, low-contrast grey-on-grey labels, and bloated modal-first workflows that interrupt batch throughput.

**Key Characteristics:**
- Full-height obsidian-slate navigation shell (`#111622` Light / `#090C12` Dark, `116px` expanded / `44px` collapsed) paired with crisp edge-to-edge workspace canvases (`#FFFFFF` Light / `#131924` Dark) divided by single `1px` structural hairlines (`border` `#E5E9F0` / `#232E40`).
- Flat-at-rest panes with zero nested boxed cards, zero rest-state box shadows, and `lift` elevation reserved strictly for active overlays, tooltips, popovers, drawers, and modals.
- Crisp OS native typography (`15px` / `13px` / `12px` / `11px` via `th.Font = ""` / `system-ui`) with mandatory `FontFeatures("tnum")` alignment across counters, quotas, speeds, and timers.
- High-contrast borderless semantic status pills (`>= 4.5:1` contrast via `semanticToneColors` in Light mode and `#090D16` `on-primary-dark` ink on `#60A5FA` primary CTAs in Dark mode).
- GPU-driven micro-interactions (`150ms` fast transitions, `220ms` UI travel) with explicit `ReduceMotion` / `prefers-reduced-motion` static fallbacks.

## Colors

A disciplined dual-theme obsidian-rail and pure-white-stage palette anchored by a single cobalt primary accent and high-contrast semantic status tones shared between desktop MyGo surfaces (`internal/ui/design.go`) and WebExtension pages (`extension/popup.html`, `extension/options.html`).

### Primary
- **Dispatch Cobalt Light** (`colors.primary-blue-light`, `#2563EB`): Primary execution buttons (`开始出图`, `保存并重连`, `新建任务`), focused input strokes, and running progress fills in Light mode.
- **Dispatch Cobalt Hover Light** (`colors.primary-blue-hover-light`, `#1D4ED8`): Hover state fill for Light mode primary action buttons (`th.AccentHover`).
- **Dispatch Cobalt Pressed Light** (`colors.primary-blue-pressed-light`, `#1E40AF`): Active pressed state fill for Light mode primary action buttons (`th.AccentPressed`).
- **On-Primary Ink Light** (`colors.on-primary-light`, `#FFFFFF`): High-contrast text and icon foreground placed over Light mode primary cobalt surfaces.
- **Electric Sky Dark** (`colors.primary-blue-dark`, `#60A5FA`): Primary accent in Dark mode for primary CTAs, focus borders, active rail indicator (`rail-accent-dark`), and live progress bars.
- **Electric Sky Hover Dark** (`colors.primary-blue-hover-dark`, `#93C5FD`): Hover state fill for Dark mode primary action buttons (`th.AccentHover`).
- **Electric Sky Pressed Dark** (`colors.primary-blue-pressed-dark`, `#3B82F6`): Pressed state fill for Dark mode primary action buttons (`th.AccentPressed` and Dark extension `--accent-hover`).
- **Obsidian On-Primary Dark** (`colors.on-primary-dark`, `#090D16`): High-contrast dark ink on `#60A5FA` primary buttons in Dark mode to satisfy `>= 4.5:1` legibility.

### Navigation Shell & Neutral Surfaces
- **Obsidian Rail Light** (`colors.rail-light`, `#111622`) / **Deepest Slate Rail Dark** (`colors.rail-dark`, `#090C12`): Persistent full-height left navigation shell (`railBg`) contrasting cleanly against the light or dark operational canvas.
- **Rail Interaction Tokens:** Hover `#1B2333` (`rail-hover-light`) / `#131926` (`rail-hover-dark`), active `#253047` (`rail-active-light`) / `#1C2538` (`rail-active-dark`), divider border `#222B3C` / `#1A2232`, bright ink `#F8FAFC`, muted rail ink `#94A3B8` / `#8B9BB4`, and sky indicator accent `#60A5FA` (`rail-accent-*`).
- **Cool Paper Light** (`colors.cool-paper-light`, `#F8FAFC`) / **Obsidian Canvas Dark** (`colors.cool-paper-dark`, `#090C12` shell; `#0D1118` `paper`, `#10151F` `rail` in `design.go`): Base canvas and recessed group headers.
- **Pure Stage White** (`colors.surface-card-light`, `#FFFFFF`) / **Carbon Slate Card Dark** (`colors.surface-card-dark`, `#131924`): Primary workspace surface (`card`), `34px` global topbar, prompt editor canvas (`uiPromptArea`), and active segmented tabs.
- **Mist Slate Light** (`colors.sunken-track-light`, `#F1F5F9`) / **Deep Recessed Slate Dark** (`colors.sunken-track-dark`, `#1A2230`): Recessed tracks (`sunken`), resting secondary controls, filter tabs, and column headers.
- **Steel Hover Light** (`colors.sunken-hover-light`, `#E2E8F0`) / **Elevated Slate Hover Dark** (`colors.sunken-hover-dark`, `#242E42`; `#232D42` in `design.go`): Hover and active track surfaces (`sunkenHi`).
- **Hairline Mist Light** (`colors.border-hairline-light`, `#E5E9F0`) / **Abyssal Hairline Dark** (`colors.border-hairline-dark`, `#232E40`; `#222B3C` in `design.go`): Standard `1px` structural hairlines (`border`) dividing full-bleed split panes, topbar, settings sub-navbar, inputs, and table rows.
- **Slate Hairline Strong Light** (`colors.border-strong-light`, `#CBD5E1`) / **Steel Hairline Strong Dark** (`colors.border-strong-dark`, `#334155`): Emphasized `1px` borders (`borderStrong`) on hovered inputs, active tabs, expanded feature toggles, popovers, dashed dropzones, and unchecked switches.
- **Ink Slate Light** (`colors.ink-slate-light`, `#0F172A`) / **Frost White Dark** (`colors.ink-slate-dark`, `#F1F5F9`): Primary body and headline text (`text`), plus inverted tooltip surfaces (`th.Inverse`).
- **Graphite Muted Light** (`colors.muted-slate-light`, `#526075`) / **Cool Pewter Dark** (`colors.muted-slate-dark`, `#94A3B8`): Secondary labels (`muted`), calm metadata hints (`本次发送至：...`), table headings, and neutral badges.
- **Silver Subtle Light** (`colors.subtle-slate-light`, `#8492A6`) / **Slate Subtle Dark** (`colors.subtle-slate-dark`, `#64748B`): Tertiary table captions, row index numerals, and zero-count indicators.

### Semantic
- **Signal Emerald Light** (`colors.signal-emerald-light`, `#15803D`) / **Mint Signal Dark** (`colors.signal-emerald-dark`, `#34D399`): Completed generation/download states (`完成`), healthy multi-account pool indicator, running browser instances (`运行中`), and verified batch notices.
- **Amber Alert Light** (`colors.amber-alert-light`, `#B45309`) / **Warm Amber Dark** (`colors.amber-alert-dark`, `#FBBF24`): Cooldown interval countdowns (`等待间隔`), manual verification/review gates (`需要人工验证`, `待验证`, `需核对`), partial pool readiness, and `>= 80%` quota threshold warning bars.
- **Crimson Fault Light** (`colors.crimson-fault-light`, `#DC2626`) / **Coral Alert Dark** (`colors.crimson-fault-dark`, `#F87171`): Execution failures (`失败`), expired/missing login (`需登录`, `未登录`), quota exhaustion (`额度用尽`), rename conflicts (`冲突`), and destructive stop/delete actions.
- **Semantic Wash Derivation:** Borderless desktop badges and lane chips pass base colors through `semanticToneColors` (`11%` wash Light / `16%` Dark, `10%` / `14%` neutral, and `25%` foreground mix toward `text` in Light mode).

### Named Rules
**The Obsidian-Chrome, Paper-Stage Rule.** Keep the full-height `navRail` anchored in deep obsidian slate (`#111622` Light / `#090C12` Dark) so persistent navigation stays calm and visually separated from the pure-white (`#FFFFFF`) or carbon-slate (`#131924`) operational workspace.

**The Semantic Contrast Floor Rule.** Every borderless badge, lane chip, and alert bar maintains `>= 4.5:1` foreground contrast by mixing Light-mode semantic foregrounds `25%` toward `ink-slate-light` and switching Dark-mode primary button ink to `on-primary-dark` (`#090D16`).

**The Cool-Slate Parity Rule.** Desktop MyGo views and `extension/*.html` WebExtension popups/options share synchronized Light and Dark slate/cobalt/emerald/amber/crimson tokens with zero per-page legacy palettes.

## Typography

**Display & Body Font:** Native OS UI font (`th.Font = ""` / `system-ui` on desktop; `system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif` and `Inter` fallback in `extension/*.html`)
**Label/Mono Font:** `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace` (for `#token` and `#port` fields in `extension/*.html`; numeric telemetry columns in desktop MyGo views use `FontFeatures("tnum")` on the native UI font)

**Character:** Legible, compact operational density driven by a clean 4-step scale (`15px` / `13px` / `12px` / `11px`), crisp weight contrast (`400`–`600`), and tabular numeric alignment.

### Hierarchy
- **Display** (`600`, `15px` desktop; `16.5px`–`20px` in `extension/*.html`): Image-edit header, modal titles, empty-state headlines, wizard step headers, and floating log console title.
- **Title** (`600`, `13px`): Section headers, workbench composer card headings (`提示词`, `参考图`), tool aside headers, and browser drawer headers.
- **Body** (`400`–`500`, `12px`; `13px` in `extension/*.html`): Default theme font size (`th.FontSize`), navigation rail labels, `uiBtn` triggers, form controls, and tabular table cells (`tnum`).
- **Caption** (`500`–`600`, `11px`): Borderless `uiBadge` and `uiPill` chips, table column headers, quota counters, elapsed timers, download ETAs, and calm metadata hints.

### Named Rules
**The Tabular Telemetry Rule.** Apply `FontFeatures("tnum")` (or `font-variant-numeric: tabular-nums` in HTML) to every counter, quota meter, elapsed timer, row index, percentage, download speed, and ETA so live updates never jitter horizontally.

**The Native Four-Step Scale Rule.** Desktop workbench copy uses the four-step `11px`–`15px` native UI ramp (`display: 15px`, `title: 13px`, `body: 12px`, `caption: 11px`); larger `16.5px`–`20px` headings appear only inside standalone WebExtension `popup.html` and `options.html` surfaces.

## Layout

`tulai` uses a two-column global application shell (`ui.Row[navRail, Column[topbar, page]]`, `interfaceZoom: 1.0`) where the obsidian navigation rail spans the full window height `0..winH` with zero topbar overhang, keeping prompts, live multi-account queues, and image grids dominant at `1024×700`+.

- **Navigation Rail (`navRail`):** Full-height left rail (`0..winH`, `railBg`) at `116px` expanded (auto-collapsing below `840px` or manually via `nav-toggle` to `44px` icon-only rail). Houses a `34px` aligned `nav-head` (`tulai` logo + wordmark + dark-rail `nav-toggle` ghost button) matching the height of `topbar`, followed by grouped `28px` navigation items (`padding: 0 8px`).
- **Global Topbar (`topbar`):** Positioned to the right of `navRail` (`height: 34px`, `padding: 0 12px`, `card` background, `1px` bottom hairline `border`). Embeds workbench mode tabs (`自由生图`, `批量洗图`, `裂变图片`) + provider switcher on workbench views or concise page context on tool views, paired with right-aligned task history/new-task controls, account-pool status, running task pill, and floating log console toggle.
- **Edge-to-Edge Split Panes:** Standard workspace pages sit flush inside `page` without outer boxed card padding, using single `1px` structural hairlines between columns:
  - **Workbench (`homePage` / `washPage` / `fissionPage`):** Two-column split above `810px` placing the flexible prompt/reference composer beside the right account-pool lanes and results grid.
  - **`图片编辑` (`imageedit_page.go`):** Flush `260px` left operation stack (`editAside`) and `0`-radius sections divided by `1px` hairlines beside the flexible preview stage and filmstrip.
  - **`提示词` (`prompt_page.go`):** Flush split pane (`>= 720px`) with a `1px` vertical divider (switching to a `1px` horizontal divider below `720px`) between template list (`名称` / `来源` / `类型`) and prompt editor.
  - **`批量重命名` (`rename_page.go`):** Edge-to-edge split panes (`>= 820px`) over a clean action bar with bounded `renameTabs` segmented control (`MaxWidth(540)`, `34px` height).
  - **`去重` (`dedup_page.go`) & `下载` (`download_page.go`):** Flush configuration/toolbar chrome over scrollable result workbenches (`>= 760px` dedup split, `>= 860px` download toolbar).
  - **`浏览器` (`views.go`, `browser_bulk.go`):** Unified `browserTopControlBand` (combining `Camoufox 账号环境`, runtime badge, `网页图片下载专用环境` readiness, and busy status without a duplicate header strip) above progressive `browserEnvironmentTable` and `browserBatchToolbar` (`card` bg).
  - **`全局设置` (`workbench_view.go`):** Flush top sub-navbar (`card` bg, bottom `1px` hairline, `badgeTone("accent", tok)` active tab) over centered `MaxWidth(920)` scroll content (`settingsCard` `.Grow(1)`).
  - **`扩展` (`extension/popup.html`, `extension/options.html`):** Fixed `356px` popup (`14px` padding) and `520px` max-width options wrap.

### Named Rules
**The Full-Height Rail, Flush-Pane Rule.** Anchor `navRail` (`116px` / `44px`) edge-to-edge vertically (`0..winH`) aligned with the `34px` topbar, and separate in-page panes with single `1px` structural hairlines rather than inset grey gutters or nested boxes so prompts and generated images command maximum viewport area.

## Elevation & Depth

Surfaces establish hierarchy strictly through obsidian rail vs pure-white canvas contrast (`railBg` vs `card`/`paper`) and crisp `1px` hairline borders (`border` / `borderStrong`). Rest-state cards, split panes, toolbars, extension cards (`--shadow: none`), and tables remain completely shadow-free; `shadow` (`rgba(15, 23, 42, 0.05)` Light / `rgba(0, 0, 0, 0.36)` Dark) and `lift` (`rgba(15, 23, 42, 0.10)` Light / `rgba(0, 0, 0, 0.46)` Dark) are reserved exclusively for floating chrome.

### Floating Elevation Layers
- **Tooltip & Popover Menus:** Subtle floating separation on `railTip` / `dlTip` inverse tooltips and `motionSelect` dropdown popups.
- **Search Popovers & Update Banners:** Elevated over workbench content with hairline border + `lift` drop shadow.
- **Modal Dialogs & Slide-Over Browser Drawer:** Centered tool modals (`editRecipeModal`, `editResultModal`, `galleryModal`) and the right-docked `Camoufox` interactive browser drawer use backdrop scrim + `lift` separation.
- **Floating Log Console (`logFloat`):** Draggable dark runtime console anchored cleanly above Light or Dark workspaces.

### Named Rules
**The Shadow-Free Rest State Rule.** Zero box shadows on rest cards, tables, panels, or extension UI (`--shadow: none`); use single `1px` structural hairlines at rest and reserve `lift` shadows strictly for floating tooltips, popovers, drawers, modals, and the log console.

**The Reduced-Motion Fidelity Rule.** GPU transitions run at `150ms` (`motionFast`) for micro-interactions (`scale(1.012)` hover, `scale(0.985)` press, floating labels, color anims) and `220ms` (`motionUI`) for `12px` travel/reveal transitions without list-height reflow, degrading cleanly to static or opacity-only states when `ReduceMotion` is enabled.

## Shapes

The system enforces crisp geometric corners (`5px` / `6px` / `8px` / `999px`) alongside flush `0`-radius structural split panes (`imageedit_page.go` sections, edge-to-edge split dividers) and `1px` solid hairlines (`border`, upgrading to `borderStrong` on hover/open and `accent` on focus):

- **Small Controls (`rounded.sm`, `5px` — `radiusS` / `th.Radius`):** Standard buttons (`uiPrimaryBtn`, `uiBtn`, `uiMiniBtn`, `uiIconBtn`, `uiIconTextBtn`), nav items, and text/number inputs.
- **Containers & Prompt Canvases (`rounded.md`, `6px` — `radiusM`):** `uiCard`, `uiCardSurface`, `uiPromptArea`, `laneTable`, `browserEnvironmentTable`, and extension panels.
- **Overlays & Consoles (`rounded.lg`, `8px` — `radiusL`):** Modal dialogs, floating `logFloat` window, and empty-state icon wells.
- **Status Pills (`rounded.pill`, `999px`):** Borderless `uiBadge`, `statusPill`, `laneChip`, and `uiPill` filter chips.
- **Flush Sections & Functional Radii:** Flush split-pane sections use `0` radius with `1px` side/bottom hairlines (`border`), `5px` `quotaBar` tracks use soft pill rounding, and empty dropzones use `1px` dashed `borderStrong` strokes turning `1px`–`2px` solid `accent` on hover or file drag-over.

### Named Rules
**The Zero-Nesting Corner Rule.** Use `0` radius on flush hairline split panes, `5px` on interactive controls, `6px` on standalone cards and prompt editors, `8px` on floating overlays, and `999px` on borderless pills—never nest rounded bordered boxes inside other rounded bordered boxes.

## Components

Every workbench control and utility page composes from shared `internal/ui/design.go` primitives with synchronized hover, focus, active, and disabled feedback.

### Buttons
Crisp `24px`–`28px` triggers with `5px` corners (`radiusS`) and single-node labeling for clean accessible hit targets and testability.
- **Primary (`uiPrimaryBtn`):** Single `ui.Button(...)` node using `primary-blue-light` (`#2563EB`) + `#FFFFFF` ink in Light mode and `primary-blue-dark` (`#60A5FA`) + `on-primary-dark` (`#090D16`) ink in Dark mode.
- **Secondary / Ghost / Mini / Icon:** `uiBtn` (`24px` height, `sunken` fill with `1px` `border`), lighter ghost `uiIconBtn` (transparent at rest, used cleanly in `nav-head` and compact action rows), `uiMiniBtn`, and `uiIconTextBtn`.

### Chips & Borderless Semantic Badges
Borderless `uiBadge` and `uiPill` pills (`0`px border at rest, `999px` pill radius, `>= 4.5:1` contrast via `semanticToneColors`, `11px` `tnum` text) delivering clean telemetry without perimeter noise:
- **Tones:** `accent` (`11%` wash Light / `16%` Dark), `success`, `warning`, `danger`, and `neutral` (`10%` wash Light / `14%` Dark).

### Prompt Canvases, Cards & Flush Sections
Clean `uiPromptArea` pure-white (`#FFFFFF` / `#131924`) prompt editors free of sunken grey wells, flat `uiCard` containers (`Padding(8, 10)`, `Gap(6)`), and flush `0`-radius split sections separated by `1px` `border` hairlines (`imageedit_page.go`, `prompt_page.go`, `rename_page.go`, `settingsPage`).

### Inputs / Fields
Crisp inputs (`28px` standard height, `34px` floating-label `renameField`, `sunken` resting fill stepping to `card` background with `accent` border on focus) and inline `1px` error telemetry.

### Navigation
Full-height (`0..winH`) obsidian-slate `navRail` (`116px` expanded / `44px` collapsed below `840px`) aligned via its `34px` `nav-head` (`tulai` brand + ghost `nav-toggle`) flush alongside the `34px` `topbar` (`[navRail, Column[topbar, page]]`), removing topbar overhang and redundant toggles.

### Multi-Tenant Lane & Browser Tables
Purpose-built operational data tables packing account toggles, borderless status pills, `5px` `quotaBar` progress meters (`tnum`), `gapRing` cooldown timers, and `Camoufox` environment actions into crisp hairline-divided rows (`laneTable`, `browserEnvironmentTable`, `downloadBody`), plus consolidated `browserTopControlBand` and flush `browserBatchToolbar` (`card` bg).

## Do's and Don'ts

### Do:
- **Do** derive every surface, rail tone, border, text, and semantic tone from `workbenchTokens(dark)` and shared helpers (`uiCardSurface`, `uiBadge`, `uiPrimaryBtn`, `uiBtn`, `uiIconBtn`, `uiPill`, `uiPromptArea`) in `internal/ui/design.go`.
- **Do** apply `FontFeatures("tnum")` (and `font-variant-numeric: tabular-nums` in HTML) on all counters, quota meters, elapsed timers, row indices, percentages, download speeds, and ETAs.
- **Do** enforce `>= 4.5:1` contrast across all borderless semantic badges and primary CTAs using `semanticToneColors` in Light mode and `on-primary-dark` (`#090D16`) ink on `#60A5FA` primary buttons in Dark mode.
- **Do** honor `c.Preferences().ReduceMotion` and `@media (prefers-reduced-motion: reduce)` cleanly.
- **Do** keep all browser login, verification, and page-bridge workflows inside built-in `Camoufox` environments (`browserDrawerView`) and `extbridge` extension surfaces.
- **Do** preserve the full-height obsidian shell (`navRail` `116px` / `44px` spanning `0..winH`, `34px` `nav-head` & `topbar`) and flush hairline split panes so prompt editors and output images command viewport space.

### Don't:
- **Don't** nest bordered cards inside bordered cards, draw duplicate page headers, or add rest-state box shadows (`--shadow: none`) on panes or extension views.
- **Don't** put `nav-toggle` in `topbar` or wrap `topbar` across `navRail` with overhang; keep `nav-toggle` aligned inside the `34px` `nav-head` column.
- **Don't** duplicate button label text nodes (`ui.ButtonBase(...).Label(x)` + `ui.Text(c, x)`)—use single-node `uiPrimaryBtn` / `ui.Button`.
- **Don't** render low-contrast grey-on-grey labels, `#FFFFFF` text on Dark `#60A5FA` primary CTAs, or thick `>1px` decorative accent bars.
- **Don't** put top-level `motion`, `shadows`, or `breakpoints` keys in `DESIGN.md` YAML frontmatter (keep them in `.impeccable/design.json` `extensions`), and don't animate list row heights during enter/exit transitions.
- **Don't** launch, control, or design UI flows around external browsers (`Chrome`, `Edge`, `Chromium`) outside the built-in `Camoufox` runtime and `extbridge` extension UI.
