---
name: "tulai"
description: "Multi-provider, multi-account desktop AI batch image dispatch console with Light/Dark operational parity."
colors:
  primary-blue-light: "#2563EB"
  primary-blue-hover-light: "#1D4ED8"
  primary-blue-pressed-light: "#1E40AF"
  on-primary-light: "#FFFFFF"
  cool-paper-light: "#F6F8FA"
  rail-light: "#F9FAFC"
  surface-card-light: "#FFFFFF"
  sunken-track-light: "#F0F3F7"
  sunken-hover-light: "#E6EBF2"
  border-hairline-light: "#E1E6EF"
  border-strong-light: "#CBD4E1"
  ink-slate-light: "#0F172A"
  muted-slate-light: "#5B677A"
  subtle-slate-light: "#8A96A8"
  signal-emerald-light: "#15803D"
  amber-alert-light: "#B45309"
  crimson-fault-light: "#DC2626"
  primary-blue-dark: "#60A5FA"
  primary-blue-hover-dark: "#93C5FD"
  primary-blue-pressed-dark: "#3B82F6"
  on-primary-dark: "#090D16"
  cool-paper-dark: "#0B0E14"
  rail-dark: "#0F131B"
  surface-card-dark: "#141923"
  sunken-track-dark: "#1B2230"
  sunken-hover-dark: "#242D3F"
  border-hairline-dark: "#252F42"
  border-strong-dark: "#34425A"
  ink-slate-dark: "#F1F5F9"
  muted-slate-dark: "#94A3B8"
  subtle-slate-dark: "#64748B"
  signal-emerald-dark: "#34D399"
  amber-alert-dark: "#FBBF24"
  crimson-fault-dark: "#F87171"
typography:
  display:
    fontFamily: "Inter, WenQuanYi Micro Hei"
    fontSize: "13.5px"
    fontWeight: 600
  title:
    fontFamily: "Inter, WenQuanYi Micro Hei"
    fontSize: "11.5px"
    fontWeight: 600
  body:
    fontFamily: "Inter, WenQuanYi Micro Hei"
    fontSize: "11px"
    fontWeight: 400
    fontFeature: "tnum"
  caption:
    fontFamily: "Inter, WenQuanYi Micro Hei"
    fontSize: "10px"
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
  sm: "4px"
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
    padding: "0 10px"
  nav-rail:
    backgroundColor: "{colors.rail-light}"
    textColor: "{colors.muted-slate-light}"
    width: "134px"
    padding: "8px 6px"
  nav-rail-collapsed:
    backgroundColor: "{colors.rail-light}"
    width: "42px"
    padding: "8px 0"
  nav-item:
    backgroundColor: "{colors.rail-light}"
    textColor: "{colors.muted-slate-light}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    height: "26px"
    padding: "0 8px"
  nav-item-hover:
    backgroundColor: "{colors.sunken-hover-light}"
    textColor: "{colors.ink-slate-light}"
---

# Design System: tulai

## Overview

**Creative North Star: "The Multi-Tenant Dispatch Console"**

`tulai` is built as a dense, high-trust desktop operational workbench where window chrome steps back behind live generation queues, multi-account health telemetry, and image output grids. Every surface across the Go/MyGo desktop application and its companion WebExtension page-bridge views operates under unified Light and Dark parity, pairing cool-slate tonal layering with crisp hairline borders so operators can track concurrent jobs across `豆包`, `千问`, `Antigravity`, and `ChatGPT` without visual fatigue.

In Operate mode, density and legibility are deliberate engineering constraints rather than compromises. Fixed-height global bars, compact four-step typography, tabular numerals (`tnum`), and unambiguous semantic states (`空闲`, `出图中`, `等待间隔`, `需要人工验证`, `需登录`, `额度用尽`) keep multi-account pools observable at a glance even at `1024×700`. The system explicitly rejects fragmented Material or Xunlei-style per-page palettes, decorative gradients or glassmorphism blur backdrops, low-contrast grey-on-grey labels, and bloated modal-first workflows that interrupt batch throughput.

