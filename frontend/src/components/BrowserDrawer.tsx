import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import {
  Badge,
  Button,
  IconButton,
  OutlineBadge,
  Tooltip,
  type ButtonSize,
} from "@/components/ui";
import { cn } from "@/lib/cn";
import { Browser, Core, fireIpc } from "@/lib/ipc";
import { useEscapeHandler } from "@/lib/shortcuts";
import {
  setAccountListLeft,
  showToast,
  useAccountListLeft,
  useBrowserDrawer,
  useViewedAccountId,
} from "@/store";
import { BrowserStage } from "./BrowserStage";

/**
 * Wraps the account list on Workbench (`AccountLanes`) and `BrowserPage`, measuring
 * its left edge so `BrowserDrawer` places itself without covering account names
 * or "可视" buttons.
 */
export function AccountListFrame({
  className,
  "data-testid": testId,
  children,
}: {
  className?: string;
  "data-testid"?: string;
  children?: ReactNode;
}) {
  const ref = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => {
      const rect = el.getBoundingClientRect();
      if (rect.width > 0) setAccountListLeft(rect.left);
    };
    measure();
    window.addEventListener("resize", measure);
    let ro: ResizeObserver | undefined;
    if (typeof ResizeObserver !== "undefined") {
      ro = new ResizeObserver(measure);
      ro.observe(el);
    }
    return () => {
      window.removeEventListener("resize", measure);
      ro?.disconnect();
    };
  }, []);

  return (
    <div ref={ref} data-testid={testId} className={className}>
      {children}
    </div>
  );
}

export function AccountViewButton({
  accountId,
  name,
  iconOnly = false,
  size = "small",
}: {
  accountId: number;
  name?: string;
  iconOnly?: boolean;
  size?: ButtonSize;
}) {
  const viewedId = useViewedAccountId();
  const variant = viewedId === accountId ? "primary" : "outline";
  const label = `查看浏览器 · ${name || `账号 #${accountId}`}`;
  const onClick = () => fireIpc(() => Browser.openBrowserDrawer(accountId));

  if (iconOnly) {
    return (
      <Tooltip content="查看实时画面" side="top">
        <IconButton
          data-testid={`browser-view-btn-${accountId}`}
          icon="eye"
          size={size}
          variant={variant}
          aria-label={label}
          onClick={onClick}
        />
      </Tooltip>
    );
  }

  return (
    <Button
      text="查看画面"
      icon="eye"
      size={size}
      variant={variant}
      aria-label={label}
      onClick={onClick}
    />
  );
}

