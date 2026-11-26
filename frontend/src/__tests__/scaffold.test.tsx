import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "@/App";
import {
  getDrawerFrameUrl,
  getEditOrigUrl,
  getEditPreviewUrl,
  toFullUrl,
  toThumbUrl,
} from "@/lib/assets";
import { dispatchTopEscape, pushEscapeHandler } from "@/lib/shortcuts";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import {
  TAB_BROWSER,
  TAB_DEDUP,
  TAB_FISSION,
  TAB_GEN,
  TAB_WASH,
  resetStoreForTests,
  useAppStore,
} from "@/store";

afterEach(() => {
  cleanup();
  resetStoreForTests();
});

describe("assets URL helpers", () => {
  it("appends &w= for thumbnails and strips w for full-res", () => {
    const raw = "tulaimedia://localhost/file?t=abc123&v=99";
    const thumb = toThumbUrl(raw, 360);
    expect(thumb).toContain("t=abc123");
    expect(thumb).toContain("v=99");
    expect(thumb).toContain("w=360");
    expect(toFullUrl(thumb)).not.toContain("w=");
  });

  it("supports Windows WebView2 http://tulaimedia.localhost origin", () => {
    const raw = "http://tulaimedia.localhost/file?t=xyz&v=10";
    expect(toThumbUrl(raw, 240)).toContain("http://tulaimedia.localhost/file");
    expect(toThumbUrl(raw, 240)).toContain("w=240");
    expect(getDrawerFrameUrl("", 7, "http://tulaimedia.localhost")).toBe(
      "http://tulaimedia.localhost/drawer/frame?v=7",
    );
    expect(getEditOrigUrl("", 3, "http://tulaimedia.localhost")).toBe(
      "http://tulaimedia.localhost/edit/orig?v=3",
    );
    expect(getEditPreviewUrl("", 4, "http://tulaimedia.localhost")).toBe(
      "http://tulaimedia.localhost/edit/preview?v=4",
    );
  });
});

describe("shortcuts priority Escape stack", () => {
  it("closes higher priority layers first", () => {
    const closed: string[] = [];
    const offLow = pushEscapeHandler("low", () => {
      closed.push("low");
    }, 10);
    const offHigh = pushEscapeHandler("high", () => {
      closed.push("high");
    }, 50);

    expect(dispatchTopEscape()).toBe(true);
    expect(closed).toEqual(["high"]);

    offHigh();
    expect(dispatchTopEscape()).toBe(true);
    expect(closed).toEqual(["high", "low"]);
    offLow();
  });
});

describe("useSyncedDraft CJK IME & external sync", () => {
  function DraftHarness({
    serverValue,
    syncKey,
    onCommit,
  }: {
    serverValue: string;
    syncKey: string;
    onCommit: (v: string) => void;
  }) {
    const draft = useSyncedDraft(serverValue, syncKey, onCommit, { debounceMs: 50 });
    return (
      <input
        data-testid="draft-input"
        value={draft.value}
        onChange={(e) => draft.onChange(e.target.value)}
        onCompositionStart={draft.onCompositionStart}
        onCompositionEnd={(e) => draft.onCompositionEnd(e.currentTarget.value)}
        onBlur={draft.onBlur}
      />
    );
  }

  it("defers commits during IME composition and commits on compositionend", () => {
    const commits: string[] = [];
    render(
      <DraftHarness serverValue="初始" syncKey="a" onCommit={(v) => commits.push(v)} />,
    );
    const input = screen.getByTestId("draft-input") as HTMLInputElement;

    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "初始ni" } });
    expect(commits).toEqual([]);

    fireEvent.compositionEnd(input, { target: { value: "初始你好" } });
    expect(commits).toEqual(["初始你好"]);
  });

  it("resyncs immediately when syncKey changes", () => {
    const onCommit = vi.fn();
    const { rerender } = render(
      <DraftHarness serverValue="模板A" syncKey="tpl-1" onCommit={onCommit} />,
    );
    const input = screen.getByTestId("draft-input") as HTMLInputElement;
    expect(input.value).toBe("模板A");

    rerender(<DraftHarness serverValue="模板B" syncKey="tpl-2" onCommit={onCommit} />);
    expect(input.value).toBe("模板B");
  });
});