**Key Characteristics:**
- Cool-slate tonal hierarchy (`paper` canvas, `rail` navigation, `card` panels, `sunken` / `sunkenHi` recessed tracks) across full Light and Dark theme parity.
- Hairline `1px` borders (`border` / `borderStrong`) for structural clarity at rest, reserving diffuse `shadow` / `lift` layers for active focus, popovers, drawers, and modals.
- Compact desktop typography (`10px` to `13.5px`) paired with `Inter, WenQuanYi Micro Hei` and mandatory `FontFeatures("tnum")` alignment across counters, quotas, speeds, and timers.
- High-contrast semantic status pills (`>= 4.5:1` contrast via `semanticToneColors` mixing in Light mode and `#090D16` foreground on `#60A5FA` primary CTAs in Dark mode).
- GPU-driven micro-interactions (`150ms` fast transitions, `220ms` UI travel) with explicit `ReduceMotion` / `prefers-reduced-motion` static fallbacks.

## Colors

A disciplined dual-theme cool-slate palette anchored by a single cobalt primary accent and high-contrast semantic status tones shared identically between desktop MyGo surfaces (`internal/ui/design.go`) and WebExtension pages (`extension/popup.html`, `extension/options.html`).

### Primary
- **Dispatch Cobalt Light** (`colors.primary-blue-light`, `#2563EB`): Primary execution buttons (`开始出图`, `保存并重连`, `新建任务`), active navigation rail bar, focused input strokes, and running progress fills in Light mode.
- **Dispatch Cobalt Hover Light** (`colors.primary-blue-hover-light`, `#1D4ED8`): Hover state fill for Light mode primary action buttons (`th.AccentHover`).
- **Dispatch Cobalt Pressed Light** (`colors.primary-blue-pressed-light`, `#1E40AF`): Active pressed state fill for Light mode primary action buttons (`th.AccentPressed`).
- **On-Primary Ink Light** (`colors.on-primary-light`, `#FFFFFF`): High-contrast text and icon foreground placed over Light mode primary cobalt surfaces.
- **Electric Sky Dark** (`colors.primary-blue-dark`, `#60A5FA`): Primary accent in Dark mode for primary CTAs, active nav accent indicator, focus borders, and live progress bars.
- **Electric Sky Hover Dark** (`colors.primary-blue-hover-dark`, `#93C5FD`): Hover state fill for Dark mode primary action buttons (`th.AccentHover`).
- **Electric Sky Pressed Dark** (`colors.primary-blue-pressed-dark`, `#3B82F6`): Pressed state fill for Dark mode primary action buttons (`th.AccentPressed` and Dark extension `--accent-hover`).
- **Obsidian On-Primary Dark** (`colors.on-primary-dark`, `#090D16`): High-contrast dark ink on `#60A5FA` primary buttons in Dark mode to satisfy `>= 4.5:1` legibility.

### Neutral
- **Cool Paper Light** (`colors.cool-paper-light`, `#F6F8FA`) / **Obsidian Canvas Dark** (`colors.cool-paper-dark`, `#0B0E14`): Base application canvas (`paper`) and floating dark log console background (`#0B0E14`).
- **Porcelain Rail Light** (`colors.rail-light`, `#F9FAFC`) / **Midnight Rail Dark** (`colors.rail-dark`, `#0F131B`): Navigation rail (`rail`), alternating lane table rows, grouped download headers, and log console header chrome.
- **Crisp Card White** (`colors.surface-card-light`, `#FFFFFF`) / **Carbon Slate Card Dark** (`colors.surface-card-dark`, `#141923`): Elevated panel surfaces (`card`), global topbar, active segmented tabs, and focused input fields.
- **Mist Slate Light** (`colors.sunken-track-light`, `#F0F3F7`) / **Deep Recessed Slate Dark** (`colors.sunken-track-dark`, `#1B2230`): Recessed tracks (`sunken`), resting input wells, segmented tab bars, table column headers, and secondary/ghost hover surfaces.
- **Steel Hover Light** (`colors.sunken-hover-light`, `#E6EBF2`) / **Elevated Slate Hover Dark** (`colors.sunken-hover-dark`, `#242D3F`): Hover and active track surfaces (`sunkenHi`), active filter tabs in Dark mode, and subtle interactive emphasis.
- **Hairline Mist Light** (`colors.border-hairline-light`, `#E1E6EF`) / **Abyssal Hairline Dark** (`colors.border-hairline-dark`, `#252F42`): Standard `1px` structural borders (`border`) separating topbar, navigation rail, cards, inputs, and table rows.
- **Slate Hairline Strong Light** (`colors.border-strong-light`, `#CBD4E1`) / **Steel Hairline Strong Dark** (`colors.border-strong-dark`, `#34425A`): Emphasized `1px` borders (`borderStrong`) on hovered inputs, active tabs, expanded feature cards, popovers, dashed dropzones, and unchecked switch tracks.
- **Ink Slate Light** (`colors.ink-slate-light`, `#0F172A`) / **Frost White Dark** (`colors.ink-slate-dark`, `#F1F5F9`): Primary body and headline text (`text`); also inverted (`th.Inverse`) for dark tooltips (`railTip`, `dlTip`) paired with `#F8FAFC` (Light theme) or `#0B0E14` (Dark theme) inverse text.
- **Graphite Muted Light** (`colors.muted-slate-light`, `#5B677A`) / **Cool Pewter Dark** (`colors.muted-slate-dark`, `#94A3B8`): Secondary labels (`muted`), table column headings, captions, subtitles, and neutral status badge text.
- **Silver Subtle Light** (`colors.subtle-slate-light`, `#8A96A8`) / **Slate Subtle Dark** (`colors.subtle-slate-dark`, `#64748B`): Tertiary group section headers (`nav-group`), row index numerals, and zero-count account-pool indicator dots.

