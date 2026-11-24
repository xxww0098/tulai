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

`tulai` is built as a dense, high-trust desktop operational workbench where window chrome steps back behind live generation queues, multi-account health telemetry, and image output grids. Every surface across the Go/NSGo (TSX + Tailwind) desktop application and its companion WebExtension page-bridge views operates under unified Light and Dark parity, pairing cool-slate tonal layering with crisp hairline borders so operators can track concurrent jobs across `豆包`, `千问`, `Antigravity`, and `ChatGPT` without visual fatigue.

In Operate mode, density and legibility are deliberate engineering constraints rather than compromises. Fixed-height global bars, compact four-step typography, tabular numerals (`tnum`), and unambiguous semantic states (`空闲`, `出图中`, `等待间隔`, `需要人工验证`, `需登录`, `额度用尽`) keep multi-account pools observable at a glance even at `1024×700`. The system explicitly rejects fragmented Material or Xunlei-style per-page palettes, decorative gradients or glassmorphism blur backdrops, low-contrast grey-on-grey labels, and bloated modal-first workflows that interrupt batch throughput.

**Key Characteristics:**
- Full-height obsidian-slate navigation shell (`#111622` Light / `#090C12` Dark, `116px` expanded / `44px` collapsed) paired with crisp edge-to-edge workspace canvases (`#FFFFFF` Light / `#131924` Dark) divided by single `1px` structural hairlines (`border` `#E5E9F0` / `#232E40`).
- Flat-at-rest panes with zero nested boxed cards, zero rest-state box shadows, and `lift` elevation reserved strictly for active overlays, tooltips, popovers, drawers, and modals.
- Crisp OS native typography (`15px` / `13px` / `12px` / `11px`, `system-ui`) with mandatory `tabular-nums` alignment across counters, quotas, speeds, and timers.
- High-contrast borderless semantic status pills (`>= 4.5:1` contrast in Light mode and `#090D16` `on-primary-dark` ink on `#60A5FA` primary CTAs in Dark mode).
- GPU-driven micro-interactions (`150ms` fast transitions, `220ms` UI travel) with explicit reduced-motion static fallbacks.

## Implementation Status

The desktop UI is TSX + Tailwind on NSGo under `cmd/tulai/ui`; Go draws only the native views registered in `internal/ui/native_views.go` (`BrandLogo`, `BrowserStage`, `ImageEditStage`, `OutlineBadge`). The rules in this document are the intended design. As of 0.1.37 the build matches them on palette and shell, and differs as follows:

- **Theme tokens do not reach `nsgo/ui` at runtime.** NSGo's compiler reads `nsgo.yaml`, and the `theme.yaml` it names, beside the TSX entry. tulai's entry is `cmd/tulai/ui/App.tsx` and `nsgo.yaml` sits one level up, in `cmd/tulai/`, so `cmd/tulai/theme.yaml` is skipped without an error. Hand-written Tailwind values are right, but `nsgo/ui` components (`Button`, `Badge`, `Input`, `Dialog`, …) fall back to NSGo's default theme: accent `#007AFF` / `#0A84FF`, text `#333333` / `#EAEAEA`, dark surface `#292929`, radius `8px`.
- **Shell measurements.** The rail is `164px` expanded / `52px` folded and folds by hand only; there is no width breakpoint. The topbar is `40px` and holds the page title, a running-task badge, the account-pool summary and the log drawer toggle. The workbench's mode and provider tabs sit in that page's own toolbar.
- **Typography.** Hand-written text uses `10px`, `11px`, `12px`, `13px` and `14px`, with `12px` for most copy. Nothing applies `tabular-nums`.
- **Shapes.** Radii in use are `4px`, `8px` and `12px` plus fully rounded pills. `SectionCard` is a bordered `12px` box, and bordered controls sit inside it, so the zero-nesting rule and the `5px` / `6px` / `8px` scale are not met.
- **Badges.** `Badge` comes from `nsgo/ui`: a bordered capsule, tinted by NSGo's own wash, in the variants `accent`, `success`, `warn`, `danger` and `neutral`. `toneVariant` in `cmd/tulai/ui/components/primitives.tsx` maps app tones onto them. NSGo 0.3.0 removed its `info` variant, so in-progress states now use `accent`. The outline tag is the Go `OutlineBadge`.
- **Motion and elevation.** There are no transitions. The only shadows are the browser drawer and the log drawer.