export function BrowserDrawer() {
  const st = useBrowserDrawer();
  const listLeft = useAccountListLeft();

  const [viewport, setViewport] = useState(() => ({
    w: typeof window !== "undefined" ? window.innerWidth : 1160,
    h: typeof window !== "undefined" ? window.innerHeight : 740,
  }));
  const [position, setPosition] = useState<{ x: number; y: number } | null>(null);
  const [customSize, setCustomSize] = useState<{ width: number; height: number } | null>(null);
  const [maximized, setMaximized] = useState(false);

  useEffect(() => {
    const onResize = () =>
      setViewport({ w: window.innerWidth || 1160, h: window.innerHeight || 740 });
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const closeDrawer = useCallback(() => {
    fireIpc(() => Browser.closeBrowserDrawer());
  }, []);

  useEscapeHandler("browser-drawer", true, closeDrawer, 30);

  const frame = useMemo(() => {
    const w = viewport.w;
    const h = viewport.h;
    const gap = 14;
    const maxW = Math.max(480, w - 2 * gap);
    const maxH = Math.max(400, h - 2 * gap);

    if (maximized) {
      return {
        left: gap,
        top: gap,
        width: Math.round(maxW),
        height: Math.round(maxH),
      };
    }

    const defaultWidth = Math.min(maxW, Math.max(520, Math.min(1160, Math.round(w * 0.84))));
    const headerChromeH = 62;
    const aspectH =
      st.width > 0 && st.height > 0
        ? Math.round(defaultWidth * (st.height / st.width)) + headerChromeH
        : Math.round(h * 0.86);
    const defaultHeight = Math.min(maxH, Math.max(440, Math.min(Math.round(h * 0.9), aspectH)));

    const width = customSize ? Math.max(440, Math.min(customSize.width, maxW)) : defaultWidth;
    const height = customSize ? Math.max(380, Math.min(customSize.height, maxH)) : defaultHeight;

    // Prefer right side of sidebar rail when wide enough, otherwise center in viewport
    const defaultX = listLeft > 0 && w - width >= 180 ? Math.max(168, Math.round((w - width + 140) / 2)) : Math.round((w - width) / 2);
    const rawX = position?.x ?? defaultX;
    const rawY = position?.y ?? (h - height) / 2;

    return {
      left: Math.round(Math.max(gap, Math.min(rawX, w - width - gap))),
      top: Math.round(Math.max(gap, Math.min(rawY, h - height - gap))),
      width: Math.round(width),
      height: Math.round(height),
    };
  }, [viewport, listLeft, position, customSize, maximized, st.width, st.height]);

  // Drag handle pointer tracking
  const onDragStart = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const startX = e.clientX;
    const startY = e.clientY;
    const origLeft = frame.left;
    const origTop = frame.top;

    const onMove = (ev: PointerEvent) => {
      setPosition({
        x: origLeft + (ev.clientX - startX),
        y: origTop + (ev.clientY - startY),
      });
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  };

  // Bottom-right resize grip tracking
  const onResizeStart = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    const startX = e.clientX;
    const startY = e.clientY;
    const origW = frame.width;
    const origH = frame.height;
    const origLeft = frame.left;
    const origTop = frame.top;

    setPosition((prev) => prev ?? { x: origLeft, y: origTop });

    const onMove = (ev: PointerEvent) => {
      setCustomSize({
        width: origW + (ev.clientX - startX),
        height: origH + (ev.clientY - startY),
      });
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  };

  const accountName = st.name || (st.accountId ? `账号 #${st.accountId}` : "账号浏览器");
  const subtitle = [st.title, st.url].filter(Boolean).join(" · ");

  return (
    <div
      role="dialog"
      aria-label={`浏览器 · ${accountName}`}
      data-testid="browser-drawer-dialog"
      style={{
        left: frame.left,
        top: frame.top,
        width: frame.width,
        height: frame.height,
      }}
      className={cn(
        "fixed z-[45] flex flex-col overflow-hidden rounded-[16px] border border-hairline bg-card float-shadow",
        "dark:border-[#2D3A52] animate-tulai-dialog",
      )}
    >
      {/* Header */}
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-divider px-4 py-3">
        <div
          data-testid="browser-drawer-handle"
          onPointerDown={onDragStart}
          className="flex min-w-0 flex-1 cursor-grab flex-col gap-0.5 select-none active:cursor-grabbing"
        >
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate text-sm font-semibold text-ink">{accountName}</span>
            {st.platform ? <OutlineBadge text={st.platform} /> : null}
            {st.awaitingVerify && !st.err && !st.loading ? (
              <Badge text="等待人工验证" variant="warn" />
            ) : st.manual && !st.err && !st.loading ? (
              <Badge text="手动操作" variant="warn" />
            ) : null}
          </div>
          {subtitle ? (
            <span className="truncate text-xs text-muted">{subtitle}</span>
          ) : null}
        </div>

        <div className="flex shrink-0 flex-wrap items-center gap-1.5">
          {st.copyReady && st.copyText ? (
            <Button
              text="复制内容"
              size="small"
              icon="copy"
              variant="outline"
              onClick={() => {
                fireIpc(() => Core.copyText(st.copyText || ""));
                fireIpc(() => Browser.clearBrowserDrawerCopy());
                showToast("已复制到剪贴板", "success");
              }}
            />
          ) : null}
          {(st.manual || st.resuming) && !st.loading && !st.err ? (
            <Button
              text={st.resuming ? "正在恢复任务..." : "我已完成，继续任务"}
              size="small"
              variant="primary"
              busy={Boolean(st.resuming)}
              onClick={() => fireIpc(() => Browser.resumeBrowserDrawer())}
            />
          ) : null}
          <Tooltip content={maximized ? "还原窗口大小" : "最大化窗口"} side="bottom">
            <IconButton
              data-testid="browser-drawer-maximize"
              icon={maximized ? "minimize-2" : "maximize-2"}
              size="small"
              variant="ghost"
              aria-label={maximized ? "还原窗口大小" : "最大化窗口"}
              onClick={() => setMaximized((v) => !v)}
            />
          </Tooltip>
          <Button
            text="关闭"
            size="small"
            variant="ghost"
            onClick={() => fireIpc(() => Browser.closeBrowserDrawer())}
          />
        </div>
      </div>

      {/* Status / Verification / Error Strip */}
      {st.loading || st.err ? (
        <div className="mx-3.5 my-2 shrink-0 rounded-[10px] bg-sunken px-3 py-1.5">
          <span
            className={cn(
              "select-text text-xs",
              st.err ? "text-status-danger" : "text-muted",
            )}
          >
            {st.err || "正在加载账号页面…"}
          </span>
        </div>
      ) : st.awaitingVerify ? (
        <div
          data-testid="browser-drawer-verify-banner"
          className="mx-3.5 my-2 flex shrink-0 items-center justify-between gap-2 rounded-[10px] border border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] px-3 py-1.5"
        >
          <span className="text-xs font-medium text-status-warn">
            请先在下方完成验证（超时计时已暂停），完成后点击「我已完成，继续任务」
          </span>
        </div>
      ) : null}

      {/* Live Interactive CDP Stage */}
      <BrowserStage data-testid="browser-drawer-stage" />

      {/* Bottom-right resize grip */}
      <div
        aria-label="调整窗口大小"
        onPointerDown={onResizeStart}
        className="absolute right-0 bottom-0 z-10 flex size-4 cursor-nwse-resize items-end justify-end p-1 text-slate-400/70 hover:text-slate-200"
      >
        <svg width="8" height="8" viewBox="0 0 8 8" fill="none" aria-hidden="true">
          <path
            d="M7 1L1 7M7 4L4 7"
            stroke="currentColor"
            strokeWidth="1.2"
            strokeLinecap="round"
          />
        </svg>
      </div>
    </div>
  );
}