### Semantic
- **Signal Emerald Light** (`colors.signal-emerald-light`, `#15803D`) / **Mint Signal Dark** (`colors.signal-emerald-dark`, `#34D399`): Completed generation/download states (`完成`), healthy multi-account pool indicator, running browser instances (`运行中`), and verified batch notices.
- **Amber Alert Light** (`colors.amber-alert-light`, `#B45309`) / **Warm Amber Dark** (`colors.amber-alert-dark`, `#FBBF24`): Cooldown interval countdowns (`等待间隔`), manual verification/review gates (`需要人工验证`, `待验证`, `需核对`), partial pool readiness, and `>= 80%` quota threshold warning bars.
- **Crimson Fault Light** (`colors.crimson-fault-light`, `#DC2626`) / **Coral Alert Dark** (`colors.crimson-fault-dark`, `#F87171`): Execution failures (`失败`), expired/missing login (`需登录`, `未登录`), quota exhaustion (`额度用尽`), rename conflicts (`冲突`), and destructive stop/delete actions.
- **Semantic Wash Derivation:** Desktop badges, lane chips, and alert bars pass base colors through `semanticToneColors` (`10%`–`14%` translucent fill, `22%` alpha border, and `25%` mix toward `text` in Light mode); `imageedit_page.go` additionally uses indigo `#6366F1` (Light) / `#818CF8` (Dark) strictly for the crop/resize feature card icon accent.

### Named Rules
**The Single-Accent Dispatch Rule.** Primary cobalt (`#2563EB` Light / `#60A5FA` Dark) is reserved for active execution CTAs, focused input borders, the `2.5px` active navigation indicator, and running state telemetry; secondary actions and structural chrome stay cool-slate neutral.

**The Semantic Contrast Floor Rule.** Every badge, lane chip, and alert bar maintains `>= 4.5:1` foreground contrast by mixing Light-mode semantic foregrounds `25%` toward `ink-slate-light` and switching Dark-mode primary button ink to `on-primary-dark` (`#090D16`).

**The Cool-Slate Parity Rule.** Desktop MyGo views and `extension/*.html` WebExtension popups/options share identical Light and Dark hex tokens with zero per-page legacy palettes.

## Typography

**Display Font:** Inter, WenQuanYi Micro Hei (with OS system sans fallback in `extension/*.html`)
**Body Font:** Inter, WenQuanYi Micro Hei
**Label/Mono Font:** ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace (used for `#token` and `#port` fields in `extension/*.html`; numeric columns in desktop MyGo views use `FontFeatures("tnum")` on `Inter, WenQuanYi Micro Hei`)

**Character:** Compact, utilitarian desktop density where a tight `3.5px` span (`10px` to `13.5px`) establishes clear hierarchy through weight (`400`–`700`), ink contrast, and tabular numeric alignment instead of oversized display headings.