Where a rule above and this list disagree, the rule is the design and the list is what ships; change one or the other deliberately. The frontmatter keeps the rule's values.

## Colors

A disciplined dual-theme obsidian-rail and pure-white-stage palette anchored by a single cobalt primary accent and high-contrast semantic status tones shared between the desktop surfaces (`cmd/tulai/theme.yaml` and the matching Tailwind values in `cmd/tulai/ui`) and WebExtension pages (`extension/popup.html`, `extension/options.html`). The names in parentheses below are the `theme.yaml` keys.

### Primary
- **Dispatch Cobalt Light** (`colors.primary-blue-light`, `#2563EB`): Primary execution buttons (`开始出图`, `保存并重连`, `新建任务`), focused input strokes, and running progress fills in Light mode.
- **Dispatch Cobalt Hover Light** (`colors.primary-blue-hover-light`, `#1D4ED8`): Hover state fill for Light mode primary action buttons.
- **Dispatch Cobalt Pressed Light** (`colors.primary-blue-pressed-light`, `#1E40AF`): Active pressed state fill for Light mode primary action buttons.
- **On-Primary Ink Light** (`colors.on-primary-light`, `#FFFFFF`): High-contrast text and icon foreground placed over Light mode primary cobalt surfaces.
- **Electric Sky Dark** (`colors.primary-blue-dark`, `#60A5FA`): Primary accent in Dark mode for primary CTAs, focus borders, active rail indicator (`rail-accent-dark`), and live progress bars.
- **Electric Sky Hover Dark** (`colors.primary-blue-hover-dark`, `#93C5FD`): Hover state fill for Dark mode primary action buttons.
- **Electric Sky Pressed Dark** (`colors.primary-blue-pressed-dark`, `#3B82F6`): Pressed state fill for Dark mode primary action buttons (and Dark extension `--accent-hover`).
- **Obsidian On-Primary Dark** (`colors.on-primary-dark`, `#090D16`): High-contrast dark ink on `#60A5FA` primary buttons in Dark mode to satisfy `>= 4.5:1` legibility.

### Navigation Shell & Neutral Surfaces
- **Obsidian Rail Light** (`colors.rail-light`, `#111622`) / **Deepest Slate Rail Dark** (`colors.rail-dark`, `#090C12`): Persistent full-height left navigation shell (`rail`) contrasting cleanly against the light or dark operational canvas.
- **Rail Interaction Tokens:** Hover `#1B2333` (`rail-hover-light`) / `#131926` (`rail-hover-dark`), active `#253047` (`rail-active-light`) / `#1C2538` (`rail-active-dark`), divider border `#222B3C` / `#1A2232`, bright ink `#F8FAFC`, muted rail ink `#94A3B8` / `#8B9BB4`, and sky indicator accent `#60A5FA` (`rail-accent-*`).
- **Cool Paper Light** (`colors.cool-paper-light`, `#F8FAFC`) / **Obsidian Canvas Dark** (`colors.cool-paper-dark`, `#090C12` shell; `#0D1118` `paper` in `theme.yaml`): Base canvas and recessed group headers.
- **Pure Stage White** (`colors.surface-card-light`, `#FFFFFF`) / **Carbon Slate Card Dark** (`colors.surface-card-dark`, `#131924`): Primary workspace surface (`card`), the global topbar, prompt editor canvas, and active segmented tabs.
- **Mist Slate Light** (`colors.sunken-track-light`, `#F1F5F9`) / **Deep Recessed Slate Dark** (`colors.sunken-track-dark`, `#1A2230`; `#1A2232` in `theme.yaml`): Recessed tracks (`sunken`), resting secondary controls, filter tabs, and column headers.
- **Steel Hover Light** (`colors.sunken-hover-light`, `#E2E8F0`) / **Elevated Slate Hover Dark** (`colors.sunken-hover-dark`, `#242E42`; `#232D42` in `theme.yaml`): Hover and active track surfaces (`sunken-hi`).
- **Hairline Mist Light** (`colors.border-hairline-light`, `#E5E9F0`) / **Abyssal Hairline Dark** (`colors.border-hairline-dark`, `#232E40`; `#222B3C` in `theme.yaml`): Standard `1px` structural hairlines (`border`) dividing full-bleed split panes, topbar, settings sub-navbar, inputs, and table rows.
- **Slate Hairline Strong Light** (`colors.border-strong-light`, `#CBD5E1`) / **Steel Hairline Strong Dark** (`colors.border-strong-dark`, `#334155`): Emphasized `1px` borders (`border-strong`) on hovered inputs, active tabs, expanded feature toggles, popovers, dashed dropzones, and unchecked switches.
- **Ink Slate Light** (`colors.ink-slate-light`, `#0F172A`) / **Frost White Dark** (`colors.ink-slate-dark`, `#F1F5F9`): Primary body and headline text (`text`), plus inverted tooltip surfaces.
- **Graphite Muted Light** (`colors.muted-slate-light`, `#526075`) / **Cool Pewter Dark** (`colors.muted-slate-dark`, `#94A3B8`): Secondary labels (`muted`), calm metadata hints (`本次发送至：...`), table headings, and neutral badges.
- **Silver Subtle Light** (`colors.subtle-slate-light`, `#8492A6`) / **Slate Subtle Dark** (`colors.subtle-slate-dark`, `#64748B`): Tertiary table captions, row index numerals, and zero-count indicators.

