import "@testing-library/jest-dom/vitest";
import type { ReactElement } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui";
import * as ipc from "@/lib/ipc";
import * as imageLoader from "@/lib/dotmatrix/imageLoader";
import { dispatchTopEscape } from "@/lib/shortcuts";
import { BrowserPage } from "@/pages/BrowserPage";
import { PromptsPage } from "@/pages/PromptsPage";
import { SettingsPage } from "@/pages/SettingsPage";
import { WizardView } from "@/pages/WizardView";
import {
  TAB_FISSION,
  TAB_GEN,
  TAB_SETTINGS,
  TAB_WASH,
  resetStoreForTests,
  useAppStore,
  type BrowserRowDTO,
  type BrowserStateDTO,
  type PromptsStateDTO,
  type SettingsStateDTO,
} from "@/store";

function renderPage(ui: ReactElement) {
  const result = render(<TooltipProvider>{ui}</TooltipProvider>);
  return {
    ...result,
    rerender: (nextUi: ReactElement) =>
      result.rerender(<TooltipProvider>{nextUi}</TooltipProvider>),
  };
}

// JSDOM shims for @tanstack/react-virtual and Radix UI Select/Dialog
beforeAll(() => {
  if (typeof globalThis.ResizeObserver === "undefined") {
    globalThis.ResizeObserver = class ResizeObserver {
      private cb: ResizeObserverCallback;
      constructor(cb: ResizeObserverCallback) {
        this.cb = cb;
      }
      observe(target: Element) {
        this.cb(
          [
            {
              target,
              contentRect: {
                width: 800,
                height: 600,
                top: 0,
                left: 0,
                bottom: 600,
                right: 800,
                x: 0,
                y: 0,
                toJSON: () => ({}),
              },
              borderBoxSize: [],
              contentBoxSize: [],
              devicePixelContentBoxSize: [],
            },
          ],
          this,
        );
      }
      unobserve() {}
      disconnect() {}
    };
  }

  Object.defineProperty(HTMLElement.prototype, "clientHeight", {
    configurable: true,
    get() {
      return 600;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "clientWidth", {
    configurable: true,
    get() {
      return 800;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get() {
      return 600;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    get() {
      return 800;
    },
  });
  HTMLElement.prototype.getBoundingClientRect = function () {
    return {
      width: 800,
      height: 600,
      top: 0,
      left: 0,
      bottom: 600,
      right: 800,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    };
  };

  if (!HTMLElement.prototype.scrollIntoView) {
    HTMLElement.prototype.scrollIntoView = () => {};
  }
  if (!HTMLElement.prototype.hasPointerCapture) {
    HTMLElement.prototype.hasPointerCapture = () => false;
  }
  if (!HTMLElement.prototype.releasePointerCapture) {
    HTMLElement.prototype.releasePointerCapture = () => {};
  }
  if (!HTMLElement.prototype.setPointerCapture) {
    HTMLElement.prototype.setPointerCapture = () => {};
  }
});

function makeBrowserRow(overrides: Partial<BrowserRowDTO> = {}): BrowserRowDTO {
  return {
    id: 1,
    platform: "豆包",
    name: "豆包主号",
    email: "",
    engine: "Fingerprint",
    isRunning: false,
    isLoggedIn: true,
    active: false,
    disabled: false,
    checked: false,
    pending: false,
    pendingOp: "",
    health: "就绪",
    healthTone: "success",
    proxy: "",
    lastError: "",
    todayCount: 3,
    usageLabel: "3/50",
    usagePct: 6,
    ...overrides,
  };
}

function makeBrowserState(overrides: Partial<BrowserStateDTO> = {}): BrowserStateDTO {
  return {
    backend: "Fingerprint",
    backendLabel: "Fingerprint",
    engines: ["Fingerprint"],
    ready: true,
    busy: false,
    badgeLabel: "已安装",
    badgeTone: "success",
    status: "Fingerprint 就绪",
    detail: "/managed/fingerprint",
    percent: 0,
    done: 0,
    total: 0,
    speed: 0,
    supported: true,
    running: false,
    rows: [
      makeBrowserRow({ id: 1, platform: "豆包", name: "豆包主号" }),
      makeBrowserRow({ id: 2, platform: "千问", name: "千问副号", engine: "Fingerprint" }),
    ],
    selectedId: 1,
    checkedCount: 0,
    batchActive: false,
    batchKind: "",
    batchText: "",
    missingHint: "",
    sharedExitHints: [],
    dlBrowserBusy: false,
    dlBrowserOpen: false,
    dlBrowserUrl: "",
    ...overrides,
  };
}

function makePromptsState(overrides: Partial<PromptsStateDTO> = {}): PromptsStateDTO {
  return {
    items: [
      {
        id: "doubao_wash",
        name: "洗图",
        label: "洗图",
        kind: "wash",
        kindLbl: "洗图",
        text: "保留主体并重绘为二维手绘风格",
        builtin: true,
      },
      {
        id: "user_1",
        name: "电商白底主图",
        label: "电商白底主图",
        kind: "gen",
        kindLbl: "生图",
        text: "纯白背景，柔和棚拍光影",
        builtin: false,
      },
    ],
    selectedId: "user_1",
    selectedName: "电商白底主图",
    selectedKind: "生图",
    selectedText: "纯白背景，柔和棚拍光影",
    selectedIsBln: false,
    filter: "",
    note: "",
    suffix: "4K 超清无水印",
    useSuffix: true,
    ...overrides,
  };
}

function makeSettingsState(overrides: Partial<SettingsStateDTO> = {}): SettingsStateDTO {
  return {
    tab: 0,
    outputPath: "/tmp/out",
    resolvedOutput: "/tmp/out",
    genAutoNumber: true,
    washAutoNumber: false,
    numPrefix: "img_",
    numStart: 1,
    numWidth: 3,
    dlDir: "/tmp/dl",
    resolvedDlDir: "/tmp/dl",
    downloadConc: 4,
    downloadRetries: 1,
    downloadRateKb: 0,
    removeWatermark: true,
    autoDeleteHistory: false,
    concurrencyAuto: true,
    concurrencyMax: 3,
    sendStrategy: "顺序循环",
    retryCount: 2,
    sendWait: 5,
    dailyCapDoubao: 50,
    dailyCapQianwen: 30,
    dailyCapQwenIntl: 100,
    waitBefore: 0,
    waitResult: true,
    waitResultSeconds: 90,
    taskSwitch: false,
    switchCount: 1,
    startBrowserIdx: 1,
    taskBrowserVisible: false,
    riskyWords: "违规词",
    proxyCustom: false,
    proxyUrl: "",
    captchaEnabled: true,
    captchaBackend: "free",
    customUrl: "",
    customKey: "",
    customModel: "",
    ikunocrKey: "",
    autoCheck: true,
    updateMirror: "",
    version: "0.1.38",
    updateBusy: false,
    updateErr: "",
    updateNotice: "",
    dotMatrixEffect: "grain",
    ...overrides,
  };
}

beforeEach(() => {
  vi.restoreAllMocks();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null as never);
});

afterEach(() => {
  cleanup();
  resetStoreForTests();
  document.documentElement.classList.remove("dark");
});

describe("BrowserPage (账号池)", () => {
  it("shows missing hint and shortcut to Settings when runtime is not ready", async () => {
    vi.spyOn(ipc, "isMyGoRuntime").mockReturnValue(true);
    const setTabSpy = vi.spyOn(ipc.Core, "setTab").mockResolvedValue(undefined);
    vi.spyOn(ipc.Core, "getShellState").mockResolvedValue({
      ...useAppStore.getState().shell,
      tab: TAB_SETTINGS,
    });
    vi.spyOn(ipc.Settings, "getSettingsState").mockResolvedValue(makeSettingsState({ tab: 2 }));
    vi.spyOn(ipc.Browser, "getBrowserState").mockResolvedValue(makeBrowserState());
    const setFieldSpy = vi.spyOn(ipc.Settings, "setSettingField").mockResolvedValue(undefined);

    useAppStore.setState({
      browser: makeBrowserState({
        ready: false,
        busy: false,
        badgeLabel: "未安装",
        badgeTone: "warning",
        missingHint: "以下勾选平台尚未添加账号：千问",
      }),
    });

    renderPage(<BrowserPage />);

    expect(screen.queryByTestId("pool-summary-bar")).not.toBeInTheDocument();
    expect(screen.getByTestId("browser-missing-hint")).toHaveTextContent(
      "以下勾选平台尚未添加账号：千问",
    );
    expect(screen.queryByTestId("browser-install-btn")).not.toBeInTheDocument();

    const goSettingsBtn = screen.getByTestId("browser-go-settings-btn");
    expect(goSettingsBtn).toHaveTextContent("前往设置安装");
    fireEvent.click(goSettingsBtn);
    await waitFor(() => {
      expect(setTabSpy).toHaveBeenCalledWith(TAB_SETTINGS);
      expect(setFieldSpy).toHaveBeenCalledWith("tab", "2");
    });
  });

  it("shows shared-exit warning in account pool, respects platform filter, and opens send interval settings", async () => {
    vi.spyOn(ipc, "isMyGoRuntime").mockReturnValue(true);
    const setTabSpy = vi.spyOn(ipc.Core, "setTab").mockResolvedValue(undefined);
    vi.spyOn(ipc.Core, "getShellState").mockResolvedValue({
      ...useAppStore.getState().shell,
      tab: TAB_SETTINGS,
    });
    vi.spyOn(ipc.Settings, "getSettingsState").mockResolvedValue(makeSettingsState({ tab: 1 }));
    vi.spyOn(ipc.Browser, "getBrowserState").mockResolvedValue(makeBrowserState());
    const setFieldSpy = vi.spyOn(ipc.Settings, "setSettingField").mockResolvedValue(undefined);

    useAppStore.setState({
      browser: makeBrowserState({
        sharedExitHints: [
          {
            platform: "豆包",
            message: "当前有 2 个豆包账号共用本机直连出口，建议配置独立代理或增大发送间隔以减少触发验证",
          },
          {
            platform: "千问",
            message: "当前有 2 个千问账号共用同一代理出口，建议配置独立代理或增大发送间隔以减少触发验证",
          },
        ],
      }),
    });

    renderPage(<BrowserPage />);

    expect(screen.getByTestId("browser-shared-exit-hint-豆包")).toHaveTextContent(
      "2 个豆包账号共用本机直连出口",
    );
    expect(screen.getByTestId("browser-shared-exit-hint-千问")).toHaveTextContent(
      "2 个千问账号共用同一代理出口",
    );

    const filterGroup = screen.getByTestId("browser-platform-filter");
    fireEvent.click(within(filterGroup).getByRole("tab", { name: "千问" }));
    expect(screen.queryByTestId("browser-shared-exit-hint-豆包")).not.toBeInTheDocument();
    expect(screen.getByTestId("browser-shared-exit-hint-千问")).toHaveTextContent(
      "2 个千问账号共用同一代理出口",
    );

    fireEvent.click(screen.getByTestId("browser-shared-exit-interval-btn"));
    await waitFor(() => {
      expect(setTabSpy).toHaveBeenCalledWith(TAB_SETTINGS);
      expect(setFieldSpy).toHaveBeenCalledWith("tab", "1");
    });
  });

  it("allows closing shared-exit warning banner, preserves dismissal across remount, and re-shows on changed message", () => {
    const dismissSpy = vi
      .spyOn(ipc.Browser, "dismissSharedExitHints")
      .mockResolvedValue(undefined);

    useAppStore.setState({
      browser: makeBrowserState({
        sharedExitHints: [
          {
            platform: "豆包",
            message: "当前有 2 个豆包账号共用本机直连出口，建议配置独立代理或增大发送间隔以减少触发验证",
          },
          {
            platform: "千问",
            message: "当前有 2 个千问账号共用本机直连出口，建议配置独立代理或增大发送间隔以减少触发验证",
          },
        ],
      }),
    });

    const view = renderPage(<BrowserPage />);

    // Filter to 千问 and close only 千问's visible warning
    const filterGroup = screen.getByTestId("browser-platform-filter");
    fireEvent.click(within(filterGroup).getByRole("tab", { name: "千问" }));
    fireEvent.click(screen.getByTestId("browser-shared-exit-dismiss-btn"));

    expect(dismissSpy).toHaveBeenCalledWith(["千问"]);
    expect(screen.queryByTestId("browser-shared-exit-hint")).not.toBeInTheDocument();

    // Switching back to 全部 still shows 豆包, then closing dismisses 豆包 too
    fireEvent.click(within(filterGroup).getByRole("tab", { name: "全部" }));
    expect(screen.getByTestId("browser-shared-exit-hint-豆包")).toBeInTheDocument();
    expect(screen.queryByTestId("browser-shared-exit-hint-千问")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("browser-shared-exit-dismiss-btn"));
    expect(dismissSpy).toHaveBeenLastCalledWith(["豆包"]);
    expect(screen.queryByTestId("browser-shared-exit-hint")).not.toBeInTheDocument();

    // Unmounting and remounting (simulating tab switch) keeps the warning closed
    view.unmount();
    renderPage(<BrowserPage />);
    expect(screen.queryByTestId("browser-shared-exit-hint")).not.toBeInTheDocument();

    // Updating the warning message (e.g. a 3rd account added) re-shows the new warning
    act(() => {
      useAppStore.setState({
        browser: makeBrowserState({
          sharedExitHints: [
            {
              platform: "豆包",
              message: "当前有 3 个豆包账号共用本机直连出口，建议配置独立代理或增大发送间隔以减少触发验证",
            },
          ],
        }),
      });
    });
    expect(screen.getByTestId("browser-shared-exit-hint-豆包")).toHaveTextContent(
      "3 个豆包账号共用本机直连出口",
    );
  });

  it("opens rename modal on account click with IME support, Escape cancel, and Enter/Save commit", () => {
    const commitSpy = vi
      .spyOn(ipc.Browser, "commitBrowserRename")
      .mockResolvedValue(undefined);

    useAppStore.setState({
      browser: makeBrowserState(),
    });

    renderPage(<BrowserPage />);

    // Click account name to open rename modal dialog
    fireEvent.click(screen.getByText("豆包主号"));
    expect(screen.getByTestId("browser-rename-dialog")).toBeInTheDocument();
    const input = screen.getByTestId("browser-name-1") as HTMLInputElement;
    expect(input.value).toBe("豆包主号");

    // Simulate CJK IME editing and cancel via Escape without committing
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "豆包主号x" } });
    fireEvent.compositionEnd(input, { target: { value: "豆包主力A" } });
    act(() => {
      expect(dispatchTopEscape()).toBe(true);
    });
    expect(screen.queryByTestId("browser-rename-dialog")).not.toBeInTheDocument();
    expect(commitSpy).not.toHaveBeenCalled();

    // Reopen resets draft to current name and commits via Enter
    fireEvent.click(screen.getByText("豆包主号"));
    const reopened = screen.getByTestId("browser-name-1") as HTMLInputElement;
    expect(reopened.value).toBe("豆包主号");
    fireEvent.compositionStart(reopened);
    fireEvent.change(reopened, { target: { value: "豆包主号x" } });
    fireEvent.compositionEnd(reopened, { target: { value: "豆包主力A" } });
    fireEvent.keyDown(reopened, { key: "Enter" });
    expect(commitSpy).toHaveBeenCalledWith(1, "豆包主力A");
    expect(screen.queryByTestId("browser-rename-dialog")).not.toBeInTheDocument();

    // Also supports committing via Save button
    fireEvent.click(screen.getByText("豆包主号"));
    const saveInput = screen.getByTestId("browser-name-1") as HTMLInputElement;
    fireEvent.change(saveInput, { target: { value: "豆包主力B" } });
    fireEvent.click(screen.getByTestId("browser-rename-save-btn"));
    expect(commitSpy).toHaveBeenCalledWith(1, "豆包主力B");
    expect(screen.queryByTestId("browser-rename-dialog")).not.toBeInTheDocument();
  });

  it("shows error feedback when saveAccountProxy fails and closes on success", async () => {
    const proxySpy = vi
      .spyOn(ipc.Browser, "saveAccountProxy")
      .mockRejectedValueOnce(new Error("代理格式无效，请填 http://主机:端口"))
      .mockResolvedValueOnce(undefined);

    useAppStore.setState({
      browser: makeBrowserState({ rows: [makeBrowserRow({ id: 7, name: "代理测试号" })] }),
    });

    renderPage(<BrowserPage />);

    const proxyBtn = screen.getByTestId("browser-proxy-btn-7");
    expect(proxyBtn).toHaveTextContent("跟随全局");
    fireEvent.click(proxyBtn);
    const proxyInput = screen.getByTestId("browser-proxy-input-7") as HTMLInputElement;

    fireEvent.change(proxyInput, { target: { value: "bad-proxy" } });
    fireEvent.click(screen.getByRole("button", { name: "保存代理" }));

    await waitFor(() => {
      expect(screen.getByTestId("browser-proxy-err-7")).toHaveTextContent(
        "代理格式无效，请填 http://主机:端口",
      );
    });
    expect(screen.getByTestId("browser-proxy-input-7")).toBeInTheDocument();

    fireEvent.change(proxyInput, { target: { value: "http://127.0.0.1:7890" } });
    fireEvent.click(screen.getByRole("button", { name: "保存代理" }));

    await waitFor(() => {
      expect(screen.queryByTestId("browser-proxy-input-7")).not.toBeInTheDocument();
    });
    expect(proxySpy).toHaveBeenNthCalledWith(2, 7, "http://127.0.0.1:7890");
  });

  it("renders unified account table with header select-all and opens modal to create accounts", () => {
    const createSpy = vi.spyOn(ipc.Browser, "createBrowser").mockResolvedValue(undefined);
    const toggleAllSpy = vi
      .spyOn(ipc.Browser, "toggleAllBrowserChecks")
      .mockResolvedValue(undefined);

    useAppStore.setState({
      browser: makeBrowserState(),
    });

    renderPage(<BrowserPage />);

    // Unified table present and creation form hidden inside modal until clicked
    expect(screen.getByTestId("browser-account-table")).toBeInTheDocument();
    expect(screen.queryByText("Fingerprint")).not.toBeInTheDocument();
    expect(screen.queryByTestId("browser-new-name")).not.toBeInTheDocument();

    // Header select-all checkbox delegates to toggleAllBrowserChecks
    fireEvent.click(screen.getByTestId("browser-select-all"));
    expect(toggleAllSpy).toHaveBeenCalledWith(true);

    // Open modal, verify Escape priority-40 dismissal, reopen and submit
    fireEvent.click(screen.getByTestId("browser-new-btn"));
    expect(screen.getByTestId("browser-new-dialog")).toBeInTheDocument();
    act(() => {
      expect(dispatchTopEscape()).toBe(true);
    });
    expect(screen.queryByTestId("browser-new-dialog")).not.toBeInTheDocument();

    // Filter to 千问 first so default platform inside modal preselects 千问
    fireEvent.click(screen.getByRole("tab", { name: "千问" }));
    fireEvent.click(screen.getByTestId("browser-new-btn"));
    expect(screen.getByTestId("browser-new-platform-千问")).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.queryByTestId("browser-new-name")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("browser-create-btn"));
    expect(createSpy).toHaveBeenCalledWith("千问", "");
    expect(screen.queryByTestId("browser-new-dialog")).not.toBeInTheDocument();
  });

  it("renders empty-state create CTA when pool has no accounts", () => {
    const createSpy = vi.spyOn(ipc.Browser, "createBrowser").mockResolvedValue(undefined);
    useAppStore.setState({
      browser: makeBrowserState({ rows: [] }),
    });

    renderPage(<BrowserPage />);

    expect(screen.queryByTestId("browser-account-table")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("browser-empty-new-btn"));
    expect(screen.getByTestId("browser-new-dialog")).toBeInTheDocument();
    expect(screen.queryByTestId("browser-new-name")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("browser-create-btn"));
    expect(createSpy).toHaveBeenCalledWith("豆包", "");
  });

  it("supports quick, batch, stop, and rename-dialog account username sync", async () => {
    const syncSpy = vi.spyOn(ipc.Browser, "syncAccountUsernames").mockResolvedValue(undefined);
    const stopSpy = vi.spyOn(ipc.Browser, "stopSyncAccountUsernames").mockResolvedValue(undefined);

    useAppStore.setState({
      browser: makeBrowserState({
        rows: [
          makeBrowserRow({ id: 1, platform: "豆包", name: "老豆包" }),
          makeBrowserRow({ id: 2, platform: "千问", name: "老千问" }),
        ],
      }),
    });

    const { rerender } = renderPage(<BrowserPage />);

    // Quick sync button syncs all currently visible rows
    fireEvent.click(screen.getByTestId("browser-quick-sync-names-btn"));
    expect(syncSpy).toHaveBeenCalledWith([1, 2]);

    // Rename dialog also provides a single-account re-fetch button
    fireEvent.click(screen.getByRole("button", { name: "老豆包" }));
    expect(screen.getByTestId("browser-rename-dialog")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("browser-rename-sync-btn"));
    expect(syncSpy).toHaveBeenCalledWith([1]);
    expect(screen.queryByTestId("browser-rename-dialog")).not.toBeInTheDocument();

    // Checked rows show batch sync button
    act(() => {
      useAppStore.setState({
        browser: makeBrowserState({
          checkedCount: 1,
          rows: [
            makeBrowserRow({ id: 1, platform: "豆包", name: "老豆包", checked: false }),
            makeBrowserRow({ id: 2, platform: "千问", name: "老千问", checked: true }),
          ],
        }),
      });
    });
    rerender(<BrowserPage />);
    fireEvent.click(screen.getByTestId("browser-batch-sync-names-btn"));
    expect(syncSpy).toHaveBeenCalledWith([2]);

    // Active syncing state displays stop button and row pending text
    act(() => {
      useAppStore.setState({
        browser: makeBrowserState({
          batchActive: true,
          batchKind: "syncing",
          batchText: "正在同步用户名：0/1（已更新 0）",
          rows: [
            makeBrowserRow({
              id: 2,
              platform: "千问",
              name: "老千问",
              pending: true,
              pendingOp: "syncing",
            }),
          ],
        }),
      });
    });
    rerender(<BrowserPage />);
    expect(screen.getByText("同步中")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("browser-stop-sync-names-btn"));
    expect(stopSpy).toHaveBeenCalledTimes(1);
  });

  it("prompts with a warning dialog before deleting an account", () => {
    const actionSpy = vi.spyOn(ipc.Browser, "browserRowAction").mockResolvedValue(undefined);

    useAppStore.setState({
      browser: makeBrowserState({
        rows: [
          makeBrowserRow({ id: 1, platform: "豆包", name: "主力号", isRunning: true }),
        ],
      }),
    });

    renderPage(<BrowserPage />);

    // Clicking delete must open warning confirmation dialog without deleting immediately
    fireEvent.click(screen.getByTestId("browser-delete-btn-1"));
    expect(actionSpy).not.toHaveBeenCalled();

    const dialog = screen.getByTestId("browser-delete-dialog");
    expect(dialog).toBeInTheDocument();
    expect(dialog).toHaveTextContent("豆包 账号「主力号」");
    expect(dialog).toHaveTextContent("清理本地浏览器数据与登录状态");
    expect(dialog).toHaveTextContent("确认删除将立即停止窗口");

    // Canceling or Escape dismisses without calling backend delete
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.queryByTestId("browser-delete-dialog")).not.toBeInTheDocument();
    expect(actionSpy).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("browser-delete-btn-1"));
    expect(screen.getByTestId("browser-delete-dialog")).toBeInTheDocument();
    act(() => {
      expect(dispatchTopEscape()).toBe(true);
    });
    expect(screen.queryByTestId("browser-delete-dialog")).not.toBeInTheDocument();
    expect(actionSpy).not.toHaveBeenCalled();

    // Confirming triggers delete IPC and closes dialog
    fireEvent.click(screen.getByTestId("browser-delete-btn-1"));
    fireEvent.click(screen.getByTestId("browser-delete-confirm-btn"));
    expect(actionSpy).toHaveBeenCalledWith(1, "delete");
    expect(screen.queryByTestId("browser-delete-dialog")).not.toBeInTheDocument();
  });

  it("renders icon-only view button in the actions column rather than the account column", () => {
    const drawerSpy = vi.spyOn(ipc.Browser, "openBrowserDrawer").mockResolvedValue(undefined);
    useAppStore.setState({
      browser: makeBrowserState({
        rows: [makeBrowserRow({ id: 3, platform: "豆包", name: "视窗号" })],
      }),
    });

    renderPage(<BrowserPage />);

    const rowEl = screen.getByTestId("browser-row-3");
    const cells = rowEl.querySelectorAll("td");
    const accountCell = cells[2];
    const actionsCell = cells[cells.length - 1];
    const viewBtn = screen.getByTestId("browser-view-btn-3");

    expect(accountCell).not.toContainElement(viewBtn);
    expect(actionsCell).toContainElement(viewBtn);
    expect(viewBtn).toHaveTextContent("");
    expect(screen.queryByText("可视")).not.toBeInTheDocument();

    fireEvent.click(viewBtn);
    expect(drawerSpy).toHaveBeenCalledWith(3);
  });

  it("renders enable/disable switch in the leftmost column instead of action text button", () => {
    const actionSpy = vi.spyOn(ipc.Browser, "browserRowAction").mockResolvedValue(undefined);
    useAppStore.setState({
      browser: makeBrowserState({
        rows: [
          makeBrowserRow({ id: 1, platform: "豆包", name: "正常号", disabled: false }),
          makeBrowserRow({ id: 2, platform: "千问", name: "停用号", disabled: true }),
        ],
      }),
    });

    renderPage(<BrowserPage />);

    const row1Cells = screen.getByTestId("browser-row-1").querySelectorAll("td");
    const row2Cells = screen.getByTestId("browser-row-2").querySelectorAll("td");
    const toggle1 = screen.getByTestId("browser-toggle-1");
    const toggle2 = screen.getByTestId("browser-toggle-2");

    expect(row1Cells[0].querySelector('[role="checkbox"]')).toBeInTheDocument();
    expect(row1Cells[1]).toContainElement(toggle1);
    expect(row2Cells[1]).toContainElement(toggle2);
    expect(toggle1).toHaveAttribute("aria-checked", "true");
    expect(toggle2).toHaveAttribute("aria-checked", "false");
    expect(screen.queryByRole("button", { name: "禁用" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "启用" })).not.toBeInTheDocument();

    fireEvent.click(toggle1);
    expect(actionSpy).toHaveBeenCalledWith(1, "toggleDisabled");
    fireEvent.click(toggle2);
    expect(actionSpy).toHaveBeenCalledWith(2, "toggleDisabled");
  });

  it("shows 打开/打开登录 when closed and 关闭 when running", () => {
    const actionSpy = vi.spyOn(ipc.Browser, "browserRowAction").mockResolvedValue(undefined);
    useAppStore.setState({
      browser: makeBrowserState({
        rows: [
          makeBrowserRow({ id: 1, name: "已登未开", isLoggedIn: true, isRunning: false }),
          makeBrowserRow({ id: 2, name: "未登未开", isLoggedIn: false, isRunning: false }),
          makeBrowserRow({ id: 3, name: "已开窗口", isLoggedIn: true, isRunning: true }),
        ],
      }),
    });

    renderPage(<BrowserPage />);

    fireEvent.click(within(screen.getByTestId("browser-row-1")).getByRole("button", { name: "打开" }));
    expect(actionSpy).toHaveBeenCalledWith(1, "launch");

    fireEvent.click(
      within(screen.getByTestId("browser-row-2")).getByRole("button", { name: "打开登录" }),
    );
    expect(actionSpy).toHaveBeenCalledWith(2, "launch");

    fireEvent.click(within(screen.getByTestId("browser-row-3")).getByRole("button", { name: "关闭" }));
    expect(actionSpy).toHaveBeenCalledWith(3, "stop");
  });

  it("supports column sorting across 启用/账号/平台/状态/今日用量/独立代理", () => {
    useAppStore.setState({
      browser: makeBrowserState({
        rows: [
          makeBrowserRow({
            id: 1,
            name: "10",
            platform: "豆包",
            disabled: true,
            health: "已禁用",
            healthTone: "danger",
            todayCount: 12,
            usagePct: 24,
            proxy: "",
          }),
          makeBrowserRow({
            id: 2,
            name: "2",
            platform: "千问",
            disabled: false,
            health: "就绪",
            healthTone: "success",
            todayCount: 0,
            usagePct: 0,
            proxy: "http://127.0.0.1:7890",
          }),
          makeBrowserRow({
            id: 3,
            name: "1",
            platform: "豆包",
            disabled: false,
            health: "运行中",
            healthTone: "success",
            todayCount: 4,
            usagePct: 8,
            proxy: "http://127.0.0.1:7891",
          }),
        ],
      }),
    });

    renderPage(<BrowserPage />);

    const getRowOrder = () =>
      screen
        .getAllByTestId(/^browser-row-\d+$/)
        .map((el) => el.getAttribute("data-testid"));

    const sortPlatformBtn = screen.getByTestId("browser-sort-platform");
    // Default sort is platform ascending (豆包 before 千问, natural account name tie-breaker)
    // and sorting alone must not render a top summary bar or sort chip.
    expect(sortPlatformBtn.closest("th")).toHaveAttribute("aria-sort", "ascending");
    expect(getRowOrder()).toEqual(["browser-row-3", "browser-row-1", "browser-row-2"]);
    expect(screen.queryByTestId("browser-active-filters-bar")).not.toBeInTheDocument();
    expect(screen.queryByTestId("browser-active-sort-chip")).not.toBeInTheDocument();

    // Toggling 平台 switches between desc and asc cleanly without adding a summary bar
    fireEvent.click(sortPlatformBtn);
    expect(sortPlatformBtn.closest("th")).toHaveAttribute("aria-sort", "descending");
    expect(getRowOrder()).toEqual(["browser-row-2", "browser-row-3", "browser-row-1"]);
    expect(screen.queryByTestId("browser-active-filters-bar")).not.toBeInTheDocument();

    fireEvent.click(sortPlatformBtn);
    expect(sortPlatformBtn.closest("th")).toHaveAttribute("aria-sort", "ascending");
    expect(getRowOrder()).toEqual(["browser-row-3", "browser-row-1", "browser-row-2"]);

    // Natural sort by 账号 ("1", "2", "10"), then desc, then back to default platform asc
    const sortAccountBtn = screen.getByTestId("browser-sort-account");
    fireEvent.click(sortAccountBtn);
    expect(sortAccountBtn.closest("th")).toHaveAttribute("aria-sort", "ascending");
    expect(getRowOrder()).toEqual(["browser-row-3", "browser-row-2", "browser-row-1"]);
    expect(screen.queryByTestId("browser-active-filters-bar")).not.toBeInTheDocument();

    fireEvent.click(sortAccountBtn);
    expect(sortAccountBtn.closest("th")).toHaveAttribute("aria-sort", "descending");
    expect(getRowOrder()).toEqual(["browser-row-1", "browser-row-2", "browser-row-3"]);

    fireEvent.click(sortAccountBtn);
    expect(sortAccountBtn.closest("th")).toHaveAttribute("aria-sort", "none");
    expect(sortPlatformBtn.closest("th")).toHaveAttribute("aria-sort", "ascending");
    expect(getRowOrder()).toEqual(["browser-row-3", "browser-row-1", "browser-row-2"]);

    // Sort by 今日用量 asc -> desc
    const sortUsageBtn = screen.getByTestId("browser-sort-usage");
    fireEvent.click(sortUsageBtn);
    expect(getRowOrder()).toEqual(["browser-row-2", "browser-row-3", "browser-row-1"]);
    fireEvent.click(sortUsageBtn);
    expect(getRowOrder()).toEqual(["browser-row-1", "browser-row-3", "browser-row-2"]);

    // Sort by 启用 (enabled first in asc, tie-broken by account name "1" then "2")
    fireEvent.click(screen.getByTestId("browser-sort-enabled"));
    expect(getRowOrder()).toEqual(["browser-row-3", "browser-row-2", "browser-row-1"]);

    // Sort by 状态 (运行中 -> 就绪 -> 已禁用)
    fireEvent.click(screen.getByTestId("browser-sort-health"));
    expect(getRowOrder()).toEqual(["browser-row-3", "browser-row-2", "browser-row-1"]);

    // Sort by 独立代理 (custom proxies sorted before global), no sort chip shown
    fireEvent.click(screen.getByTestId("browser-sort-proxy"));
    expect(getRowOrder()).toEqual(["browser-row-2", "browser-row-3", "browser-row-1"]);
    expect(screen.queryByTestId("browser-active-filters-bar")).not.toBeInTheDocument();
    expect(screen.queryByTestId("browser-active-sort-chip")).not.toBeInTheDocument();

    // Clearing column sort from popover restores default platform ascending order
    fireEvent.click(screen.getByTestId("browser-filter-btn-proxy"));
    fireEvent.click(screen.getByTestId("browser-col-reset-proxy"));
    expect(sortPlatformBtn.closest("th")).toHaveAttribute("aria-sort", "ascending");
    expect(getRowOrder()).toEqual(["browser-row-3", "browser-row-1", "browser-row-2"]);
  });

  it("supports column filtering across columns, syncs platform filter, scopes select-all, and resets empty view", () => {
    const toggleCheckSpy = vi
      .spyOn(ipc.Browser, "toggleBrowserCheck")
      .mockResolvedValue(undefined);
    const toggleAllSpy = vi
      .spyOn(ipc.Browser, "toggleAllBrowserChecks")
      .mockResolvedValue(undefined);

    useAppStore.setState({
      browser: makeBrowserState({
        rows: [
          makeBrowserRow({
            id: 1,
            name: "主力豆包",
            email: "main@example.com",
            platform: "豆包",
            disabled: false,
            health: "就绪",
            todayCount: 4,
            usagePct: 8,
            proxy: "http://127.0.0.1:7890",
          }),
          makeBrowserRow({
            id: 2,
            name: "备用千问",
            platform: "千问",
            disabled: false,
            health: "冷却中",
            healthTone: "warning",
            todayCount: 0,
            usagePct: 0,
            proxy: "",
          }),
          makeBrowserRow({
            id: 3,
            name: "禁用千问",
            platform: "千问",
            disabled: true,
            health: "已禁用",
            healthTone: "danger",
            todayCount: 45,
            usagePct: 90,
            proxy: "",
          }),
        ],
      }),
    });

    renderPage(<BrowserPage />);

    // Filter by 启用 -> 仅已启用
    fireEvent.click(screen.getByTestId("browser-filter-btn-enabled"));
    expect(screen.getByTestId("browser-col-popover-enabled")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("browser-filter-enabled-enabled"));
    expect(screen.queryByTestId("browser-row-3")).not.toBeInTheDocument();
    expect(screen.getByTestId("browser-row-1")).toBeInTheDocument();
    expect(screen.getByTestId("browser-row-2")).toBeInTheDocument();

    // Escape closes popover at priority 35
    act(() => {
      expect(dispatchTopEscape()).toBe(true);
    });
    expect(screen.queryByTestId("browser-col-popover-enabled")).not.toBeInTheDocument();

    // Header select-all with active filter toggles only visible filtered rows (1 and 2)
    fireEvent.click(screen.getByTestId("browser-select-all"));
    expect(toggleAllSpy).not.toHaveBeenCalled();
    expect(toggleCheckSpy).toHaveBeenCalledTimes(2);
    expect(toggleCheckSpy).toHaveBeenCalledWith(1, true);
    expect(toggleCheckSpy).toHaveBeenCalledWith(2, true);

    // Filter by 平台 column checkbox syncs with segmented tabs
    fireEvent.click(screen.getByTestId("browser-filter-btn-platform"));
    const doubaoBox = within(screen.getByTestId("browser-filter-platform-豆包")).getByRole(
      "checkbox",
    );
    const qwenIntlBox = within(screen.getByTestId("browser-filter-platform-千问国际")).getByRole(
      "checkbox",
    );
    fireEvent.click(doubaoBox); // uncheck 豆包 -> 千问 & 千问国际 remain checked (still "全部")
    const segTabs = screen.getByTestId("browser-platform-filter");
    expect(within(segTabs).getByRole("tab", { name: "全部" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    fireEvent.click(qwenIntlBox); // uncheck 千问国际 -> only 千问 remains checked
    expect(within(segTabs).getByRole("tab", { name: "千问" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.queryByTestId("browser-row-1")).not.toBeInTheDocument();
    expect(screen.getByTestId("browser-row-2")).toBeInTheDocument();

    // Reset all filters
    fireEvent.click(screen.getByTestId("browser-reset-filters-btn"));
    expect(screen.getByTestId("browser-row-1")).toBeInTheDocument();
    expect(screen.getByTestId("browser-row-2")).toBeInTheDocument();
    expect(screen.getByTestId("browser-row-3")).toBeInTheDocument();

    // Filter by 今日用量 (high >= 80%) and 独立代理 (custom) -> empty state with reset CTA
    fireEvent.click(screen.getByTestId("browser-filter-btn-usage"));
    fireEvent.click(screen.getByTestId("browser-filter-usage-high"));
    expect(screen.getByTestId("browser-row-3")).toBeInTheDocument();
    expect(screen.queryByTestId("browser-row-1")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("browser-filter-btn-proxy"));
    fireEvent.click(screen.getByTestId("browser-filter-proxy-custom"));
    expect(screen.queryByTestId("browser-row-3")).not.toBeInTheDocument();
    // Table headers stay mounted in filtered empty state
    expect(screen.getByTestId("browser-account-table")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("browser-empty-reset-filters-btn"));
    expect(screen.getByTestId("browser-row-1")).toBeInTheDocument();
    expect(screen.getByTestId("browser-row-2")).toBeInTheDocument();
    expect(screen.getByTestId("browser-row-3")).toBeInTheDocument();

    // Filter by 账号 keyword & 状态
    fireEvent.click(screen.getByTestId("browser-filter-btn-account"));
    fireEvent.change(screen.getByTestId("browser-filter-account-input"), {
      target: { value: "main@example" },
    });
    expect(screen.getByTestId("browser-row-1")).toBeInTheDocument();
    expect(screen.queryByTestId("browser-row-2")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("browser-col-reset-account"));
    expect(screen.getByTestId("browser-row-2")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("browser-filter-btn-health"));
    const readyCheckbox = within(screen.getByTestId("browser-filter-health-就绪")).getByRole(
      "checkbox",
    );
    fireEvent.click(readyCheckbox);
    expect(screen.queryByTestId("browser-row-1")).not.toBeInTheDocument();
    expect(screen.getByTestId("browser-row-2")).toBeInTheDocument();
    expect(screen.getByTestId("browser-row-3")).toBeInTheDocument();
  });
});

describe("PromptsPage", () => {
  it("filters templates by all five categories (including 裂变/通用) and renders newline suffix preview", () => {
    useAppStore.setState({
      prompts: makePromptsState({
        items: [
          ...makePromptsState().items,
          {
            id: "user_2",
            name: "多角度裂变词",
            label: "多角度裂变词",
            kind: "fission",
            kindLbl: "裂变",
            text: "重构视角与场景，保持主体辨识度",
            builtin: false,
          },
          {
            id: "user_3",
            name: "通用质感词",
            label: "通用质感词",
            kind: "",
            kindLbl: "通用",
            text: "画面干净通透",
            builtin: false,
          },
        ],
      }),
    });

    renderPage(<PromptsPage />);

    expect(screen.getByTestId("prompts-filter-count")).toHaveTextContent("4 个");
    const combinedPreview = screen.getByTestId("prompts-combined-preview");
    expect(combinedPreview).toHaveTextContent("纯白背景，柔和棚拍光影 4K 超清无水印");
    expect(combinedPreview.textContent).toBe("纯白背景，柔和棚拍光影\n4K 超清无水印");

    // Filter to 洗图 only
    fireEvent.click(screen.getByTestId("prompts-kind-filter-洗图"));
    expect(screen.getByTestId("prompts-filter-count")).toHaveTextContent("1 个");
    expect(screen.getByTestId("prompt-item-doubao_wash")).toBeInTheDocument();
    expect(screen.queryByTestId("prompt-item-user_1")).not.toBeInTheDocument();

    // Filter to 裂变 & 通用
    fireEvent.click(screen.getByTestId("prompts-kind-filter-裂变"));
    expect(screen.getByTestId("prompts-filter-count")).toHaveTextContent("1 个");
    expect(screen.getByTestId("prompt-item-user_2")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("prompts-kind-filter-通用"));
    expect(screen.getByTestId("prompts-filter-count")).toHaveTextContent("1 个");
    expect(screen.getByTestId("prompt-item-user_3")).toBeInTheDocument();
  });

  it("routes apply-to-workbench by template kind (生图->TAB_GEN, 洗图->TAB_WASH, 裂变->TAB_FISSION)", async () => {
    vi.spyOn(ipc, "isMyGoRuntime").mockReturnValue(true);
    const setTabSpy = vi.spyOn(ipc.Core, "setTab").mockResolvedValue(undefined);
    const getShellSpy = vi
      .spyOn(ipc.Core, "getShellState")
      .mockImplementation(async () => useAppStore.getState().shell);
    const getWbSpy = vi
      .spyOn(ipc.Workbench, "getWorkbenchState")
      .mockResolvedValue({} as never);
    const setWbFieldSpy = vi
      .spyOn(ipc.Workbench, "setWorkbenchField")
      .mockResolvedValue(undefined);

    useAppStore.setState({
      prompts: makePromptsState(),
    });

    const { rerender } = renderPage(<PromptsPage />);

    // 1) 生图 -> TAB_GEN
    expect(screen.getByTestId("prompts-apply-btn")).toHaveTextContent("填入自由生图");
    const editorBody = screen.getByTestId("prompts-edit-body") as HTMLTextAreaElement;
    fireEvent.change(editorBody, { target: { value: "极简陶瓷质感产品图" } });
    fireEvent.click(screen.getByTestId("prompts-apply-btn"));

    await waitFor(() => {
      expect(setTabSpy).toHaveBeenCalledWith(TAB_GEN);
      expect(setWbFieldSpy).toHaveBeenCalledWith("prompts", "极简陶瓷质感产品图");
    });
    expect(getShellSpy).toHaveBeenCalled();
    expect(getWbSpy).toHaveBeenCalled();

    // 2) 洗图 -> TAB_WASH
    act(() => {
      useAppStore.setState({
        prompts: makePromptsState({
          selectedId: "doubao_wash",
          selectedName: "洗图",
          selectedKind: "洗图",
          selectedText: "保留主体并重绘为二维手绘风格",
          selectedIsBln: true,
        }),
      });
    });
    rerender(<PromptsPage />);

    expect(screen.getByTestId("prompts-apply-btn")).toHaveTextContent("填入批量洗图");
    expect(screen.getByTestId("prompts-combined-preview")).toHaveTextContent(
      "「洗图」模式发送时不附加全局后缀",
    );
    fireEvent.click(screen.getByTestId("prompts-apply-btn"));

    await waitFor(() => {
      expect(setTabSpy).toHaveBeenCalledWith(TAB_WASH);
      expect(setWbFieldSpy).toHaveBeenCalledWith("prompts", "保留主体并重绘为二维手绘风格");
    });

    // 3) 裂变 -> TAB_FISSION
    act(() => {
      useAppStore.setState({
        prompts: makePromptsState({
          selectedId: "user_fission",
          selectedName: "裂变模板",
          selectedKind: "裂变",
          selectedText: "重构镜头与构图",
          selectedIsBln: false,
        }),
      });
    });
    rerender(<PromptsPage />);

    expect(screen.getByTestId("prompts-apply-btn")).toHaveTextContent("填入裂变图片");
    fireEvent.click(screen.getByTestId("prompts-apply-btn"));

    await waitFor(() => {
      expect(setTabSpy).toHaveBeenCalledWith(TAB_FISSION);
      expect(setWbFieldSpy).toHaveBeenCalledWith("prompts", "重构镜头与构图");
    });
  });

  it("supports structured reader view, dirty/builtin copy saving, and two-step delete", () => {
    const saveSpy = vi.spyOn(ipc.Prompts, "savePromptEdits").mockResolvedValue(undefined);
    const deleteSpy = vi.spyOn(ipc.Prompts, "deleteUserPrompt").mockResolvedValue(undefined);

    useAppStore.setState({
      prompts: makePromptsState({
        selectedId: "doubao_wash",
        selectedName: "洗图",
        selectedKind: "洗图",
        selectedText: "总原则。\n\n无框优先：去掉外围画框与帷幕。\n\n二维优先：改画成二维手绘插画。",
        selectedIsBln: true,
      }),
    });

    const { rerender } = renderPage(<PromptsPage />);

    // Clean builtin disables save until edited
    const saveBtn = screen.getByTestId("prompts-save-btn");
    expect(saveBtn).toHaveTextContent("另存副本");
    expect(saveBtn).toBeDisabled();

    // Switch to 分段阅读 and inspect parsed sections
    fireEvent.click(screen.getByTestId("prompts-view-reader"));
    const reader = screen.getByTestId("prompts-reader-view");
    expect(reader).toHaveTextContent("无框优先");
    expect(reader).toHaveTextContent("二维优先");

    // Back to 编辑, modify body -> dirty badge lights up & save becomes enabled
    fireEvent.click(screen.getByTestId("prompts-view-edit"));
    fireEvent.change(screen.getByTestId("prompts-edit-body"), {
      target: { value: "修改后的洗图指令" },
    });
    expect(screen.getByTestId("prompts-dirty-badge")).toHaveTextContent("未保存");
    expect(screen.getByTestId("prompts-save-btn")).toBeEnabled();
    fireEvent.click(screen.getByTestId("prompts-save-btn"));
    expect(saveSpy).toHaveBeenCalledWith("洗图", "洗图", "修改后的洗图指令");

    // Switch to user prompt and verify 2-step delete confirmation
    act(() => {
      useAppStore.setState({
        prompts: makePromptsState(),
      });
    });
    rerender(<PromptsPage />);

    fireEvent.click(screen.getByTestId("prompts-delete-btn"));
    expect(deleteSpy).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("prompts-delete-confirm-btn"));
    expect(deleteSpy).toHaveBeenCalledTimes(1);
  });

  it("opens new template modal, closes via priority-40 Escape stack, and creates new prompt", () => {
    const createSpy = vi.spyOn(ipc.Prompts, "createNewPrompt").mockResolvedValue(undefined);

    useAppStore.setState({
      prompts: makePromptsState(),
    });

    renderPage(<PromptsPage />);

    fireEvent.click(screen.getByTestId("prompts-new-btn"));
    expect(screen.getByTestId("prompts-new-dialog")).toBeInTheDocument();

    // Priority-40 Escape handler closes dialog cleanly
    act(() => {
      expect(dispatchTopEscape()).toBe(true);
    });
    expect(screen.queryByTestId("prompts-new-dialog")).not.toBeInTheDocument();

    // Reopen and create
    fireEvent.click(screen.getByTestId("prompts-new-btn"));
    fireEvent.change(screen.getByTestId("prompts-new-name"), {
      target: { value: "新品裂变词" },
    });
    fireEvent.change(screen.getByTestId("prompts-new-body"), {
      target: { value: "多角度商业摄影展示" },
    });
    fireEvent.click(screen.getByTestId("prompts-create-confirm"));

    expect(createSpy).toHaveBeenCalledWith("新品裂变词", "生图", "多角度商业摄影展示");
  });
  it("opens clear history dialog for single/batch accounts and supports stopping active clear", async () => {
    const clearSpy = vi.spyOn(ipc.Browser, "clearBrowserHistory").mockResolvedValue(undefined);
    const stopClearSpy = vi.spyOn(ipc.Browser, "stopClearBrowserHistory").mockResolvedValue(undefined);

    useAppStore.setState({
      browser: makeBrowserState({
        checkedCount: 2,
        rows: [
          makeBrowserRow({ id: 1, platform: "豆包", name: "豆包主号", checked: true }),
          makeBrowserRow({ id: 2, platform: "千问", name: "千问副号", checked: true }),
        ],
      }),
    });

    const { rerender } = renderPage(<BrowserPage />);

    // 单账号清理历史（选保留 5 条，默认开启极速模式）
    fireEvent.click(screen.getByTestId("browser-clear-history-btn-1"));
    expect(screen.getByTestId("browser-clear-history-dialog")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("clear-history-keep-5"));
    fireEvent.click(screen.getByTestId("browser-clear-history-confirm-btn"));
    await waitFor(() => {
      expect(clearSpy).toHaveBeenCalledWith([1], 5, true);
    });

    // 批量清理历史（关闭极速模式，保留 0 条）
    fireEvent.click(screen.getByTestId("browser-batch-clear-history-btn"));
    expect(screen.getByTestId("browser-clear-history-dialog")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("clear-history-fast-switch"));
    fireEvent.click(screen.getByTestId("browser-clear-history-confirm-btn"));
    await waitFor(() => {
      expect(clearSpy).toHaveBeenCalledWith([1, 2], 0, false);
    });

    // 未勾选时顶栏展示「一键清历史」，一键清理当前列表全部账号
    useAppStore.setState({
      browser: makeBrowserState({
        checkedCount: 0,
        rows: [
          makeBrowserRow({ id: 1, platform: "豆包", name: "豆包主号", checked: false }),
          makeBrowserRow({ id: 2, platform: "千问", name: "千问副号", checked: false }),
        ],
      }),
    });
    rerender(<BrowserPage />);
    fireEvent.click(screen.getByTestId("browser-quick-clear-history-btn"));
    expect(screen.getByTestId("browser-clear-history-dialog")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("browser-clear-history-confirm-btn"));
    await waitFor(() => {
      expect(clearSpy).toHaveBeenCalledWith([1, 2], 0, true);
    });

    // 清理进行中时展示停止按钮
    useAppStore.setState({
      browser: makeBrowserState({
        batchActive: true,
        batchKind: "clearing",
        batchText: "正在极速清理历史：1/2（已删 3 条）",
      }),
    });
    rerender(<BrowserPage />);
    fireEvent.click(screen.getByTestId("browser-stop-clear-history-btn"));
    await waitFor(() => {
      expect(stopClearSpy).toHaveBeenCalledTimes(1);
    });
  });
});

describe("SettingsPage", () => {
  it("switches theme via settings-theme-tabs and syncs dark class + Core.setDarkTheme", async () => {
    vi.spyOn(ipc, "isMyGoRuntime").mockReturnValue(true);
    const darkSpy = vi.spyOn(ipc.Core, "setDarkTheme").mockResolvedValue(undefined);
    vi.spyOn(ipc.Core, "getShellState").mockResolvedValue({
      ...useAppStore.getState().shell,
      dark: true,
    });

    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1280 });
    useAppStore.setState({
      settings: makeSettingsState({ tab: 0 }),
    });

    renderPage(<SettingsPage />);

    // >= 1120px mounts left rail instead of toolbar segmented tabs
    expect(screen.getByTestId("settings-rail")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-tabs")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "深色模式" }));
    await waitFor(() => {
      expect(darkSpy).toHaveBeenCalledWith(true);
    });
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("renders 9 built-in dot matrix effects in Tab 0, selects default, and syncs to store & generation", async () => {
    const setFieldSpy = vi
      .spyOn(ipc.Settings, "setSettingField")
      .mockResolvedValue(undefined);

    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1280 });
    useAppStore.setState({
      settings: makeSettingsState({ tab: 0, dotMatrixEffect: "grain" }),
    });

    renderPage(<SettingsPage />);

    const card = screen.getByTestId("settings-dotmatrix-card");
    expect(card).toBeInTheDocument();
    expect(screen.getByTestId("settings-dotmatrix-active-badge")).toHaveTextContent("默认：静态噪粒");

    const radios = screen.getAllByRole("radio");
    expect(radios).toHaveLength(9);
    expect(screen.getByTestId("settings-dotmatrix-option-grain")).toHaveAttribute(
      "aria-checked",
      "true",
    );

    // Select "六角雪花" (snowflakes) as default
    fireEvent.click(screen.getByTestId("settings-dotmatrix-option-snowflakes"));
    expect(setFieldSpy).toHaveBeenCalledWith("dotMatrixEffect", "snowflakes");
    expect(useAppStore.getState().shell.dotMatrixEffect).toBe("snowflakes");
    expect(screen.getByTestId("settings-dotmatrix-option-snowflakes")).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByTestId("settings-dotmatrix-active-badge")).toHaveTextContent("默认：六角雪花");

    await waitFor(() => {
      expect(screen.getByTestId("settings-autosave-status")).toHaveTextContent("已自动保存");
    });

    // Settings cards directly loop animations without a manual preview button or verbose description text
    expect(screen.queryByTestId("settings-dotmatrix-preview-btn")).not.toBeInTheDocument();
    expect(within(card).queryByText("微粒细闪呼吸 · 半调生长入场")).not.toBeInTheDocument();
  });

  it("toggles autoDeleteHistory switch in Tab 1", () => {
    const setFieldSpy = vi
      .spyOn(ipc.Settings, "setSettingField")
      .mockResolvedValue(undefined);
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1280 });
    useAppStore.setState({
      settings: makeSettingsState({ tab: 1, autoDeleteHistory: false }),
    });

    renderPage(<SettingsPage />);

    const sw = screen.getByTestId("settings-auto-delete-history-switch");
    fireEvent.click(sw);
    expect(setFieldSpy).toHaveBeenCalledWith("autoDeleteHistory", "true");

    // Independent daily cap input for 千问国际
    const qwenIntlCap = screen.getByLabelText("千问国际单日上限") as HTMLInputElement;
    expect(qwenIntlCap.value).toBe("100");
    fireEvent.change(qwenIntlCap, { target: { value: "42" } });
    fireEvent.blur(qwenIntlCap);
    expect(setFieldSpy).toHaveBeenCalledWith("dailyCapQwenIntl", "42");
  });

  it("cycles all nine presets from dots through decoded artwork to a held image and back", async () => {
    vi.useFakeTimers();
    try {
      vi.spyOn(imageLoader, "loadAndDecodeImage").mockImplementation(async src => ({
        src, image: new Image(), naturalWidth: 640, naturalHeight: 300, corsSafe: true,
      }));
      useAppStore.setState({ settings: makeSettingsState({ tab: 0 }) });
      renderPage(<SettingsPage />);

      const cards = within(screen.getByTestId("settings-dotmatrix-card")).getAllByRole("radio");
      expect(cards).toHaveLength(9);
      const checkPhase = (phase: string) => {
        for (const card of cards) {
          expect(card.querySelector("[data-phase]")).toHaveAttribute("data-phase", phase);
          if (phase === "loading") expect(card.querySelector("img")).toBeNull();
          else expect(card.querySelector("img")).toHaveAttribute("alt", "雪山与河谷");
        }
      };
      const advance = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });

      checkPhase("loading");
      await advance(1750);
      checkPhase("revealing");
      await advance(1900);
      checkPhase("settled");
      await advance(1450);
      checkPhase("loading");
      await advance(1750);
      checkPhase("revealing");
    } finally {
      vi.useRealTimers();
    }
  });

  it("uses exact Go contract values for sendStrategy (顺序循环/随机循环) and auto-saves edits without a Save button", async () => {
    const setFieldSpy = vi
      .spyOn(ipc.Settings, "setSettingField")
      .mockResolvedValue(undefined);

    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1000 });
    useAppStore.setState({
      settings: makeSettingsState({ tab: 1, sendStrategy: "顺序循环" }),
    });

    renderPage(<SettingsPage />);

    // Manual save button is removed in favor of auto-save
    expect(screen.queryByTestId("settings-save-btn")).not.toBeInTheDocument();
    // Concurrency controls moved to Workbench AccountLanes header
    expect(screen.queryByRole("switch", { name: "自动并发" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("单平台并发上限")).not.toBeInTheDocument();

    // < 1120px mounts toolbar segmented tabs instead of left rail
    expect(screen.getByTestId("settings-tabs")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-rail")).not.toBeInTheDocument();

    const strategyTrigger = screen.getByTestId("settings-strategy-select");
    expect(strategyTrigger).toHaveTextContent("顺序循环");

    // Open Radix Select and pick 随机循环
    fireEvent.keyDown(strategyTrigger, { key: "ArrowDown" });
    const randomOpt = await screen.findByRole("option", { name: "随机循环" });
    fireEvent.click(randomOpt);

    expect(setFieldSpy).toHaveBeenCalledWith("sendStrategy", "随机循环");

    // Editing a debounced field auto-saves on blur / tab switch without a manual Save button
    const riskyInput = screen.getByLabelText("风险过滤词") as HTMLInputElement;
    fireEvent.change(riskyInput, { target: { value: "新违禁词" } });
    fireEvent.blur(riskyInput);

    expect(setFieldSpy).toHaveBeenCalledWith("riskyWords", "新违禁词");
    await waitFor(() => {
      expect(screen.getByTestId("settings-autosave-status")).toHaveTextContent("已自动保存");
    });
  });

  it("uses exact Go contract values for captchaBackend (free/custom/ikunocr) and shows conditional fields", async () => {
    const setFieldSpy = vi
      .spyOn(ipc.Settings, "setSettingField")
      .mockResolvedValue(undefined);

    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1280 });
    useAppStore.setState({
      settings: makeSettingsState({ tab: 2, captchaBackend: "free" }),
    });

    const { rerender } = renderPage(<SettingsPage />);

    const captchaTrigger = screen.getByTestId("settings-captcha-select");
    expect(captchaTrigger).toHaveTextContent("内置离线识别");
    expect(screen.queryByTestId("settings-captcha-custom-fields")).not.toBeInTheDocument();
    expect(screen.queryByTestId("settings-captcha-ikunocr-fields")).not.toBeInTheDocument();

    // Pick custom visual API
    fireEvent.keyDown(captchaTrigger, { key: "ArrowDown" });
    const customOpt = await screen.findByRole("option", { name: "自定义视觉 API" });
    fireEvent.click(customOpt);
    expect(setFieldSpy).toHaveBeenCalledWith("captchaBackend", "custom");
    await waitFor(() => {
      expect(screen.getByTestId("settings-autosave-status")).toHaveTextContent("已自动保存");
    });

    // Simulate Go echoing captchaBackend = "custom"
    act(() => {
      useAppStore.setState({
        settings: makeSettingsState({ tab: 2, captchaBackend: "custom" }),
      });
    });
    rerender(<SettingsPage />);
    expect(screen.getByTestId("settings-captcha-custom-fields")).toBeInTheDocument();

    // Simulate Go echoing captchaBackend = "ikunocr"
    act(() => {
      useAppStore.setState({
        settings: makeSettingsState({ tab: 2, captchaBackend: "ikunocr" }),
      });
    });
    rerender(<SettingsPage />);
    expect(screen.getByTestId("settings-captcha-ikunocr-fields")).toBeInTheDocument();
  });

  it("renders Fingerprint runtime install controls on Browser & Network tab", () => {
    const installSpy = vi.spyOn(ipc.Browser, "installBrowserRuntime").mockResolvedValue(undefined);

    useAppStore.setState({
      settings: makeSettingsState({ tab: 2 }),
      browser: makeBrowserState({
        ready: false,
        busy: false,
        badgeLabel: "未安装",
        badgeTone: "warning",
      }),
    });

    renderPage(<SettingsPage />);

    expect(screen.getByTestId("settings-browser-runtime-card")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-browser-bridge-card")).not.toBeInTheDocument();
    expect(screen.getAllByText("Fingerprint").length).toBeGreaterThan(0);

    const installBtn = screen.getByTestId("browser-install-btn");
    expect(installBtn).toHaveTextContent("下载安装 Fingerprint");
    fireEvent.click(installBtn);
    expect(installSpy).toHaveBeenCalledTimes(1);
  });

  it("triggers update actions and renders release notes on About & Updates tab", () => {
    const updateSpy = vi.spyOn(ipc.Core, "updateAction").mockResolvedValue(undefined);

    useAppStore.setState((st) => ({
      shell: {
        ...st.shell,
        update: {
          ...st.shell.update,
          visible: true,
          version: "0.2.0",
          current: "0.1.38",
          notes: "## 0.2.0\n- 全新 MyGo 界面",
        },
      },
      settings: makeSettingsState({ tab: 3 }),
    }));

    renderPage(<SettingsPage />);

    expect(screen.getByText(/全新 MyGo 界面/)).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("settings-check-update-btn"));
    expect(updateSpy).toHaveBeenCalledWith("check");

    fireEvent.click(screen.getByTestId("settings-install-update-btn"));
    expect(updateSpy).toHaveBeenCalledWith("install");
  });

  it("renders rich update progress phases (probing, downloading, stalled, verifying, deferred) and supports cancel", () => {
    const updateSpy = vi.spyOn(ipc.Core, "updateAction").mockResolvedValue(undefined);

    useAppStore.setState((st) => ({
      shell: {
        ...st.shell,
        update: {
          ...st.shell.update,
          visible: true,
          version: "0.1.41",
          current: "0.1.40",
          downloading: true,
          phase: "probing",
          fraction: -1,
          progressText: "正在测速选择最快的下载源...",
        },
      },
      settings: makeSettingsState({ tab: 3 }),
    }));

    const { rerender } = renderPage(<SettingsPage />);
    expect(screen.getByTestId("settings-update-progress-card")).toBeInTheDocument();
    expect(screen.getByTestId("settings-update-percent")).toHaveTextContent("测速中");
    expect(screen.getByTestId("settings-update-progress")).toHaveAttribute(
      "data-indeterminate",
      "true",
    );
    fireEvent.click(screen.getByTestId("settings-cancel-update-btn"));
    expect(updateSpy).toHaveBeenCalledWith("cancel");

    // Determinate downloading with speed, ETA, bytes, and mirror badge
    act(() => {
      useAppStore.setState((st) => ({
        shell: {
          ...st.shell,
          update: {
            ...st.shell.update,
            downloading: true,
            phase: "downloading",
            fraction: 0.426,
            doneBytes: Math.round(42.6 * 1024 * 1024),
            totalBytes: 100 * 1024 * 1024,
            speedBps: 8 * 1024 * 1024,
            etaSeconds: 8,
            source: "edgeone.gh-proxy.org",
            assetName: "tulai.Setup.0.1.41.amd64.exe",
          },
        },
      }));
    });
    rerender(<SettingsPage />);
    expect(screen.getByTestId("settings-update-percent")).toHaveTextContent("42.6%");
    expect(screen.getByTestId("settings-update-progress")).toHaveAttribute("aria-valuenow", "43");
    expect(screen.getByTestId("settings-update-source")).toHaveTextContent("edgeone.gh-proxy.org");
    const meta = screen.getByTestId("settings-update-meta");
    expect(meta).toHaveTextContent("tulai.Setup.0.1.41.amd64.exe");
    expect(meta).toHaveTextContent("42.6 MB / 100.0 MB");
    expect(meta).toHaveTextContent("8.0 MB/s");
    expect(meta).toHaveTextContent("剩余 8 秒");

    // Stalled download (speed decayed to 0 before completion)
    act(() => {
      useAppStore.setState((st) => ({
        shell: {
          ...st.shell,
          update: {
            ...st.shell.update,
            speedBps: 0,
            etaSeconds: 0,
          },
        },
      }));
    });
    rerender(<SettingsPage />);
    expect(screen.getByTestId("settings-update-meta")).toHaveTextContent("等待响应 / 切换线路中…");

    // Verifying integrity (100%, emerald, cancel hidden)
    act(() => {
      useAppStore.setState((st) => ({
        shell: {
          ...st.shell,
          update: {
            ...st.shell.update,
            phase: "verifying",
            fraction: 1,
            doneBytes: 100 * 1024 * 1024,
            totalBytes: 100 * 1024 * 1024,
          },
        },
      }));
    });
    rerender(<SettingsPage />);
    expect(screen.getByTestId("settings-update-percent")).toHaveTextContent("100%");
    expect(screen.queryByTestId("settings-cancel-update-btn")).not.toBeInTheDocument();

    // Deferred state when tasks are running
    act(() => {
      useAppStore.setState((st) => ({
        shell: {
          ...st.shell,
          update: {
            ...st.shell.update,
            downloading: false,
            deferred: true,
            phase: "deferred",
            fraction: -1,
            progressText: "任务进行中，结束后将自动更新",
          },
        },
      }));
    });
    rerender(<SettingsPage />);
    expect(screen.getByTestId("settings-update-progress-card")).toHaveTextContent("等待当前任务结束");
    expect(screen.getByTestId("settings-cancel-update-btn")).toHaveTextContent("取消等待");
  });

  it("renders custom update mirror input when custom mirror is configured", async () => {
    const setFieldSpy = vi.spyOn(ipc.Settings, "setSettingField").mockResolvedValue(undefined);
    vi.spyOn(ipc.Settings, "getSettingsState").mockResolvedValue(
      makeSettingsState({ tab: 3, updateMirror: "https://my-mirror.example/" }),
    );

    useAppStore.setState({
      settings: makeSettingsState({ tab: 3, updateMirror: "https://my-mirror.example/" }),
    });

    renderPage(<SettingsPage />);

    expect(screen.getByTestId("settings-custom-mirror-row")).toBeInTheDocument();
    const input = screen.getByLabelText("自定义更新镜像地址") as HTMLInputElement;
    expect(input.value).toBe("https://my-mirror.example/");

    fireEvent.change(input, { target: { value: "https://fast.example/" } });
    fireEvent.blur(input);
    await waitFor(() => {
      expect(setFieldSpy).toHaveBeenCalledWith("updateMirror", "https://fast.example/");
    });
  });

  it("swallows runtime-unavailable IPC rejections outside MyGo without unhandled rejections", async () => {
    const unhandled: unknown[] = [];
    const onUnhandled = (e: PromiseRejectionEvent) => unhandled.push(e.reason);
    window.addEventListener("unhandledrejection", onUnhandled);

    const err = new Error("mygo: runtime not found");
    vi.spyOn(ipc.Settings, "setSettingField").mockRejectedValue(err);
    vi.spyOn(ipc.Browser, "refreshBrowsers").mockRejectedValue(err);
    vi.spyOn(ipc.Prompts, "selectPrompt").mockRejectedValue(err);

    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1280 });
    useAppStore.setState({
      settings: makeSettingsState({ tab: 0 }),
      browser: makeBrowserState(),
      prompts: makePromptsState(),
    });

    try {
      renderPage(<SettingsPage />);
      fireEvent.click(screen.getByTestId("settings-rail-tab-1"));
      fireEvent.click(screen.getByTestId("settings-rail-tab-2"));

      cleanup();
      renderPage(<BrowserPage />);
      fireEvent.click(screen.getByRole("button", { name: "刷新状态" }));

      cleanup();
      renderPage(<PromptsPage />);
      fireEvent.click(screen.getByTestId("prompt-item-doubao_wash"));

      await new Promise((r) => setTimeout(r, 20));
      expect(unhandled).toEqual([]);
    } finally {
      window.removeEventListener("unhandledrejection", onUnhandled);
    }
  });
});