### Hierarchy
- **Display** (`600`–`700`, `13.5px` desktop; `16.5px`–`20px` in `extension/*.html`): Image-edit header (`批处理`), empty-state headlines, recipe/result modal titles, wizard step titles, and floating log console header.
- **Title** (`600`–`700`, `11.5px`): Standard `uiPageHeader` page headings (`Bold`), workbench input card headings (`提示词`, `参考图`), `处理流程` aside title, and browser drawer headers.
- **Body** (`400`–`500`, `11px`; `13px`–`14px` in `extension/*.html`): Default workbench theme font size (`th.FontSize`), navigation rail labels, `uiBtn` buttons, `uiSectionTitle` headings (`600` weight), and table cell values (`tnum`).
- **Caption** (`500`–`600`, `10px`; `10.5px` on `statusPill` and `imageedit` card summaries; `9.5px` on rail group labels and log count chip): `uiBadge` and `laneChip` pills, table column headers (`600` weight), quota counters, elapsed timers, download ETAs, and helper hints.

### Named Rules
**The Tabular Telemetry Rule.** Apply `FontFeatures("tnum")` (or `font-variant-numeric: tabular-nums` in HTML) to every counter, quota meter, elapsed timer, row index, percentage, download speed, and ETA so live updates never jitter horizontally.

**The Compact Scale Ceiling Rule.** Desktop workbench copy stays within the four-step `10px`–`13.5px` scale (`display: 13.5px`, `title: 11.5px`, `body: 11px`, `caption: 10px`); larger `16.5px`–`20px` headings appear only inside standalone WebExtension `popup.html` and `options.html` surfaces.

## Layout

`tulai` uses a fixed topbar + collapsible navigation rail shell (`interfaceZoom: 1.0`) designed to keep live multi-account queues and image grids dominant at `1024×700` and adapt cleanly down to narrow splits.

- **Global Topbar (`topbarH: 34px`):** Full-width `34px` bar (`padding: 0 10px`, `gap: 8px`, `1px` bottom border) housing the `24×24` `nav-toggle` button, mode tabs (`自由生图`, `批量洗图`, `裂变图片`) + provider switcher (`>= 810px`) on workbench tabs or page icon + title (+ subtitle at `>= 920px`) on tool tabs, plus right-aligned history/new-task controls, account-pool health indicator (`>= 920px`), `运行中` live pill, and floating log toggle.
- **Navigation Rail (`navRail`):** Full-height left rail (`1px` right border, `3px` vertical item gap) at `sidebarExpandedW: 134px` (`padding: 8px 6px`) for viewports `>= 840px`; folds automatically at `sidebarFoldW: 840px` (or manually via `nav-toggle`) to `sidebarCollapsedW: 42px` (`padding: 8px 0`, icon-only `28×28` items with right-anchored `railTip` tooltips). Organizes 8 destinations into 4 groups (`工作台`, `资产与环境`, `批处理`, `系统`) plus bottom `设置向导` and `检查更新` utilities.
- **Workbench Master-Detail (`homeSplitMin: 810px`):** Standard pages apply `6px 8px` outer padding (`pagePadX: 8px`). Above `810px`, `homePage` / `washPage` / `fissionPage` render a two-column split (`homeGap: 6px`): fixed-width left composer (`composerW: 248px`, widening to `composerWideW: 272px` at `>= 1200px` `composerWideMin`) with pinned bottom `runCard`, beside a flexible right stage stacking `accountPanel` (`laneTable`) over `resultsPane`. Below `810px`, collapses into a scrollable single column with an inline `platformPicker`.
- **Dense Tool & Table Splits:**
  - `批量重命名` (`rename_page.go`): Edge-to-edge outer container with `8px 12px` inner workspace padding; splits at `>= 820px` (except manual mode) into a `316px` (`344px` at `>= 1040px`) rule card and flexible preview list above a `38px` bottom action toolbar.
  - `图片编辑` (`imageedit_page.go`): Fixed `256px` left operation stack (`editAside`) paired with a flexible right preview stage and filmstrip.
  - `去重` (`dedup_page.go`): Splits at `>= 760px` into a `268px` (`276px` at `>= 940px`) configuration sidebar and flexible result workbench.
  - `提示词` (`prompt_page.go`): Splits at `>= 720px` into a `248px` (`280px` at `>= 900px`) template list and flexible prompt editor.
  - `浏览器` (`views.go`, `browser_table.go`): Top runtime + download-browser cards sit 2-up at `>= 920px`; `browserEnvironmentTable` progressively reveals `编号` (`>= 900px`), `今日用量` (`>= 1000px`), and `代理` (`>= 1100px`) columns.
  - `下载` (`download_page.go`): Sub-toolbar flips between single-row (`>= 860px`) and two-row (`< 860px`); composer modal spans `min(560px, max(260px, w - 80px))`.
  - `全局设置` (`workbench_view.go`): Centered `920px` max-width column with 2-up cards at `>= 720px`.
  - `扩展` (`extension/popup.html`, `extension/options.html`): Fixed `356px` popup (`14px` padding) and `520px` max-width options wrap (`36px 20px` padding).