### Semantic
- **Signal Emerald Light** (`colors.signal-emerald-light`, `#15803D`) / **Mint Signal Dark** (`colors.signal-emerald-dark`, `#34D399`): Completed generation/download states (`完成`), healthy multi-account pool indicator, running browser instances (`运行中`), and verified batch notices.
- **Amber Alert Light** (`colors.amber-alert-light`, `#B45309`) / **Warm Amber Dark** (`colors.amber-alert-dark`, `#FBBF24`): Cooldown interval countdowns (`等待间隔`), manual verification/review gates (`需要人工验证`, `待验证`, `需核对`), partial pool readiness, and `>= 80%` quota threshold warning bars.
- **Crimson Fault Light** (`colors.crimson-fault-light`, `#DC2626`) / **Coral Alert Dark** (`colors.crimson-fault-dark`, `#F87171`): Execution failures (`失败`), expired/missing login (`需登录`, `未登录`), quota exhaustion (`额度用尽`), rename conflicts (`冲突`), and destructive stop/delete actions.
- **Semantic Wash Derivation:** Borderless desktop badges and lane chips use a tinted wash of their base color (`11%` wash Light / `16%` Dark, `10%` / `14%` neutral, and `25%` foreground mix toward `text` in Light mode).

### Named Rules
**The Obsidian-Chrome, Paper-Stage Rule.** Keep the full-height `navRail` anchored in deep obsidian slate (`#111622` Light / `#090C12` Dark) so persistent navigation stays calm and visually separated from the pure-white (`#FFFFFF`) or carbon-slate (`#131924`) operational workspace.

**The Semantic Contrast Floor Rule.** Every borderless badge, lane chip, and alert bar maintains `>= 4.5:1` foreground contrast by mixing Light-mode semantic foregrounds `25%` toward `ink-slate-light` and switching Dark-mode primary button ink to `on-primary-dark` (`#090D16`).

**The Cool-Slate Parity Rule.** Desktop views and `extension/*.html` WebExtension popups/options share synchronized Light and Dark slate/cobalt/emerald/amber/crimson tokens with zero per-page legacy palettes.

## Typography

**Display & Body Font:** Native OS UI font (`system-ui` on desktop; `system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif` and `Inter` fallback in `extension/*.html`)
**Label/Mono Font:** `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace` (for `#token` and `#port` fields in `extension/*.html`; numeric telemetry columns in desktop views use `tabular-nums` on the system UI font)

**Character:** Legible, compact operational density driven by a clean 4-step scale (`15px` / `13px` / `12px` / `11px`), crisp weight contrast (`400`–`600`), and tabular numeric alignment.