describe("App shell contracts", () => {
  it("renders workbench mode tabs in exact order and hides running/pool badges on workbench", () => {
    useAppStore.setState((st) => ({
      shell: {
        ...st.shell,
        tab: TAB_WASH,
        running: true,
        pool: { ready: 2, busy: 1, issue: 0, total: 3, summary: "账号 2/3" },
      },
    }));

    render(<App />);
    expect(screen.getByTestId("app-root")).toBeInTheDocument();
    expect(screen.getByTestId("sidebar-rail")).toBeInTheDocument();
    expect(screen.getByTestId("topbar")).toBeInTheDocument();

    const modeTabs = screen.getByTestId("wb-mode-tabs");
    const labels = Array.from(modeTabs.querySelectorAll("[role='tab']")).map(
      (el) => el.textContent,
    );
    expect(labels).toEqual(["批量洗图", "自由生图", "裂变图片"]);

    // Anti-reference contract: never show duplicate running/pool chrome on workbench
    expect(screen.queryByTestId("topbar-running-badge")).not.toBeInTheDocument();
    expect(screen.queryByTestId("topbar-pool-summary")).not.toBeInTheDocument();
    expect(screen.queryByTestId("topbar-log-btn")).not.toBeInTheDocument();
    expect(screen.getByTestId("sidebar-log-btn")).toBeInTheDocument();

    // Provider submenu testids
    expect(screen.getByTestId("nav-workbench")).toBeInTheDocument();
    expect(screen.getByTestId("nav-platform-submenu")).toBeInTheDocument();
    expect(screen.getByTestId("nav-platform-doubao")).toBeInTheDocument();
    expect(screen.getByTestId("nav-platform-qianwen")).toBeInTheDocument();
    expect(screen.getByTestId("nav-platform-qwen_intl")).toBeInTheDocument();
  });

  it("shows running pill and hides pool pill on utility pages without workbench topbar", () => {
    useAppStore.setState((st) => ({
      shell: {
        ...st.shell,
        tab: TAB_DEDUP,
        running: true,
        progText: "3/10",
        pool: { ready: 2, busy: 1, issue: 0, total: 3, summary: "账号 2/3" },
      },
    }));

    render(<App />);
    expect(screen.queryByTestId("topbar")).not.toBeInTheDocument();
    expect(screen.getByTestId("topbar-running-badge")).toBeInTheDocument();
    expect(screen.queryByTestId("topbar-pool-summary")).not.toBeInTheDocument();
    expect(screen.queryByTestId("topbar-log-btn")).not.toBeInTheDocument();
    expect(screen.getByTestId("sidebar-log-btn")).toBeInTheDocument();
  });

  it("places log toggle above settings in the sidebar and toggles LogDrawer", () => {
    useAppStore.setState((st) => ({
      shell: {
        ...st.shell,
        logTotal: 2,
        logErrors: 0,
      },
    }));

    render(<App />);
    const rail = screen.getByTestId("sidebar-rail");
    const logBtn = screen.getByTestId("sidebar-log-btn");
    const settingsBtn = screen.getByTestId("nav-settings");

    expect(rail).toContainElement(logBtn);
    expect(rail).toContainElement(settingsBtn);
    expect(
      logBtn.compareDocumentPosition(settingsBtn) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(logBtn).toHaveTextContent("日志");
    expect(logBtn).toHaveTextContent("2");
    expect(logBtn).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(logBtn);
    expect(useAppStore.getState().shell.logOpen).toBe(true);
    const drawer = screen.getByTestId("log-drawer");
    expect(drawer).toBeInTheDocument();
    const expectedLeft = `${Math.round(Math.max(12, (window.innerWidth - Math.min(880, Math.max(320, window.innerWidth - 24))) / 2))}px`;
    const expectedTop = `${Math.round(Math.max(12, (window.innerHeight - Math.min(520, Math.max(260, window.innerHeight - 24))) / 2))}px`;
    expect(drawer).toHaveStyle({ left: expectedLeft, top: expectedTop });
    expect(logBtn).toHaveAttribute("aria-pressed", "true");

    act(() => {
      useAppStore.setState((st) => ({
        shell: {
          ...st.shell,
          navCollapsed: true,
          navManual: true,
          logErrors: 3,
        },
      }));
    });

    expect(logBtn).toHaveAttribute("aria-label", "运行日志 (3 条错误)");
    expect(drawer).toHaveStyle({ left: expectedLeft, top: expectedTop });
  });

  it("hides pool summary on BrowserPage while keeping running badge", () => {
    useAppStore.setState((st) => ({
      shell: {
        ...st.shell,
        tab: TAB_BROWSER,
        running: true,
        pool: { ready: 2, busy: 0, issue: 0, total: 2, summary: "账号 2/2" },
      },
    }));

    render(<App />);
    expect(screen.getByTestId("topbar-running-badge")).toBeInTheDocument();
    expect(screen.queryByTestId("topbar-pool-summary")).not.toBeInTheDocument();
  });

  it("switches workbench mode tabs and updates store tab", () => {
    render(<App />);
    const genTab = screen.getByRole("tab", { name: "自由生图" });
    fireEvent.click(genTab);
    expect(useAppStore.getState().shell.tab).toBe(TAB_GEN);

    const fissionTab = screen.getByRole("tab", { name: "裂变图片" });
    fireEvent.click(fissionTab);
    expect(useAppStore.getState().shell.tab).toBe(TAB_FISSION);
  });

  it("renders LogDrawer and BrowserDrawer when open", () => {
    act(() => {
      useAppStore.setState((st) => ({
        logs: {
          ...st.logs,
          open: true,
          total: 1,
          shown: 1,
          countAll: 1,
          countInfo: 1,
          entries: [
            {
              seq: 1,
              time: "12:00:00",
              level: "INF",
              code: "",
              source: "ui",
              text: "系统就绪",
              detail: "",
              elapsed: "",
              expanded: false,
            },
          ],
        },
        browserDrawer: {
          ...st.browserDrawer,
          open: true,
          accountId: 42,
          name: "豆包主号",
          platform: "豆包",
          manual: true,
          awaitingVerify: true,
          width: 1000,
          height: 600,
        },
      }));
    });

    render(<App />);
    expect(screen.getByTestId("log-drawer")).toBeInTheDocument();
    expect(screen.getByTestId("log-close-btn")).toBeInTheDocument();
    expect(screen.getByTestId("browser-drawer-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("browser-drawer-handle")).toBeInTheDocument();
    expect(screen.getByTestId("browser-drawer-stage")).toBeInTheDocument();
    expect(screen.getByTestId("browser-drawer-verify-banner")).toHaveTextContent(
      "超时计时已暂停",
    );
  });

  it("renders live download progress in sidebar update card when downloading", () => {
    act(() => {
      useAppStore.setState((st) => ({
        shell: {
          ...st.shell,
          update: {
            ...st.shell.update,
            visible: true,
            version: "0.1.41",
            downloading: true,
            phase: "downloading",
            fraction: 0.64,
            doneBytes: 64 * 1024 * 1024,
            totalBytes: 100 * 1024 * 1024,
            speedBps: 6 * 1024 * 1024,
            etaSeconds: 6,
            source: "edgeone.gh-proxy.org",
          },
        },
      }));
    });

    render(<App />);
    const card = screen.getByTestId("sidebar-update-card");
    expect(card).toHaveTextContent("更新 v0.1.41");
    expect(screen.getByTestId("sidebar-update-percent")).toHaveTextContent("64%");
    expect(screen.getByTestId("sidebar-update-progress")).toHaveAttribute("aria-valuenow", "64");
    expect(card).toHaveTextContent("6.0 MB/s · 剩余 6 秒");
  });

  it("forwards mouse drag buttons bitmask and flushes pending move before mouseReleased", async () => {
    const { Browser } = await import("@/lib/ipc");
    const sendSpy = vi.spyOn(Browser, "sendBrowserDrawerInput").mockResolvedValue(true);
    try {
      act(() => {
        useAppStore.setState((st) => ({
          browserDrawer: {
            ...st.browserDrawer,
            open: true,
            accountId: 7,
            name: "千问1",
            platform: "千问",
            manual: true,
            awaitingVerify: true,
            width: 1000,
            height: 500,
          },
        }));
      });

      render(<App />);
      const stage = screen.getByTestId("browser-drawer-stage");
      vi.spyOn(stage, "getBoundingClientRect").mockReturnValue({
        left: 0,
        top: 0,
        width: 1000,
        height: 500,
        right: 1000,
        bottom: 500,
        x: 0,
        y: 0,
        toJSON: () => ({}),
      });
      Object.defineProperty(stage, "clientWidth", { value: 1000, configurable: true });
      Object.defineProperty(stage, "clientHeight", { value: 500, configurable: true });

      const makePtr = (type: string, init: MouseEventInit) => {
        const ev = new MouseEvent(type, { bubbles: true, cancelable: true, ...init });
        Object.defineProperty(ev, "pointerId", { value: 1 });
        return ev;
      };
      fireEvent(stage, makePtr("pointerdown", { button: 0, buttons: 1, clientX: 100, clientY: 200 }));
      fireEvent(stage, makePtr("pointermove", { buttons: 1, clientX: 260, clientY: 200 }));
      // Release within the same frame before rAF runs: must flush move before mouseReleased
      fireEvent(stage, makePtr("pointerup", { button: 0, buttons: 0, clientX: 260, clientY: 200 }));

      const calls = sendSpy.mock.calls.map((c) => c[0]);
      expect(calls.map((c) => c.kind)).toEqual(["mousePressed", "mouseMoved", "mouseReleased"]);
      expect(calls[0]).toMatchObject({ kind: "mousePressed", button: "left", buttons: 1, x: 100, y: 200 });
      expect(calls[1]).toMatchObject({ kind: "mouseMoved", buttons: 1, x: 260, y: 200 });
      expect(calls[2]).toMatchObject({ kind: "mouseReleased", button: "left", buttons: 0, x: 260, y: 200 });
    } finally {
      sendSpy.mockRestore();
    }
  });

  it("handles overlay IPC buttons without unhandled rejections outside MyGo runtime", async () => {
    const rejections: unknown[] = [];
    const onRejection = (ev: PromiseRejectionEvent) => {
      rejections.push(ev.reason);
    };
    window.addEventListener("unhandledrejection", onRejection);

    try {
      act(() => {
        useAppStore.setState((st) => ({
          shell: {
            ...st.shell,
            update: { ...st.shell.update, visible: true, version: "9.9.9" },
          },
          logs: {
            ...st.logs,
            open: true,
            total: 1,
            shown: 1,
            countAll: 1,
            countError: 1,
            entries: [
              {
                seq: 1,
                time: "12:00:00",
                level: "ERR",
                code: "",
                source: "ui",
                text: "测试错误",
                detail: "详情",
                elapsed: "",
                expanded: false,
              },
            ],
          },
          browserDrawer: {
            ...st.browserDrawer,
            open: true,
            accountId: 7,
            name: "测试号",
            platform: "豆包",
            manual: true,
            copyReady: true,
            copyText: "abc",
          },
        }));
      });

      render(<App />);
      fireEvent.click(screen.getByRole("button", { name: "忽略" }));
      fireEvent.click(screen.getByRole("button", { name: "暂停跟随" }));
      fireEvent.click(screen.getByRole("button", { name: "导出" }));
      fireEvent.click(screen.getByRole("button", { name: "复制" }));
      fireEvent.click(screen.getByRole("button", { name: "清空" }));
      fireEvent.click(screen.getByRole("button", { name: "错误 1" }));
      fireEvent.click(screen.getByRole("button", { name: "全部来源" }));
      fireEvent.click(screen.getByRole("button", { name: "复制内容" }));
      fireEvent.click(screen.getByRole("button", { name: "我已完成，继续任务" }));

      await new Promise((r) => setTimeout(r, 20));
      expect(rejections).toEqual([]);
    } finally {
      window.removeEventListener("unhandledrejection", onRejection);
    }
  });

  it("blocks native browser contextmenu globally", () => {
    render(<App />);
    const rootEvent = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
    screen.getByTestId("app-root").dispatchEvent(rootEvent);
    expect(rootEvent.defaultPrevented).toBe(true);

    const bodyEvent = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
    document.body.dispatchEvent(bodyEvent);
    expect(bodyEvent.defaultPrevented).toBe(true);
  });
});