### Named Rules
**The Stage-First Viewport Rule.** Navigation rails (`134px` / `42px`), workbench composers (`248px` / `272px`), and tool asides (`248px`–`344px`) stay strictly bounded so live account lanes, image grids, and batch tables command the majority of `1024×700`+ windows.

## Elevation & Depth

Surfaces establish hierarchy primarily through cool-slate tonal stepping (`paper` → `rail` → `card` → `sunken` → `sunkenHi`) and `1px` hairline borders (`border` / `borderStrong`), keeping rest states crisp and flat. Light-mode step cards and wizard cards add a subtle `1px` top inner highlight (`inset 0 1px 0 0 rgba(255, 255, 255, 0.9)`), while diffuse shadows built from `shadow` (`rgba(15, 23, 42, 0.06)` Light / `rgba(0, 0, 0, 0.36)` Dark) and `lift` (`rgba(15, 23, 42, 0.08)` Light / `rgba(0, 0, 0, 0.46)` Dark) signal active cards and floating layers.

### Shadow Vocabulary
- **Rest Card Shadow** (`0 1px 2px 0` using `shadow`): Applied subtly to `laneTable`, workbench `stepCard`, enabled `editFeatureCard` panels, primary `editAction`, and active Light-mode `stageButton` tabs.
- **Wizard & Session Lift** (`0 1px 3px 0` / `0 2px 6px 0` using `shadow`): Light separation on `wizardCard` panels and session history cards (`session_ui.go`).
- **Active Step Emphasis** (`0 3px 10px 0` using `lift` + `1px` `accent` at `55%` alpha border): Elevates the current workbench `stepCurrent` card above adjacent cards.
- **Floating Tooltips & Menus** (`0 4px 14px 0 rgba(16, 32, 64, 0.18)` on `railTip` / `dlTip`; `0 6px 20px 0` on `motionSelect` / `renameTemplateSelect` popups): Separates ephemeral overlays from dense rows.
- **Search Popovers & Banners** (`0 8px 24px 0` using `lift`): Anchors `downloadSearchFab` popovers and `updateBanner` notices.
- **Modal Dialogs** (`0 10px 28px 0` on `editRecipeModal` / `editResultModal`; `0 14px 36px 0` on `galleryModal`): Isolates blocking dialogs over workbench surfaces.
- **Slide-Over Browser Drawer** (`-8px 0 28px 0` using `lift`, paired with `rgba(15, 23, 42, 0.28)` Light / `rgba(2, 6, 23, 0.50)` Dark backdrop scrim): Separates the right-docked `Camoufox` interactive browser drawer (`min(w, max(580px, 64%))`).
- **Floating Log Console** (`0 16px 40px 0 rgba(0, 0, 0, 0.50)`): Anchors the draggable, always-dark (`#0B0E14`) `logFloat` window above Light or Dark workspaces.

### Named Rules
**The Hairline-First Elevation Rule.** Rest panels separate by cool-slate tonal step and `1px` `border` strokes; reserve `lift` shadows strictly for active step focus, popovers, drawers, modals, and floating consoles.

**The Reduced-Motion Fidelity Rule.** GPU transitions run at `150ms` (`motionFast`) for micro-interactions (`scale(1.012)` hover, `scale(0.985)` press, floating labels, color anims) and `220ms` (`motionUI`) for `12px` travel/reveal transitions without list-height reflow, degrading cleanly to static or opacity-only states when `ReduceMotion` is enabled.

## Shapes

The system enforces tight, geometric corner discipline (`4px` / `6px` / `8px` / `999px`) paired with consistent `1px` solid strokes (`border`, upgrading to `borderStrong` on hover/open and `accent` on focus):

