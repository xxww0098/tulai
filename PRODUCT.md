<!-- impeccable:product-schema 1 -->
# tulai — Product Foundation

Platform: adaptive

## Platform

adaptive

## Product Overview

`tulai` is a cross-platform desktop multi-provider, multi-account AI batch image SaaS workbench built with Go and [NSGo](https://nsgo.dev) (TSX + Tailwind) for Windows, macOS, and Linux, with full Light and Dark theme parity.

All account credentials, Camoufox browser profiles, prompt templates, task history, and generated outputs stay local to the user's machine while connecting to remote AI providers and target webpages:

- **AI Providers:** `豆包`, `千问`, `Antigravity`, and `ChatGPT`.
- **Browser & Bridge Environments:** Built-in app-managed `Camoufox` fingerprint browser instances (isolated per-account profiles, per-account proxies, and an in-app browser drawer for login and manual verification), Chrome/Edge page bridge (`extbridge`) support, and `agy` CLI authentication/account import for `Antigravity`.
- **Batch Image Workbench (`图片生成`):**
  - `自由生图`: prompt-based text-to-image and reference-image generation with prompt templates, parameter composition, task queue, and session history.
  - `批量洗图`: high-throughput batch image washing with per-image progress tracking and style/prompt controls.
  - `裂变图片`: 1-to-4 compositional fission from a source image with pHash similarity verification.
- **Tool Suite:**
  - `去重`: perceptual-hash visual similarity grouping that preserves highest-resolution originals.
  - `提示词`: reusable prompt library across generation, washing, and general workflows.
  - `下载`: batch webpage, URL-list, and CSV/XLSX image downloader with dynamic page capture, filtering, and retries.
  - `批量重命名`: rule-based replacement, numbering, sorting, AI-assisted naming suggestions, and undo.
  - `图片编辑`: batch crop, resize, format conversion, compression, and watermarking.
  - `浏览器` & `设置`: multi-account pool management, concurrency/quota rules, output directory naming, watermark cleanup, captcha solving, proxy configuration, and in-app updates.

## Users

- **E-commerce Operations Teams:** Running high-volume product image washing, SKU multi-angle fission, batch resizing, and structured renaming at scale.
- **Content Studios & Media Operators:** Acquiring source assets via batch web downloads, deduplicating image collections, and running repeatable prompt pipelines.
- **Design & Production Teams:** Operating shared multi-account pools across `豆包`, `千问`, `ChatGPT`, and `Antigravity` while handling daily quotas, rate-limit cooldowns, and CAPTCHA/login gates without interrupting batch throughput.

## Creative North Star (Operate Mode)

**The Multi-Tenant Dispatch Console** — a dense, high-trust desktop operational workbench where the chrome disappears behind live jobs, account-pool telemetry, and image results:

- **Cool-Slate Tonal Layering:** Crisp surface hierarchy (`paper` canvas, `rail` navigation, `card` panels, `sunken` / `sunkenHi` recessed tracks and controls) without visual noise.
- **Hairline 1px Borders:** Structural separation via quiet 1px borders (`border` / `borderStrong`) rather than heavy drop shadows or nested decorative boxes.
- **Tabular Numerals (`tnum`):** Fixed-width numeric alignment across task counters, elapsed timers, daily quota meters, download speeds, and progress percentages.
- **Explicit Account-Pool State Visibility:** Unambiguous operational status at a glance (`空闲`, `出图中`, `等待间隔`, `需要人工验证`, `需登录`, `额度用尽`, automatic failover/rotation) with >= 4.5:1 contrast across Light and Dark modes.
- **Compact Viewport Discipline:** Maximize live stage and results real estate even at compact desktop widths (e.g., `1024x700`), with Light and Dark parity across every workspace and tool page.

## Anti-References

Future UI work in `tulai` must explicitly avoid:

- **Fragmented Material or Xunlei-Style Sub-Page Palettes:** Ad-hoc per-page colors or legacy skins that drift away from the unified cool-slate workbench token system.
- **Decorative Marketing Surfaces:** Unnecessary color gradients, glassmorphism blur backdrops, or vanity hero-metric summary cards that waste operational space.
- **Low-Contrast Grey-on-Grey Chrome:** Muted labels, subtle badges, or unbordered status pills that dip below 4.5:1 readability in Light or Dark mode.
- **Bloated Modal-First Workflows:** Oversized paddings, redundant kickers, or blocking dialogs where inline stage controls or drawers keep the operator in flow.

## Durable Product Principles

1. **Commit-Boundary Safety:** Never silently re-dispatch an unconfirmed submission; surface explicit verification states (`需核对`, `待验证`) and preserve local drafts, session history, and outputs.
2. **Pool Resilience Over Single-Account Fragility:** Surface per-account health, cooldown timers, and remaining quota clearly so multi-account rotation is observable and predictable.
3. **Built-In Browser Sovereignty:** All browser automation, fingerprinting, login takeover, and page-bridge operations use the app-managed `Camoufox` environments and dedicated bridges rather than external browsers.
4. **Unified Design Tokens:** Every workbench mode and utility tool draws from the shared `internal/ui/design.go` token contract for consistent density, typography, semantic contrast, and Light/Dark parity.