### Hierarchy
- **Display** (`600`, `15px` desktop; `16.5px`–`20px` in `extension/*.html`): Image-edit header, modal titles, empty-state headlines, wizard step headers, and floating log console title.
- **Title** (`600`, `13px`): Section headers, workbench composer card headings (`提示词`, `参考图`), tool aside headers, and browser drawer headers.
- **Body** (`400`–`500`, `12px`; `13px` in `extension/*.html`): Default body size, navigation rail labels, button labels, form controls, and tabular table cells (`tnum`).
- **Caption** (`500`–`600`, `11px`): Borderless badge and filter-pill chips, table column headers, quota counters, elapsed timers, download ETAs, and calm metadata hints.

### Named Rules
**The Tabular Telemetry Rule.** Apply `tabular-nums` (in HTML, `font-variant-numeric: tabular-nums`) to every counter, quota meter, elapsed timer, row index, percentage, download speed, and ETA so live updates never jitter horizontally.

**The Native Four-Step Scale Rule.** Desktop workbench copy uses the four-step `11px`–`15px` native UI ramp (`display: 15px`, `title: 13px`, `body: 12px`, `caption: 11px`); larger `16.5px`–`20px` headings appear only inside standalone WebExtension `popup.html` and `options.html` surfaces.

## Layout

`tulai` uses a two-column global application shell in `cmd/tulai/ui/App.tsx`: a full-height navigation rail beside a column of `topbar` and the current page, keeping prompts, live multi-account queues, and image grids dominant at `1024×700`+.

- **Navigation Rail (`navRail`):** Full-height left rail (`#111622` Light / `#090C12` Dark, `1px` right hairline) at `164px` expanded, folded by its collapse button to a `52px` icon-only rail. A brand header (`tulai` logo, wordmark, version, and a ghost collapse button) sits above two groups of navigation items (icon + `12px` medium label): `图片生成`, `浏览器`, `提示词`, then below a hairline `下载`, `去重`, `批量重命名`, `图片编辑`. `设置` and the update notice sit at the foot.
- **Global Topbar (`topbar`):** `40px`, `card` background, `1px` bottom hairline, holding the page title, a running-task badge, the account-pool summary pill, and the log drawer toggle.
- **Edge-to-Edge Split Panes:** Each page (under `cmd/tulai/ui/pages/`) is a `PageToolbar` (actions left and right, an optional status row below) over panes that become columns from `768px` up:
  - **Workbench (`WorkbenchPage.tsx`):** Mode tabs (`批量洗图`, `自由生图`, `裂变图片`) and provider tabs in the toolbar beside `历史记录` and the start/stop button; a `344px` composer (`376px` from `1280px`) beside the account lanes and results grid. In wash mode the prompt starts as a short summary with an explicit `编辑提示词` / `收起提示词` toggle; the expanded editor is `176px` high and scrolls internally, so long templates do not displace the reference images. Source thumbnails use contain fitting to show frames and collages in full. Free generation and fission retain their always-visible editors. Completed results can be explicitly expanded or collapsed, with the button label matching visibility.
  - **`图片编辑` (`ImageEditPage.tsx`):** A `180px` filmstrip, the full-width preview canvas (`ImageEditStage`), and a `276px` collapsible inspector.
  - **`提示词` (`PromptsPage.tsx`):** A `272px` template list beside the prompt editor.
  - **`批量重命名` (`RenamePage.tsx`):** A `320px` rule column beside the rename preview.
  - **`去重` (`DedupPage.tsx`) & `下载` (`DownloadPage.tsx`):** A `288px` image pool / `324px` link composer beside the similarity report / download queue.
  - **`浏览器` (`BrowserPage.tsx`):** One toolbar with the account-environment tabs, a readiness badge and the install button, and the extension-bridge address with its token actions, above an add-account bar and the account list.
  - **`设置` (`SettingsPage.tsx`):** Segmented tabs (`常规与输出`, `账号与任务`, `网络与高级`, `关于与更新`) over centered `780px` content.
  - **`扩展` (`extension/popup.html`, `extension/options.html`):** Fixed `356px` popup (`14px` padding) and `520px` max-width options wrap.

### Named Rules
**The Full-Height Rail, Flush-Pane Rule.** Anchor `navRail` (`116px` / `44px`) edge-to-edge vertically (`0..winH`) aligned with the `34px` topbar, and separate in-page panes with single `1px` structural hairlines rather than inset grey gutters or nested boxes so prompts and generated images command maximum viewport area.