- **Small Controls (`rounded.sm`, `4px` — `radiusS`):** Standard buttons (`uiBtn`, `uiMiniBtn`, `uiIconBtn`, `uiIconTextBtn`), text/number inputs (`renameField`, `editOutputCard`, `browserPane`), `16×16` checkboxes, `24×24` header icon boxes, and `22×22` download site tiles (`workspaceModeTabs` inner active thumb uses `3px`; `th.Radius` defaults general MyGo controls to `5px`).
- **Cards & Containers (`rounded.md`, `6px` — `radiusM`):** `uiCard` / `uiCardSurface` panels, `laneTable`, `browserEnvironmentTable`, `dl-body`, `rename-list-panel`, segmented tab frames (`platformTabs`, `downloadFilterTabs`, `renameTabs`, `settingsTabs`), navigation rail rows (`26px` / `28×28` collapsed), and extension popups/inputs.
- **Overlays & Consoles (`rounded.lg`, `8px` — `radiusL`):** Modal dialogs (`editRecipeModal`, `editResultModal`, `galleryModal`), floating `logFloat` window, extension form containers, and `40×40` empty-state icon wells.
- **Status Pills (`rounded.pill`, `999px`):** `uiBadge`, `statusPill`, `laneChip`, `uiPill` filter chips, `topbar-running` indicator, and log count badges.
- **Active Nav & Functional Radii:** `nav-active-bar` draws a `2.5px`-wide vertical `accent` bar (`1.5px` radius, `16px` tall expanded / `14px` collapsed, offset `1px` from left edge); `4px` progress tracks use `2px` radius, `5px` `quotaBar` tracks use `3px` radius, compact switches (`24×14`, `26×15`, `28×16`) use half-height pill radii (`7px`–`8px`), and empty dropzones use `1px` dashed `borderStrong` strokes that turn `1px`–`2px` solid `accent` on hover or file drag-over.

### Named Rules
**The Tight Corner Hierarchy Rule.** Keep interactive buttons and fields at `4px`, cards/tables/nav items at `6px`, floating dialogs and consoles at `8px`, and status pills at `999px`—never mix oversized decorative corner radii into desktop tooling.

## Components

Every workbench control and utility page composes from shared `internal/ui/design.go` primitives with synchronized hover, focus, active, and disabled feedback.

### Buttons
Crisp, low-height (`20px`–`28px`) triggers with `4px` corners and `150ms` `hoverPressPose` scale (`1.012` hover / `0.985` press).
- **Shape:** Tight rectangular (`4px` radius in `uiBtn` / `uiMiniBtn` / `uiIconBtn` / `uiIconTextBtn`; `5px` via `th.Radius` on `runButton`; `6px` on `extension/*.html`).
- **Primary:** `primary-blue-light` (`#2563EB`) + `#FFFFFF` ink in Light mode, `primary-blue-dark` (`#60A5FA`) + `on-primary-dark` (`#090D16`) ink in Dark mode; `24px` height / `0 8px` padding (`uiBtn`), `26px` / `0 12px` on `runButton`, `28px` / `0 10px`–`0 14px` on `downloadNewFab` and modal submits.
- **Hover / Focus:** Transitions over `150ms` to `primary-blue-hover-*` (`#1D4ED8` Light / `#93C5FD` Dark) on hover and `primary-blue-pressed-*` (`#1E40AF` Light / `#3B82F6` Dark) on press; disabled state switches to `sunken` fill + `muted` ink + `1px` `border` (or `0.45` opacity on `editAction`).
- **Secondary / Mini / Icon / Pill:** `uiBtn` (`24px` height, `0 8px` padding, `11px` text) and `uiMiniBtn` (`20px` height, `0 6px` padding, `10px` text) sit on `sunken` with `1px` `border`, brightening to `sunkenHi` + `borderStrong` on hover; `uiIconBtn` (`22×22`, `24×24`, or `28×28` square, `ghost` mode transparent at rest) enforces `Label` + `Tooltip`; `uiIconTextBtn` pairs a `11px`–`12px` Lucide stroke icon (`4px` gap) with concise `10px` copy.