describe("WizardView", () => {
  it("shows blocked Fingerprint notice and formats fractional/indeterminate download progress", () => {
    vi.spyOn(ipc, "isMyGoRuntime").mockReturnValue(true);
    vi.spyOn(ipc.Core, "wizardAction").mockResolvedValue(undefined);
    vi.spyOn(ipc.Core, "wizardState").mockResolvedValue(useAppStore.getState().wizard);
    vi.spyOn(ipc.Core, "getShellState").mockResolvedValue(useAppStore.getState().shell);

    useAppStore.setState({
      wizard: {
        ...useAppStore.getState().wizard,
        on: true,
        step: "browser",
        planBackend: "Fingerprint",
        planKind: "Fingerprint",
        planTitle: "当前架构不支持 Fingerprint",
        planBlocked: "Fingerprint (fingerprint-chromium) 目前支持 Windows x64、macOS Apple Silicon 和 Linux x64。",
        planCanNext: false,
        planNeedsDl: false,
        runtimeBusy: true,
        runtimePercent: 0.42,
        runtimeStatus: "正在下载 Fingerprint 浏览器内核…",
      },
    });

    const { rerender } = renderPage(<WizardView />);
    expect(screen.getByText("Fingerprint")).toBeInTheDocument();
    expect(
      screen.getByText("Fingerprint (fingerprint-chromium) 目前支持 Windows x64、macOS Apple Silicon 和 Linux x64。"),
    ).toBeInTheDocument();
    expect(screen.getByText("正在下载 Fingerprint 浏览器内核… · 42%")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "下一步" })).toBeDisabled();

    // Negative runtimePercent is indeterminate and must not display "-1%".
    act(() => {
      useAppStore.setState({
        wizard: {
          ...useAppStore.getState().wizard,
          runtimePercent: -1,
        },
      });
    });
    rerender(<WizardView />);
    expect(screen.getByText("正在下载 Fingerprint 浏览器内核…")).toBeInTheDocument();
    expect(screen.queryByText(/-1%/)).not.toBeInTheDocument();
  });
});