## Elevation & Depth

Surfaces establish hierarchy strictly through obsidian rail vs pure-white canvas contrast (`rail` vs `card`/`paper`) and crisp `1px` hairline borders (`border` / `border-strong`). Rest-state cards, split panes, toolbars, extension cards (`--shadow: none`), and tables remain completely shadow-free; `shadow` (`rgba(15, 23, 42, 0.05)` Light / `rgba(0, 0, 0, 0.36)` Dark) and `lift` (`rgba(15, 23, 42, 0.10)` Light / `rgba(0, 0, 0, 0.46)` Dark) are reserved exclusively for floating chrome.

### Floating Elevation Layers
- **Tooltip & Popover Menus:** Subtle floating separation on inverse tooltips and dropdown popups.
- **Search Popovers & Update Banners:** Elevated over workbench content with hairline border + `lift` drop shadow.
- **Modal Dialogs & Floating Browser Drawer:** Centered tool modals (`Dialog`) and the floating account-browser drawer (`BrowserDrawer`) use backdrop scrim + `lift` separation.
- **Floating Log Console (`LogDrawer`):** Draggable dark runtime console anchored cleanly above Light or Dark workspaces.

### Named Rules
**The Shadow-Free Rest State Rule.** Zero box shadows on rest cards, tables, panels, or extension UI (`--shadow: none`); use single `1px` structural hairlines at rest and reserve `lift` shadows strictly for floating tooltips, popovers, drawers, modals, and the log console.

**The Reduced-Motion Fidelity Rule.** GPU transitions run at `150ms` (`motionFast`) for micro-interactions (`scale(1.012)` hover, `scale(0.985)` press, floating labels, color anims) and `220ms` (`motionUI`) for `12px` travel/reveal transitions without list-height reflow, degrading cleanly to static or opacity-only states when reduced motion is requested.

## Shapes

The system enforces crisp geometric corners (`5px` / `6px` / `8px` / `999px`) alongside flush `0`-radius structural split panes (image-edit sections, edge-to-edge split dividers) and `1px` solid hairlines (`border`, upgrading to `border-strong` on hover/open and `accent` on focus):

- **Small Controls (`rounded.sm`, `5px`):** Standard, mini and icon buttons, nav items, and text/number inputs.
- **Containers & Prompt Canvases (`rounded.md`, `6px`):** Cards, prompt canvases, lane and account tables, and extension panels.
- **Overlays & Consoles (`rounded.lg`, `8px`):** Modal dialogs, the floating log console, and empty-state icon wells.
- **Status Pills (`rounded.pill`, `999px`):** Borderless status badges, lane chips, and filter pills.
- **Flush Sections & Functional Radii:** Flush split-pane sections use `0` radius with `1px` side/bottom hairlines (`border`), `5px` quota progress tracks use soft pill rounding, and empty dropzones use `1px` dashed `border-strong` strokes turning `1px`–`2px` solid `accent` on hover or file drag-over.

### Named Rules
**The Zero-Nesting Corner Rule.** Use `0` radius on flush hairline split panes, `5px` on interactive controls, `6px` on standalone cards and prompt editors, `8px` on floating overlays, and `999px` on borderless pills—never nest rounded bordered boxes inside other rounded bordered boxes.

## Components

Every workbench control and utility page composes from the same pieces, with synchronized hover, focus, active, and disabled feedback: the `nsgo/ui` components (`Button`, `IconButton`, `Badge`, `Input`, `Select`, `Switch`, `Checkbox`, `Tabs`, `Progress`, `Dialog`, `Slider`), the shared TSX parts in `cmd/tulai/ui/components/primitives.tsx` (`PageToolbar`, `FormRow`, `SectionCard`, `PillToggle`, `EmptyNotice`, `FieldLabel`, `StatusBadge`), and the Go-drawn views registered in `internal/ui/native_views.go`.