### Chips & Semantic Badges
Compact `18px`–`20px` pills delivering unambiguous account-pool and task telemetry at `>= 4.5:1` contrast.
- **Style:** `uiBadge`, `statusPill`, and `laneChip` render at `18px` height, `0 6px`–`0 7px` horizontal padding, `999px` pill radius, `1px` border at `22%` foreground alpha, and `10px` (`10.5px` in `statusPill`) `500`-weight `tnum` text; `uiPill` filter chips render at `20px` height with `0 8px` padding.
- **State:** `accent` (`12%`–`14%` wash: `生成中`, `进行中`, `使用中`, `待改`), `success` (`完成`, `运行中`, `已安装`), `warning` (`等待间隔 N`, `需要人工验证`, `待验证`, `需核对`, `冷却中`), `danger` (`失败`, `需登录`, `未登录`, `额度用尽`, `冲突`), and `neutral` (`10%` wash: `空闲`, `排队中`, `就绪`, `不变`). Active `uiPill` / `dlPill` filters intensify on hover to `20%` `accent` wash and `42%` border with `600` font weight.

### Cards / Containers
Flat-by-default structural frames separating input composers, feature toggles, and tables without nested box bloat.
- **Corner Style:** `6px` (`rounded.md`) across `uiCard`, `uiCardSurface`, `editFeatureCard`, and dropzones (`8px` on modal dialogs).
- **Background:** `surface-card-light` (`#FFFFFF`) or `surface-card-dark` (`#141923`); recessed empty-dropzones (`compactDrop`) sit on `sunken`.
- **Shadow Strategy:** Flat at rest inside tool pages (`uiCard`), `0 1px 2px 0` `shadow` on `laneTable`, `stepCard`, and enabled `editFeatureCard`, elevating to `0 3px 10px 0` `lift` + `55%` `accent` border on `stepCurrent`.
- **Border:** `1px` solid `border` (`borderStrong` when `editFeatureCard` is enabled; `1px` dashed `borderStrong` on `compactDrop`, shifting to `1px`–`2px` solid `accent` + `8%` wash on hover or file drag-over).
- **Internal Padding:** `7px 9px` (`gap: 5px`) default on `uiCard`, `8px 10px` on `stepCard`, `10px 12px` (`gap: 6px`–`8px`) on `settingsCard` / `dedupPickCard`.

### Inputs / Fields
Recessed `sunken` wells that step forward into crisp `card` surfaces with cobalt focus borders.
- **Style:** `24px`–`30px` height (`34px` on `renameField`, `34px`–`36px` in `extension/*.html`), `sunken` fill, `1px` `border`, `4px` (`6px` extension) corners, `0 8px` horizontal padding; `motionSelect` triggers pair `sunken` fill with a `150ms` rotating `180°` chevron and `shadow`/`lift` dropdown panel.
- **Focus:** Shifts background to `card` and stroke to `1px` `accent` (`th.Focus` `55%` `accent` alpha / `0 0 0 3px` `--focus-ring` in extension HTML). In `renameField`, focus or non-empty value animates (`150ms`) the inline label `-7px` vertically and `0.75×` scale while shifting input top padding to `11px`.
- **Error / Disabled:** Validation errors surface in inline `danger` tone bars (`10%` wash, `22%` border) or `crimson-fault-*` caption text; busy inputs disable interaction uniformly across active generation runs.

### Navigation
Dual-tier global shell pairing a fixed `34px` dispatch topbar with a collapsible `134px` / `42px` navigation rail.
- **Style, typography, default/hover/active states, mobile/compact treatment:** `topbar` (`34px` height, `card` bg, `1px` bottom `border`) embeds `24×24` sidebar toggle, segmented `workspaceModeTabs` (`24px` track on `sunken`, `card` active thumb with `borderStrong`), `platformTabs` (`20px` tabs with provider icons and live running counts), `topbarPoolControl` (`24px` button with `6px` semantic dot + `ready/total 账号`), `topbar-running` live pill, and `topbarLogControl` (`24px` button with unread warning/error dot and `9.5px` log count chip). `navRail` (`134px` expanded on `rail`, folding to `42px` below `840px`) displays the `tulai` + `本地` header, `9.5px` `subtle` group headings, and `26px` (`28×28` collapsed) `6px`-radius tabs: resting `rail` / `muted`, hover `sunkenHi` / `text` with `75%` `border`, pressed `sunken` / `text`, and active `12%` `accent` tint + `1px` `border` + `2.5px` left `accent` bar + `600` weight label (plus right-anchored `railTip` inverse tooltips when collapsed).

