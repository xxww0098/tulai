<!-- impeccable:product-schema 1 -->
# tulai — Product Foundation

Platform: adaptive

## Platform

adaptive

## Product Overview

`tulai` is a cross-platform desktop multi-provider, multi-account AI batch image SaaS workbench built with Go and [MyGo](https://mygo.run) v0.10.0 (React 19 + Vite 6 + Tailwind CSS v4 + TypeScript 5, managed with `Bun`) for Windows, macOS, and Linux, with full Light and Dark theme parity.

The desktop runtime connects the React frontend (`frontend/`) to 9 Go domain services (`Core`, `Workbench`, `Browser`, `Prompts`, `Download`, `Dedup`, `Rename`, `ImageEdit`, `Settings` via generated bindings in `frontend/src/mygo.ts`) serialized through a single-goroutine Controller Actor (`internal/ui/actor.go`), coalesced topic push events (`tulai:topics`), native file-drop forwarding (`tulai:file-drop`), and the `tulaimedia` custom local media protocol (`internal/ui/media_handler.go`).

All account credentials, app-managed browser profiles, prompt templates, the current workspace, and generated outputs stay local to the user's machine while connecting to remote AI providers and target webpages:

- **AI Providers:** `豆包` and `千问`.
- **Managed Browser Environment (see [`AGENTS.md`](AGENTS.md)):**
  - **Fingerprint (`fingerprint-chromium`):** Dedicated app-managed fingerprint browser (`runtime/fingerprint/`, isolated per-account profiles in `BrowserData/Fingerprint/` with `.tulai-fingerprint.json`, per-account proxies with WebRTC non-proxied UDP protection) driven over a private CDP pipe and isolated-world page bridge — never connecting to system browsers or external debug ports.
  - **In-App `BrowserDrawer`:** Floating draggable/resizable live viewport (`BrowserStage`) for login takeover and manual CAPTCHA verification directly inside the app.
  - **Web Download Browser:** Dedicated managed `Fingerprint` profile (`DownloadBrowser`) with an isolated-world download helper for dynamic pages.
- **9 Views Across the Workbench & Tool Suite (`frontend/src/pages/`):**
  1. **`图片生成` (`WorkbenchPage.tsx`):** Three modes in exact tab order — `批量洗图` (batch image washing with style presets, per-image resume, WashBot「小白洗图君」station cards, Shift+click multi-select & batch secondary rewash + undo), `自由生图` (`文生图` & `图生图` with independent drafts, repeat counts, `.txt` import, unified vs per-line pairing, templates, global suffix, native ratio controls, and structured prompt builder), and `裂变图片` (1-to-4 compositional fission with 63-bit pHash similarity check). Includes Lightbox with `100%–400%` zoom/pan and draggable original-vs-output comparison curtain.
  2. **`浏览器` (`BrowserPage.tsx`):** Managed `Fingerprint` runtime status & byte-level installer, internal bridge token diagnostics, quick account creation, and virtualized account list with inline rename, per-account proxy editor, and batch launch/stop.
  3. **`提示词` (`PromptsPage.tsx`):** Reusable prompt template library filtered by `全部` / `生图` / `洗图` / `裂变` (unclassified labeled `通用`), global prompt suffix toggle, and one-click apply to Workbench.
  4. **`下载` (`DownloadPage.tsx`):** Webpage, URL-list, and CSV/XLSX batch image downloader with depth/size/dimension filters, searchable site guide (`站点说明`), and managed Fingerprint dynamic page capture (`浏览器抓取`).
  5. **`去重` (`DedupPage.tsx`):** Three-level (`严格` / `均衡 (推荐)` / `宽松`) perceptual-hash visual similarity grouping that copies the highest-resolution original per group, with flat-color toggle and CSV report.
  6. **`批量重命名` (`RenamePage.tsx`):** Five modes (`查找替换`, `插入文本`, `序号重排`, `逐行编辑`, `AI 命名`), live conflict preview, drag-and-drop import, multi-line paste, and undo.
  7. **`图片编辑` (`ImageEditPage.tsx`):** Three-column darkroom with collapsible multi-select filmstrip, interactive canvas (`ImageEditStage`: 8-handle aspect-aware crop, 60fps CSS filter live preview, draggable `5%–95%` before/after split curtain, cursor-anchored wheel zoom & pan), inspector cards (`裁剪、尺寸与旋转`, `色彩与清晰度`, `输出格式与目录`), reusable recipes (`配方`), and batch export with failed-item retry.
  8. **`设置` (`SettingsPage.tsx`):** Four tabs — `常规与输出` (Light/Dark theme, output directory, auto-numbering, download defaults), `账号与任务` (concurrency, watermark removal, visible task browser, dispatch strategy, retries/intervals/timeouts, risky words, per-provider daily caps), `网络与高级` (global proxy, CAPTCHA solver backends `free` / `custom` / `ikunocr`), and `关于与更新` (GitHub Releases updater & mirror selection).
  9. **首次设置向导 (`WizardView.tsx`):** First-run modal guiding provider selection, managed browser engine installation, and initial account login.
- **Global Shell Chrome:** Collapsible navigation rail (`⌘/Ctrl+\`), quick page jumps (`⌘/Ctrl+1..8`, `⌘/Ctrl+,`), floating dark console `LogDrawer` (`⌘/Ctrl+J`), hover preview portal (`HoverPreviewHost`), prioritized `Esc` dismissal stack, and native OS file/folder drag-and-drop.

## Users

- **E-commerce Operations Teams:** Running high-volume product image washing, SKU multi-angle fission, batch darkroom crop/resize/tone adjustments, and structured renaming at scale.
- **Content Studios & Media Operators:** Acquiring source assets via batch web downloads, deduplicating image collections, and running repeatable prompt pipelines.
- **Design & Production Teams:** Operating shared multi-account pools across `豆包` and `千问` while handling daily quotas, rate-limit cooldowns, and CAPTCHA/login gates without interrupting batch throughput.

## Creative North Star (Operate Mode)

**The Modular Bento Dispatch Console** — a dense, high-trust desktop operational workbench where the chrome disappears behind live jobs, account-pool telemetry, and image results (detailed in [`DESIGN.md`](DESIGN.md)):

- **Cool-Slate Tonal Layering:** Crisp surface hierarchy (`paper` canvas, `rail` navigation, `card` Bento panels, `sunken` / `sunken-hover` recessed rows and controls) without visual noise.
- **Hairline 1px Borders:** Structural separation via quiet 1px borders (`hairline` / `divider` / `strong`) rather than heavy drop shadows or nested decorative boxes.
- **Tabular Numerals (`.tnum`):** Fixed-width numeric alignment across task counters, elapsed timers, daily quota meters, download speeds, and progress percentages.
- **Explicit Account-Pool State Visibility:** Unambiguous operational status at a glance (`空闲`, `出图中`, `等待间隔`, `冷却中`, `验证码`, `需要登录`, `今日已满`, `额度不足`, automatic failover/rotation) with >= 4.5:1 contrast across Light and Dark modes.
- **Compact Viewport Discipline:** Maximize live stage and results real estate down to `900×620` minimum (`1160×740` default), with responsive auto-collapsing rail (`< 960px`), Settings toolbar mode (`< 1120px`), compact darkroom filmstrip (`< 1040px`), and full Light/Dark parity.

## Anti-References

Future UI work in `tulai` must explicitly avoid:

- **Fragmented Material or Legacy Sub-Page Palettes:** Ad-hoc per-page colors or skins that drift away from the unified cool-slate workbench token system.
- **Decorative Marketing Surfaces:** Unnecessary color gradients, glassmorphism blur backdrops, or vanity hero-metric summary cards that waste operational space.
- **Low-Contrast Grey-on-Grey Chrome:** Muted labels, subtle badges, or unbordered status pills that dip below 4.5:1 readability in Light or Dark mode.
- **Bloated Modal-First Workflows:** Oversized paddings, redundant kickers, or blocking dialogs where inline stage controls or floating drawers keep the operator in flow.

## Durable Product Principles

1. **Commit-Boundary Safety:** Never silently re-dispatch an unconfirmed submission; surface explicit verification states (`需核对`, `待验证`) and preserve local drafts and outputs.
2. **Pool Resilience Over Single-Account Fragility:** Surface per-account health, cooldown timers, and remaining quota clearly so multi-account rotation is observable and predictable.
3. **Built-In Browser Sovereignty:** All browser automation, fingerprinting, login takeover, and page-bridge operations use project-managed `Fingerprint` (`fingerprint-chromium`, private CDP pipe) with isolated profiles — never external/system browsers or external debug ports.
4. **Unified Design Tokens:** Every workbench mode and utility tool shares one token set declared in [`frontend/src/styles/tokens.css`](frontend/src/styles/tokens.css) and [`frontend/src/styles/global.css`](frontend/src/styles/global.css), consumed via Tailwind CSS v4 across `frontend/src/`, for consistent density, typography, semantic contrast, and Light/Dark parity.