### Buttons
Crisp `24px`–`28px` triggers with `5px` corners (`rounded.sm`) and single-node labeling for clean accessible hit targets and testability.
- **Primary:** One primary button using `primary-blue-light` (`#2563EB`) + `#FFFFFF` ink in Light mode and `primary-blue-dark` (`#60A5FA`) + `on-primary-dark` (`#090D16`) ink in Dark mode.
- **Secondary / Ghost / Mini / Icon:** Secondary buttons (`24px` height, `sunken` fill with `1px` `border`), lighter ghost and icon buttons (transparent at rest, used in the rail's brand header and compact action rows), and mini buttons.

### Chips & Borderless Semantic Badges
Borderless badge and filter-pill chips (`0`px border at rest, `999px` pill radius, `>= 4.5:1` contrast via the semantic wash, `11px` `tnum` text) delivering clean telemetry without perimeter noise:
- **Tones:** `accent` (`11%` wash Light / `16%` Dark), `success`, `warning`, `danger`, and `neutral` (`10%` wash Light / `14%` Dark).

### Prompt Canvases, Cards & Flush Sections
Clean prompt editors in pure white (`#FFFFFF` / `#131924`) free of sunken grey wells, flat cards (`Padding(8, 10)`, `Gap(6)`), and flush `0`-radius split sections separated by `1px` `border` hairlines (image edit, prompts, rename, settings).

### Inputs / Fields
Crisp inputs (`28px` standard height, `34px` floating-label rename fields, `sunken` resting fill stepping to `card` background with `accent` border on focus) and inline `1px` error telemetry.

### Navigation
Full-height (`0..winH`) obsidian-slate navigation rail (`116px` expanded / `44px` collapsed below `840px`) aligned via its `34px` brand header (`tulai` brand + a ghost collapse button) flush alongside the `34px` topbar, removing topbar overhang and redundant toggles.

### Multi-Tenant Lane & Browser Tables
Purpose-built operational data tables packing account toggles, borderless status pills, `5px` quota progress meters (`tnum`), cooldown timers, and browser-environment actions into crisp hairline-divided rows (account lanes, the browser account list, the download queue), plus one consolidated browser toolbar.

## Do's and Don'ts

### Do:
- **Do** derive every surface, rail tone, border, text, and semantic tone from the tokens in `cmd/tulai/theme.yaml` — written as the same Tailwind values across `cmd/tulai/ui` — and the shared parts in `cmd/tulai/ui/components/primitives.tsx`.
- **Do** apply `tabular-nums` (and `font-variant-numeric: tabular-nums` in HTML) on all counters, quota meters, elapsed timers, row indices, percentages, download speeds, and ETAs.
- **Do** enforce `>= 4.5:1` contrast across all borderless semantic badges and primary CTAs using the semantic wash in Light mode and `on-primary-dark` (`#090D16`) ink on `#60A5FA` primary buttons in Dark mode.
- **Do** honor reduced-motion settings and `@media (prefers-reduced-motion: reduce)` cleanly.
- **Do** keep all browser login, verification, and page-bridge workflows inside the built-in `Camoufox` and Chrome CDP environments (`BrowserDrawer`) and `extbridge` extension surfaces.
- **Do** preserve the full-height obsidian shell (`navRail` `116px` / `44px` spanning `0..winH`, `34px` brand header & `topbar`) and flush hairline split panes so prompt editors and output images command viewport space.

### Don't:
- **Don't** nest bordered cards inside bordered cards, draw duplicate page headers, or add rest-state box shadows (`--shadow: none`) on panes or extension views.
- **Don't** put the rail's collapse button in `topbar` or wrap `topbar` across `navRail` with overhang; keep the collapse button aligned inside the `34px` brand-header column.
- **Don't** duplicate button label text nodes—a button carries its label once.
- **Don't** render low-contrast grey-on-grey labels, `#FFFFFF` text on Dark `#60A5FA` primary CTAs, or thick `>1px` decorative accent bars.
- **Don't** put top-level `motion`, `shadows`, or `breakpoints` keys in `DESIGN.md` YAML frontmatter (keep them in `.impeccable/design.json` `extensions`), and don't animate list row heights during enter/exit transitions.
- **Don't** launch, control, or design UI flows around external browsers (`Chrome`, `Edge`, `Chromium`) outside the built-in `Camoufox` and Chrome CDP runtimes and `extbridge` extension UI.