### Multi-Tenant Lane & Browser Tables
Purpose-built operational data tables packing account health, quota progress, live tasks, and proxy state into `34px`-tall scannable rows.
- **`laneTable` (`internal/ui/lane_table.go`):** Card container (`padding: 6px 9px`, `shadow: 0 1px 2px 0`) with `laneHeader` (`platformIcon` + title + `tnum` status + `24×14` concurrency switch + `+ 添加账号`), `sunken` column header band, and `34px` rows (`rail` alternating tint, `5%` `accent` running highlight, `sunken` hover). Fixed column tracks (`state: 74px` / `64px` Antigravity, `quota: 132px`, `ops: 96px` / `72px`) collapse `状态` and `当前任务` inline below `< 1150px`. Embeds `26×15` account toggle, `laneChip` pill, `5px`-tall `quotaBar` (`text` at `12%` alpha track; `accent` / `warning` at `>= 80%` / `muted` at `100%`) or dual `5小时`/`7天` `Meter` bars, `18×18` `gapRing` countdown arc, and `22×22` action triggers.
- **`browserEnvironmentTable` & `downloadBody`:** `browserEnvironmentTable` (`internal/ui/browser_table.go`) uses `ui.Table` (`card` bg, `1px` `border`, `6px` radius, `10%` `accent` selection mix) with progressive fixed columns (`选择: 50px`, `编号: 56px`, `环境名称 >= 190px`, `服务商: 86px`, `环境状态: 112px`, `代理: 172px`, `今日用量: 84px`, `操作: 84px`); `downloadBody` (`internal/ui/download_page.go`) uses `34px`-min-height rows (`dlRowPadX: 10px`, `dlColGap: 12px`, `84px` size, `150px` progress + `%`, `80px` speed, `70px` ETA, `88px` hover-revealed actions) with optional `rail` site group dividers.

## Do's and Don'ts

### Do:
- **Do** derive every surface, border, text, and semantic tone from `workbenchTokens(dark)` and shared helpers (`uiCardSurface`, `uiBadge`, `uiBtn`, `uiMiniBtn`, `uiIconBtn`, `uiPill`) in `internal/ui/design.go` so Light and Dark modes stay synchronized.
- **Do** apply `FontFeatures("tnum")` (and `font-variant-numeric: tabular-nums` in HTML) on all task counters, quota meters, elapsed timers, row indices, percentages, download speeds, and ETAs.
- **Do** enforce `>= 4.5:1` contrast across all semantic badges and primary CTAs using `semanticToneColors` in Light mode and `on-primary-dark` (`#090D16`) ink on `#60A5FA` primary buttons in Dark mode.
- **Do** honor `c.Preferences().ReduceMotion` and `@media (prefers-reduced-motion: reduce)` by omitting positional/scale transitions and halting continuous pulse animations (`waitingDot`, `paintGenBar`, `gapRing`, indeterminate `motionProgress`).
- **Do** keep all browser login, verification, and page-bridge workflows inside built-in `Camoufox` environments (`browserDrawerView`) and `extbridge` extension surfaces.
- **Do** preserve compact shell dimensions (`topbarH: 34px`, `sidebarExpandedW: 134px`, `sidebarCollapsedW: 42px` below `840px`, `composerW: 248px` / `272px` at `>= 1200px`) so live account lanes and output grids dominate `1024×700`+ viewports.

### Don't:
- **Don't** introduce per-page legacy palettes, Material-default theme overrides, or Xunlei-style sub-page colors outside the unified cool-slate `workbenchTokens` system.
- **Don't** add decorative color gradients, glassmorphism blur backdrops, or vanity summary metric cards that steal vertical operational real estate.
- **Don't** render low-contrast grey-on-grey labels, unbordered status pills, or `#FFFFFF` white text over Dark-mode `#60A5FA` primary buttons.
- **Don't** put top-level `motion`, `shadows`, or `breakpoints` keys in `DESIGN.md` YAML frontmatter (keep them in `.impeccable/design.json` `extensions`), and don't animate list row heights during enter/exit transitions.
- **Don't** launch, control, or design UI flows around external browsers (`Chrome`, `Edge`, `Chromium`) outside the built-in `Camoufox` runtime and `extbridge` extension UI.
