import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { ReactElement } from "react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui";
import { classifyDroppedSheetPaths } from "@/components/download/downloadFormat";
import { dispatchTopEscape } from "@/lib/shortcuts";
import { DedupPage } from "@/pages/DedupPage";
import { DownloadPage } from "@/pages/DownloadPage";
import { RenamePage } from "@/pages/RenamePage";

function renderPage(ui: ReactElement) {
  const result = render(<TooltipProvider>{ui}</TooltipProvider>);
  return {
    ...result,
    rerenderPage: (next: ReactElement) =>
      result.rerender(<TooltipProvider>{next}</TooltipProvider>),
  };
}
import {
  resetStoreForTests,
  useAppStore,
  type DedupStateDTO,
  type DownloadStateDTO,
  type RenameStateDTO,
} from "@/store";

const mockCore = vi.hoisted(() => ({
  revealPath: vi.fn().mockResolvedValue(undefined),
  openPath: vi.fn().mockResolvedValue(undefined),
  getShellState: vi.fn(),
  getLogsState: vi.fn(),
  wizardState: vi.fn(),
}));

const mockDownload = vi.hoisted(() => ({
  getDownloadState: vi.fn(),
  setDownloadFilter: vi.fn().mockResolvedValue(undefined),
  setDownloadField: vi.fn().mockResolvedValue(undefined),
  submitDownloadURLs: vi.fn().mockResolvedValue(undefined),
  downloadRowAction: vi.fn().mockResolvedValue(undefined),
  toggleDownloadCheck: vi.fn().mockResolvedValue(undefined),
  downloadPickDir: vi.fn().mockResolvedValue(undefined),
  downloadOpenDir: vi.fn().mockResolvedValue(undefined),
  openDownloadBrowser: vi.fn().mockResolvedValue(undefined),
  openDownloadGuide: vi.fn().mockResolvedValue(undefined),
  pickDownloadSheet: vi.fn().mockResolvedValue(undefined),
  importDownloadSheet: vi.fn().mockResolvedValue(undefined),
  downloadImportAction: vi.fn().mockResolvedValue(undefined),
  downloadPauseResumeAll: vi.fn().mockResolvedValue(undefined),
  downloadClearTrash: vi.fn().mockResolvedValue(undefined),
}));

const mockDedup = vi.hoisted(() => ({
  getDedupState: vi.fn(),
  dedupAddFiles: vi.fn().mockResolvedValue(undefined),
  dedupAddFolder: vi.fn().mockResolvedValue(undefined),
  dedupClear: vi.fn().mockResolvedValue(undefined),
  dedupIngestPaths: vi.fn().mockResolvedValue(undefined),
  dedupOpenOutDir: vi.fn().mockResolvedValue(undefined),
  dedupPickOutDir: vi.fn().mockResolvedValue(undefined),
  dedupSetFlatColor: vi.fn().mockResolvedValue(undefined),
  dedupSetLevel: vi.fn().mockResolvedValue(undefined),
  dedupSetMore: vi.fn().mockResolvedValue(undefined),
  dedupStart: vi.fn().mockResolvedValue(undefined),
}));

const mockRename = vi.hoisted(() => ({
  getRenameState: vi.fn(),
  renamePickFiles: vi.fn().mockResolvedValue(undefined),
  renamePickFolder: vi.fn().mockResolvedValue(undefined),
  renameIngestPaths: vi.fn().mockResolvedValue(undefined),
  renameClear: vi.fn().mockResolvedValue(undefined),
  renameRemoveAt: vi.fn().mockResolvedValue(undefined),
  renameSetField: vi.fn().mockResolvedValue(undefined),
  renameSetRule: vi.fn().mockResolvedValue(undefined),
  renameAddRule: vi.fn().mockResolvedValue(undefined),
  renameRemoveRule: vi.fn().mockResolvedValue(undefined),
  renameSetDraft: vi.fn().mockResolvedValue(undefined),
  renamePasteLines: vi.fn().mockResolvedValue(undefined),
  renameMoveRow: vi.fn().mockResolvedValue(undefined),
  renameSuggestAI: vi.fn().mockResolvedValue(undefined),
  renameSaveAIModel: vi.fn().mockResolvedValue(undefined),
  renameApply: vi.fn().mockResolvedValue(undefined),
  renameUndo: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@/lib/ipc", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/ipc")>();
  return {
    ...actual,
    Core: { ...actual.Core, ...mockCore },
    Download: mockDownload,
    Dedup: mockDedup,
    Rename: mockRename,
  };
});

beforeAll(() => {
  // Enable @tanstack/react-virtual rendering inside jsdom
  class MockResizeObserver {
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
          } as ResizeObserverEntry,
        ],
        this as unknown as ResizeObserver,
      );
    }
    unobserve() {}
    disconnect() {}
  }

  vi.stubGlobal("ResizeObserver", MockResizeObserver);

  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
    width: 800,
    height: 600,
    top: 0,
    left: 0,
    bottom: 600,
    right: 800,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  });

  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    value: 600,
  });
  Object.defineProperty(HTMLElement.prototype, "clientHeight", {
    configurable: true,
    value: 600,
  });
});

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  cleanup();
  resetStoreForTests();
});

function makeDownloadState(overrides: Partial<DownloadStateDTO> = {}): DownloadStateDTO {
  return {
    filter: 0,
    search: "",
    searchOn: false,
    groupBySite: false,
    selectedId: 0,
    composerOn: false,
    composerErr: "",
    urls: "https://example.com/a.png",
    dir: "/tmp/dl",
    resolvedDir: "/tmp/dl",
    browserMode: false,
    browserBusy: false,
    browserOpen: false,
    browserUrl: "",
    browserErr: "",
    browserReady: true,
    browserInstallBusy: false,
    browserSupported: true,
    guideOn: false,
    guideSearch: "",
    guideGroups: [],
    import: {
      open: false,
      fileName: "",
      sheets: [],
      sheet: "",
      columns: [],
      folders: false,
      imageCount: 0,
      invalid: 0,
      err: "",
    },
    importNotice: "",
    depth: 1,
    minKb: 10,
    maxKb: 5000,
    minW: 400,
    maxW: 4096,
    minH: 400,
    maxH: 4096,
    tasks: [
      {
        id: 101,
        url: "https://shop.example.com/img1.jpg",
        name: "img1.jpg",
        site: "天猫",
        dir: "/tmp/dl",
        phase: "下载中",
        tone: "running",
        written: 51200,
        total: 102400,
        speed: 10240,
        progressText: "50 KB / 100 KB",
        fraction: 0.5,
        err: "",
        path: "",
        checked: false,
        isPage: false,
        imagesDone: 0,
        imagesTotal: 0,
        imagesFiltered: 0,
        rendered: false,
        terminal: false,
      },
      {
        id: 102,
        url: "https://shop.example.com/img2.jpg",
        name: "img2.jpg",
        site: "京东",
        dir: "/tmp/dl",
        phase: "失败",
        tone: "danger",
        written: 0,
        total: 0,
        speed: 0,
        progressText: "",
        fraction: 0,
        err: "连接超时",
        path: "/tmp/dl/img2.jpg",
        checked: true,
        isPage: false,
        imagesDone: 0,
        imagesTotal: 0,
        imagesFiltered: 0,
        rendered: false,
        terminal: true,
      },
    ],
    countAll: 2,
    countOpen: 1,
    countDone: 0,
    countCanceled: 1,
    totalSpeed: 10240,
    activeCount: 1,
    queuedCount: 0,
    pausedCount: 0,
    failedCount: 1,
    ...overrides,
  };
}

function makeDedupState(overrides: Partial<DedupStateDTO> = {}): DedupStateDTO {
  return {
    items: [
      { path: "/img/1.png", name: "1.png", fileUrl: "tulaimedia://localhost/file?t=t1&v=1" },
      { path: "/img/2.png", name: "2.png", fileUrl: "tulaimedia://localhost/file?t=t2&v=1" },
      { path: "/img/3.png", name: "3.png", fileUrl: "tulaimedia://localhost/file?t=t3&v=1" },
    ],
    itemCount: 3,
    level: 1,
    levelHint: "均衡模式：适合常规洗图与电商去重",
    threshold: 8,
    outDir: "/out/dedup",
    resolvedOut: "/out/dedup",
    more: false,
    flatColorOk: false,
    busy: false,
    scanned: 3,
    total: 3,
    note: "",
    err: "",
    hasReport: true,
    unique: 2,
    duplicates: 1,
    groupCount: 1,
    savedDir: "/out/dedup/dupes",
    groups: [
      {
        index: 1,
        distance: 3,
        keep: {
          path: "/img/1.png",
          name: "1.png",
          fileUrl: "tulaimedia://localhost/file?t=t1&v=1",
          width: 1024,
          height: 1024,
          size: "240 KB",
          meta: "1024×1024 · 240 KB",
        },
        dupes: [
          {
            path: "/img/2.png",
            name: "2.png",
            fileUrl: "tulaimedia://localhost/file?t=t2&v=1",
            width: 800,
            height: 800,
            size: "180 KB",
            meta: "800×800 · 180 KB",
          },
        ],
      },
    ],
    ...overrides,
  };
}

function makeRenameState(overrides: Partial<RenameStateDTO> = {}): RenameStateDTO {
  return {
    mode: 0,
    busy: false,
    includeExt: false,
    insertKind: "",
    position: "开头",
    index: 1,
    text: "",
    prefix: "",
    suffix: "",
    numFormat: "",
    numStart: 1,
    numWidth: 3,
    numPrefix: "sku_",
    numSuffix: "",
    sortBy: "名称升序",
    aiPrompt: "简洁中文主图名",
    aiSettings: false,
    aiUrl: "https://api.example.com/v1",
    aiKey: "sk-secret",
    aiModel: "gpt-4o-mini",
    rules: [
      { index: 0, find: "old", with: "new", template: 0, count: 1, offset: 0, text: "" },
    ],
    rows: [
      {
        index: 0,
        path: "/f/old_1.png",
        oldName: "old_1.png",
        newName: "new_1.png",
        draft: "draft_1",
        changed: true,
      },
      {
        index: 1,
        path: "/f/old_2.png",
        oldName: "old_2.png",
        newName: "old_2.png",
        draft: "draft_2",
        changed: false,
      },
    ],
    changedCount: 1,
    canUndo: true,
    planErr: "",
    note: "",
    err: "",
    templateNames: [
      "普通查找",
      "前 N 位",
      "后 N 位",
      "从第 X 位开始的 N 位",
      "某字符之后的内容",
      "某字符之前的内容",
      "某字符之后的 N 位",
      "某字符之前的 N 位",
    ],
    ...overrides,
  };
}

describe("DownloadPage", () => {
  it("renders Xunlei-style sidebar categories, composer modal with depth/size filters, and task row/inspector actions", () => {
    useAppStore.setState({ download: makeDownloadState() });
    renderPage(<DownloadPage />);

    expect(screen.getByTestId("dl-sidebar")).toBeInTheDocument();
    expect(screen.getByTestId("dl-queue")).toBeInTheDocument();
    expect(screen.getByTestId("download-task-list")).toBeInTheDocument();

    // Xunlei categories match Go dlFilter enum: 0=正在下载, 1=已完成, 2=回收站, 3=全部任务
    expect(screen.getByTestId("dl-filter-0")).toHaveTextContent("正在下载");
    expect(screen.getByTestId("dl-filter-1")).toHaveTextContent("已完成");
    expect(screen.getByTestId("dl-filter-2")).toHaveTextContent("回收站");
    expect(screen.getByTestId("dl-filter-3")).toHaveTextContent("全部任务");
    fireEvent.click(screen.getByTestId("dl-filter-1"));
    expect(mockDownload.setDownloadFilter).toHaveBeenCalledWith(1);

    // Group by site toggle
    fireEvent.click(screen.getByTestId("dl-group-by-site"));
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("groupBySite", "true");

    // Open "+ 新建任务" composer dialog
    fireEvent.click(screen.getByTestId("dl-new-task-btn"));
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("composerOn", "true");
    expect(screen.getByTestId("dl-composer-dialog")).toBeInTheDocument();

    // Browser mode switch inside composer dialog
    fireEvent.click(screen.getByRole("switch", { name: "使用指纹浏览器登录抓取" }));
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("browserMode", "true");

    // Expand filter grid and verify all 7 fields including maxH
    fireEvent.click(screen.getByTestId("dl-filters-toggle"));
    expect(screen.getByTestId("dl-filters-grid")).toBeInTheDocument();
    const maxHInput = screen.getByLabelText("最大高度") as HTMLInputElement;
    expect(maxHInput.value).toBe("4096");
    fireEvent.change(maxHInput, { target: { value: "2048" } });
    fireEvent.blur(maxHInput);
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("maxH", "2048");

    // Submit URLs directly uses draft value
    const urlsArea = screen.getByTestId("dl-urls-input") as HTMLTextAreaElement;
    fireEvent.change(urlsArea, { target: { value: "https://example.com/new.png" } });
    fireEvent.click(screen.getByRole("button", { name: "立即下载" }));
    expect(mockDownload.submitDownloadURLs).toHaveBeenCalledWith("https://example.com/new.png");

    // Escape closes composer dialog (priority 40)
    act(() => {
      dispatchTopEscape();
    });
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("composerOn", "false");

    // Toolbar queue actions
    fireEvent.click(screen.getByRole("button", { name: "全部暂停" }));
    expect(mockDownload.downloadPauseResumeAll).toHaveBeenCalledWith(true);
    fireEvent.click(screen.getByRole("button", { name: "全部开始" }));
    expect(mockDownload.downloadPauseResumeAll).toHaveBeenCalledWith(false);
    fireEvent.click(screen.getByRole("button", { name: "清理已结束" }));
    expect(mockDownload.downloadClearTrash).toHaveBeenCalledWith("prune");

    // Task row telemetry (speed + ETA) and contextual buttons
    const row101 = screen.getByTestId("dl-task-101");
    const row102 = screen.getByTestId("dl-task-102");
    expect(row101).toBeInTheDocument();
    expect(row101).toHaveTextContent("10.0 KB/s");
    expect(row101).toHaveTextContent("剩余 5秒");
    expect(row102).toBeInTheDocument();

    fireEvent.click(within(row101).getByRole("button", { name: "暂停" }));
    expect(mockDownload.downloadRowAction).toHaveBeenCalledWith(101, "pause");
    fireEvent.click(within(row102).getByRole("button", { name: "重试" }));
    expect(mockDownload.downloadRowAction).toHaveBeenCalledWith(102, "retry");
    fireEvent.click(within(row102).getByRole("button", { name: "定位" }));
    expect(mockDownload.downloadRowAction).toHaveBeenCalledWith(102, "reveal");

    // Batch bar appears for checked task (102 is checked) and supports batch retry
    const batchBar = screen.getByTestId("dl-batch-bar");
    expect(batchBar).toHaveTextContent("已选 1");
    fireEvent.click(within(batchBar).getByRole("button", { name: "重试" }));
    expect(mockDownload.downloadRowAction).toHaveBeenCalledWith(102, "retry");

    // Clicking task row opens inspector and Escape (priority 10) closes it
    fireEvent.click(row101);
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("selectedId", "101");
    expect(screen.getByTestId("dl-inspector")).toBeInTheDocument();
    act(() => {
      dispatchTopEscape();
    });
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("selectedId", "0");
  });

  it("renders site-grouped task headers and trash clear action on trash tab", () => {
    useAppStore.setState({
      download: makeDownloadState({
        filter: 2,
        groupBySite: true,
      }),
    });
    renderPage(<DownloadPage />);

    expect(screen.getByTestId("dl-site-group-天猫")).toBeInTheDocument();
    expect(screen.getByTestId("dl-site-group-京东")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "清空回收站" }));
    expect(mockDownload.downloadClearTrash).toHaveBeenCalledWith("clear");
  });

  it("renders Import modal with multi-sheet selector, column checkboxes, and Escape priority", () => {
    useAppStore.setState({
      download: makeDownloadState({
        import: {
          open: true,
          fileName: "products.xlsx",
          sheets: ["Sheet1", "Sheet2"],
          sheet: "Sheet1",
          columns: [
            { index: 0, name: "主图链接", sample: "https://cdn.example.com/1.jpg", isUrl: true, isName: false },
            { index: 1, name: "商品编码", sample: "SKU-001", isUrl: false, isName: true },
          ],
          folders: false,
          imageCount: 12,
          invalid: 1,
          err: "",
        },
      }),
    });

    renderPage(<DownloadPage />);
    expect(screen.getByTestId("dl-import-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("dl-import-sheet-select")).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("按列创建子目录"));
    expect(mockDownload.downloadImportAction).toHaveBeenCalledWith("toggleFolders", "", 0);

    const urlCheckboxes = screen.getAllByRole("checkbox", { name: "网址列" });
    fireEvent.click(urlCheckboxes[1]);
    expect(mockDownload.downloadImportAction).toHaveBeenCalledWith("toggleURL", "", 1);

    const nameCheckboxes = screen.getAllByRole("checkbox", { name: "命名列" });
    fireEvent.click(nameCheckboxes[0]);
    expect(mockDownload.downloadImportAction).toHaveBeenCalledWith("toggleName", "", 0);

    fireEvent.click(screen.getByRole("button", { name: "确认导入" }));
    expect(mockDownload.downloadImportAction).toHaveBeenCalledWith("submit", "", 0);

    // Priority 40 escape handler closes import dialog
    act(() => {
      dispatchTopEscape();
    });
    expect(mockDownload.downloadImportAction).toHaveBeenCalledWith("close", "", 0);
  });

  it("renders Supported Sites guide modal with site icons, unmerged rows, Fingerprint filter, and Escape close", () => {
    useAppStore.setState({
      download: makeDownloadState({
        guideOn: true,
        guideGroups: [
          {
            title: "普通网页和图片搜索",
            entries: [
              {
                site: "图片直链、普通网页",
                pages: "图片直链或网页中可访问的图片；动态网页可尝试开启托管指纹浏览器模式。",
                icons: "",
              },
            ],
          },
          {
            title: "电商商品与店铺",
            entries: [
              {
                site: "淘宝",
                pages: "商品详情的主图；可通过托管指纹浏览器登录。",
                icons: "taobao.com",
              },
              {
                site: "天猫",
                pages: "商品详情的主图；可通过托管指纹浏览器登录。",
                icons: "tmall.com",
              },
            ],
          },
          {
            title: "摄影、作品与相册",
            entries: [
              {
                site: "Pinterest",
                pages: "单图和画板；画板可能只取得部分图片。",
                icons: "pinterest.com",
              },
            ],
          },
        ],
      }),
    });

    renderPage(<DownloadPage />);
    const dialog = screen.getByTestId("dl-guide-dialog");
    expect(dialog).toBeInTheDocument();
    expect(within(dialog).getByText("电商商品与店铺")).toBeInTheDocument();
    expect(within(dialog).getByText("淘宝")).toBeInTheDocument();
    expect(within(dialog).getByText("天猫")).toBeInTheDocument();
    expect(within(dialog).getByText("taobao.com")).toBeInTheDocument();
    expect(within(dialog).getByText("tmall.com")).toBeInTheDocument();
    expect(within(dialog).getByText("Pinterest")).toBeInTheDocument();

    // Named sites render bundled PNG/SVG favicons; generic webpages render fallback globe icon
    expect(within(dialog).getByAltText("淘宝")).toHaveAttribute("src");
    expect(within(dialog).getByAltText("天猫")).toHaveAttribute("src");
    expect(within(dialog).getByAltText("Pinterest")).toHaveAttribute("src");
    expect(within(dialog).getAllByTestId("dl-guide-site-fallback")).toHaveLength(1);

    // Filter down to sites requiring Fingerprint browser
    fireEvent.click(within(dialog).getByTestId("dl-guide-fingerprint-filter"));
    expect(within(dialog).getByText("淘宝")).toBeInTheDocument();
    expect(within(dialog).getByText("天猫")).toBeInTheDocument();
    expect(within(dialog).queryByText("Pinterest")).not.toBeInTheDocument();

    act(() => {
      dispatchTopEscape();
    });
    expect(mockDownload.openDownloadGuide).toHaveBeenCalledWith(false);
  });

  it("shows Fingerprint runtime guidance in composer when uninstalled or open", () => {
    useAppStore.setState({
      download: makeDownloadState({
        composerOn: true,
        browserReady: false,
        browserSupported: true,
        browserInstallBusy: false,
      }),
    });

    const { rerenderPage } = renderPage(<DownloadPage />);
    expect(
      screen.getByText("指纹浏览器 (Fingerprint) 尚未安装，需登录或动态网页抓取时请先安装。"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "前往设置安装" })).toBeInTheDocument();

    act(() => {
      useAppStore.setState({
        download: makeDownloadState({
          composerOn: true,
          browserReady: true,
          browserSupported: true,
          browserOpen: true,
          browserUrl: "https://example.com/album",
        }),
      });
    });
    rerenderPage(<DownloadPage />);
    expect(within(screen.getByTestId("dl-composer-dialog")).getByText("指纹浏览器运行中")).toBeInTheDocument();
    expect(screen.getByText("https://example.com/album")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "在指纹浏览器打开" })).toBeInTheDocument();

    act(() => {
      useAppStore.setState({
        download: makeDownloadState({
          composerOn: false,
          browserReady: true,
          browserSupported: true,
          browserOpen: true,
          browserUrl: "https://example.com/album",
        }),
      });
    });
    rerenderPage(<DownloadPage />);
    expect(screen.getByRole("button", { name: "指纹浏览器已打开" })).toBeInTheDocument();
    expect(screen.getByText("指纹浏览器运行中")).toBeInTheDocument();
  });

  it("uses container-responsive queue columns, accurate failed page copy, and immediate inspector/composer close", () => {
    useAppStore.setState({
      download: makeDownloadState({
        selectedId: 201,
        composerOn: true,
        countAll: 1,
        countOpen: 1,
        countDone: 0,
        countCanceled: 0,
        tasks: [
          {
            id: 201,
            url: "https://www.temu.com/search_result.html?search_key=%E6%95%B0%E5%AD%97%E6%B2%B9%E7%94%BB",
            name: "Temu",
            site: "temu.com",
            dir: "/tmp/dl",
            phase: "失败",
            tone: "danger",
            written: 0,
            total: 0,
            speed: 0,
            progressText: "",
            fraction: 0,
            err: "没有找到已加载的 TEMU 搜索商品；请完成验证或等待页面加载",
            path: "",
            checked: false,
            isPage: true,
            imagesDone: 0,
            imagesTotal: 0,
            imagesFiltered: 0,
            rendered: true,
            terminal: true,
          },
        ],
      }),
    });

    renderPage(<DownloadPage />);

    // Queue is a CSS container and headers stay single-line without vertical character wrapping
    expect(screen.getByTestId("dl-queue")).toHaveClass("@container");
    const tableHeader = screen.getByTestId("dl-table-header");
    expect(tableHeader).toHaveTextContent("任务名称 / 进度");
    for (const child of Array.from(tableHeader.children)) {
      expect(child).toHaveClass("whitespace-nowrap");
    }

    // Failed page task with 0 images must not falsely show "页面解析中"
    const row = screen.getByTestId("dl-task-201");
    expect(row).toHaveTextContent("未获取到图片");
    expect(row).not.toHaveTextContent("页面解析中");
    const inspector = screen.getByTestId("dl-inspector");
    expect(inspector).toHaveClass("md:w-[252px]");
    expect(inspector).toHaveTextContent("未获取到图片");
    expect(inspector).not.toHaveTextContent("页面解析中");
    expect(within(inspector).queryByText("本地路径")).not.toBeInTheDocument();

    // Closing composer when server composerOn=true immediately hides dialog optimistically
    expect(screen.getByTestId("dl-composer-dialog")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.queryByTestId("dl-composer-dialog")).not.toBeInTheDocument();

    // Closing inspector when server selectedId=201 immediately hides inspector optimistically
    fireEvent.click(within(inspector).getByRole("button", { name: "关闭任务详情" }));
    expect(screen.queryByTestId("dl-inspector")).not.toBeInTheDocument();
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("selectedId", "0");
  });

  it("validates composer URLs (dedupe/clean, Ctrl+Enter, browser multi-URL) and blocks invalid min>max filters", () => {
    useAppStore.setState({
      download: makeDownloadState({
        composerOn: true,
        urls: "",
        browserMode: true,
        composerErr: "请先打开指纹浏览器并访问目标网页",
        minKb: 0,
        maxKb: 0,
        minW: 0,
        maxW: 0,
        minH: 0,
        maxH: 0,
      }),
    });

    renderPage(<DownloadPage />);
    const dialog = screen.getByTestId("dl-composer-dialog");
    const urlsArea = within(dialog).getByTestId("dl-urls-input") as HTMLTextAreaElement;

    // Paste duplicate + invalid URLs
    fireEvent.change(urlsArea, {
      target: {
        value:
          "https://a.example.com/1.jpg\nhttps://a.example.com/1.jpg\nnot-a-url\nhttps://b.example.com/2.jpg",
      },
    });
    expect(within(dialog).getByTestId("dl-urls-dup-badge")).toHaveTextContent("重复 1");
    expect(within(dialog).getByTestId("dl-urls-invalid-badge")).toHaveTextContent("无效 1");
    expect(within(dialog).getByTestId("dl-browser-multi-url-warn")).toBeInTheDocument();

    // Clean duplicates and invalid lines
    fireEvent.click(within(dialog).getByTestId("dl-urls-clean-btn"));
    expect(urlsArea.value).toBe("https://a.example.com/1.jpg\nhttps://b.example.com/2.jpg");

    // Clicking "指纹浏览器抓取" dismisses stale composerErr and invokes openDownloadBrowser
    fireEvent.click(within(dialog).getByRole("button", { name: "指纹浏览器抓取" }));
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("dismissComposerErr", "1");
    expect(mockDownload.openDownloadBrowser).toHaveBeenCalledWith(
      "https://a.example.com/1.jpg\nhttps://b.example.com/2.jpg",
    );

    // Invalid filter range minKb > maxKb shows error and blocks submit
    fireEvent.click(within(dialog).getByTestId("dl-filters-toggle"));
    const minKbInput = within(dialog).getByLabelText("最小(KB)") as HTMLInputElement;
    const maxKbInput = within(dialog).getByLabelText("最大(KB)") as HTMLInputElement;
    fireEvent.change(minKbInput, { target: { value: "500" } });
    fireEvent.change(maxKbInput, { target: { value: "100" } });
    expect(within(dialog).getByTestId("dl-filter-errors")).toHaveTextContent(
      "最小体积 (KB) 不能大于最大体积 (KB)",
    );

    mockDownload.submitDownloadURLs.mockClear();
    fireEvent.click(within(dialog).getByRole("button", { name: "立即下载" }));
    expect(mockDownload.submitDownloadURLs).not.toHaveBeenCalled();

    // Reset filters clears invalid state, then editing a valid filter and immediately pressing Ctrl+Enter flushes filter before submit
    fireEvent.click(within(dialog).getByTestId("dl-filters-reset"));
    expect(within(dialog).queryByTestId("dl-filter-errors")).not.toBeInTheDocument();

    mockDownload.setDownloadField.mockClear();
    mockDownload.submitDownloadURLs.mockClear();
    fireEvent.change(minKbInput, { target: { value: "50" } });
    fireEvent.keyDown(urlsArea, { key: "Enter", ctrlKey: true });
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("minKB", "50");
    expect(mockDownload.submitDownloadURLs).toHaveBeenCalledWith(
      "https://a.example.com/1.jpg\nhttps://b.example.com/2.jpg",
    );
    expect(mockDownload.setDownloadField.mock.invocationCallOrder[0]).toBeLessThan(
      mockDownload.submitDownloadURLs.mock.invocationCallOrder[0],
    );
  });

  it("supports guide quick actions, inspector browser recovery, batch cancel, and aligned status bar copy", () => {
    useAppStore.setState({
      download: makeDownloadState({
        selectedId: 301,
        guideOn: true,
        browserErr: "指纹浏览器已关闭，请重新打开",
        countAll: 3,
        countOpen: 2,
        countDone: 1,
        countCanceled: 0,
        guideGroups: [
          {
            title: "电商商品与店铺",
            entries: [
              {
                site: "Temu",
                pages: "商品详情及搜索结果；搜索通过托管指纹浏览器抓取。",
                icons: "temu.com",
              },
            ],
          },
        ],
        tasks: [
          {
            id: 301,
            url: "https://www.temu.com/search_result.html?search_key=art",
            name: "Temu 搜索",
            site: "temu.com",
            dir: "/tmp/dl",
            phase: "失败",
            tone: "danger",
            written: 0,
            total: 0,
            speed: 0,
            progressText: "",
            fraction: 0,
            err: "请先完成页面验证",
            path: "   ",
            checked: false,
            isPage: true,
            imagesDone: 0,
            imagesTotal: 0,
            imagesFiltered: 0,
            rendered: true,
            terminal: true,
          },
          {
            id: 302,
            url: "https://cdn.example.com/pic.jpg",
            name: "pic.jpg",
            site: "example.com",
            dir: "/tmp/dl",
            phase: "下载中",
            tone: "transfer",
            written: 1024,
            total: 2048,
            speed: 512,
            progressText: "1 KB / 2 KB",
            fraction: 0.5,
            err: "",
            path: "",
            checked: true,
            isPage: false,
            imagesDone: 0,
            imagesTotal: 0,
            imagesFiltered: 0,
            rendered: false,
            terminal: false,
          },
        ],
      }),
    });

    renderPage(<DownloadPage />);

    // Guide quick open in Fingerprint browser closes guide and opens https://domain
    const guide = screen.getByTestId("dl-guide-dialog");
    fireEvent.click(within(guide).getByRole("button", { name: "在指纹浏览器打开 Temu" }));
    expect(mockDownload.openDownloadBrowser).toHaveBeenCalledWith("https://temu.com");
    expect(mockDownload.openDownloadGuide).toHaveBeenCalledWith(false);
    expect(screen.queryByTestId("dl-guide-dialog")).not.toBeInTheDocument();

    // Inspector shows recovery open-in-browser + retry for failed rendered task and hides whitespace path
    const inspector = screen.getByTestId("dl-inspector");
    expect(within(inspector).queryByText("本地路径")).not.toBeInTheDocument();
    expect(within(inspector).queryByRole("button", { name: "定位" })).not.toBeInTheDocument();
    fireEvent.click(within(inspector).getByTestId("dl-inspector-open-browser"));
    expect(mockDownload.openDownloadBrowser).toHaveBeenCalledWith(
      "https://www.temu.com/search_result.html?search_key=art",
    );
    fireEvent.click(within(inspector).getByRole("button", { name: "重试" }));
    expect(mockDownload.downloadRowAction).toHaveBeenCalledWith(301, "retry");

    // Batch bar supports "移入回收站" (cancel) for checked non-terminal task 302
    const batchBar = screen.getByTestId("dl-batch-bar");
    fireEvent.click(within(batchBar).getByRole("button", { name: "移入回收站" }));
    expect(mockDownload.downloadRowAction).toHaveBeenCalledWith(302, "cancel");

    // Bottom status bar uses unified "正在下载" label matching sidebar
    expect(screen.getByText("全部 3 · 正在下载 2 · 已完成 1 · 回收站 0")).toBeInTheDocument();

    // Queue inline error can be dismissed optimistically
    const notice = screen.getByTestId("dl-composer-notice");
    expect(notice).toHaveTextContent("指纹浏览器已关闭，请重新打开");
    fireEvent.click(within(notice).getByRole("button", { name: "关闭提示" }));
    expect(screen.queryByTestId("dl-composer-notice")).not.toBeInTheDocument();
    expect(mockDownload.setDownloadField).toHaveBeenCalledWith("dismissBrowserErr", "1");
  });

  it("displays global speed/counts across filtered tabs and classifies dropped spreadsheet paths", () => {
    useAppStore.setState({
      download: makeDownloadState({
        filter: 1, // 已完成 tab with empty task list
        tasks: [],
        countAll: 5,
        countOpen: 4,
        countDone: 1,
        countCanceled: 0,
        totalSpeed: 204800,
        activeCount: 2,
        queuedCount: 1,
        pausedCount: 1,
        failedCount: 0,
      }),
    });

    renderPage(<DownloadPage />);
    expect(screen.getByTestId("dl-global-speed")).toHaveTextContent("200 KB/s");
    expect(screen.getByTestId("dl-global-counts")).toHaveTextContent("下载中 2 · 等待 1 · 暂停 1");
    expect(screen.getByText("总速度 200 KB/s")).toBeInTheDocument();

    const drop = classifyDroppedSheetPaths([
      "/tmp/products.xlsx",
      "/tmp/urls.CSV",
      "/tmp/legacy.xls",
      "/tmp/photo.png",
    ]);
    expect(drop.supported).toEqual(["/tmp/products.xlsx", "/tmp/urls.CSV"]);
    expect(drop.legacyXls).toEqual(["/tmp/legacy.xls"]);
    expect(drop.other).toEqual(["/tmp/photo.png"]);
  });
});

describe("DedupPage", () => {
  it("switches similarity levels, toggles flatColor, and renders report groups with thumb URLs and revealPath", () => {
    useAppStore.setState({ dedup: makeDedupState() });
    renderPage(<DedupPage />);

    const dedupQueue = screen.getByTestId("dedup-queue");
    const dedupReport = screen.getByTestId("dedup-report");
    expect(dedupQueue).toBeInTheDocument();
    expect(dedupReport).toBeInTheDocument();
    expect(dedupQueue.parentElement).toBe(dedupReport.parentElement);
    expect(dedupQueue.parentElement).toHaveClass("flex-col");
    expect(dedupQueue.parentElement).not.toHaveClass("flex-row");

    // Level pills
    fireEvent.click(screen.getByTestId("dedup-level-0"));
    expect(mockDedup.dedupSetLevel).toHaveBeenCalledWith(0);

    // Flat color switch
    fireEvent.click(screen.getByRole("switch", { name: "纯色图不区分颜色" }));
    expect(mockDedup.dedupSetFlatColor).toHaveBeenCalledWith(true);

    // Start dedup enabled when >= 2 images
    const startBtn = screen.getByTestId("dedup-start-btn");
    expect(startBtn).not.toBeDisabled();
    fireEvent.click(startBtn);
    expect(mockDedup.dedupStart).toHaveBeenCalled();

    // Report group renders 1-based group index, Hamming distance, thumb URL, and clickable duplicate reveal
    expect(screen.getByTestId("dedup-group-1")).toBeInTheDocument();
    expect(screen.getByText("相似组 #1")).toBeInTheDocument();
    expect(screen.getByText("视觉差异度 3")).toBeInTheDocument();

    const keepCard = screen.getByTestId("dedup-keep-1");
    const keepImg = keepCard.querySelector("img");
    expect(keepImg?.getAttribute("src")).toContain("w=240");

    fireEvent.click(screen.getByTestId("dedup-dupe-1-0"));
    expect(mockCore.revealPath).toHaveBeenCalledWith("/img/2.png");
  });

  it("disables start button with fewer than 2 images, handles >24 expand toggle, and shows empty states", () => {
    const manyItems = Array.from({ length: 26 }, (_, i) => ({
      path: `/img/item_${i}.png`,
      name: `item_${i}.png`,
      fileUrl: `tulaimedia://localhost/file?t=i${i}&v=1`,
    }));

    useAppStore.setState({
      dedup: makeDedupState({
        items: manyItems,
        itemCount: 26,
        more: false,
        hasReport: false,
        groups: [],
      }),
    });

    const { rerenderPage } = renderPage(<DedupPage />);
    expect(screen.getByText("已就绪，点击「开始去重」")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("dedup-more-btn"));
    expect(mockDedup.dedupSetMore).toHaveBeenCalledWith(true);

    act(() => {
      useAppStore.setState({
        dedup: makeDedupState({
          items: [{ path: "/img/1.png", name: "1.png", fileUrl: "" }],
          itemCount: 1,
          hasReport: true,
          unique: 1,
          duplicates: 0,
          groups: [],
        }),
      });
    });

    rerenderPage(<DedupPage />);
    expect(screen.getByTestId("dedup-start-btn")).toBeDisabled();
    expect(screen.getByText("未发现重复图片")).toBeInTheDocument();
  });
});

describe("RenamePage", () => {
  it("matches uTools 1:1 5-tab layout and supports mode-specific configs", async () => {
    useAppStore.setState({ rename: makeRenameState({ mode: 0 }) });
    const { rerenderPage } = renderPage(<RenamePage />);

    const tabs = screen.getAllByRole("tab").map((el) => el.textContent?.trim());
    expect(tabs).toEqual(["查找替换", "智能重命名", "插入内容", "自动编号", "手动编辑"]);

    const renameConfig = screen.getByTestId("rename-config");
    const renamePreview = screen.getByTestId("rename-preview");
    expect(renameConfig).toBeInTheDocument();
    expect(renamePreview).toBeInTheDocument();
    expect(renameConfig.parentElement).toBe(renamePreview.parentElement);
    expect(renameConfig.parentElement).toHaveClass("flex-col");
    expect(renameConfig.parentElement).not.toHaveClass("flex-row");
    expect(screen.getByText(/共 2 个文件/)).toBeInTheDocument();
    expect(screen.getByText(/将重命名 1 个/)).toBeInTheDocument();

    // Mode 0: 查找替换 (two-column rule row, regex toggle, includeExt, add rule)
    const rule0 = screen.getByTestId("rename-rule-0");
    expect(rule0).toBeInTheDocument();
    expect(rule0).toHaveClass("flex-row");
    fireEvent.click(screen.getByRole("button", { name: "添加替换规则" }));
    expect(mockRename.renameAddRule).toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "切换正则表达式" }));
    expect(mockRename.renameSetRule).toHaveBeenCalledWith(0, "/old/g", "new", "", 0, 1, 0);

    fireEvent.click(screen.getByLabelText("替换文件扩展名"));
    expect(mockRename.renameSetField).toHaveBeenCalledWith("includeExt", "true");

    // Switch to Mode 2: 插入内容
    fireEvent.click(screen.getByTestId("rename-mode-2"));
    expect(mockRename.renameSetField).toHaveBeenCalledWith("mode", "2");

    act(() => {
      useAppStore.setState({
        rename: makeRenameState({ mode: 2, position: "指定位置", index: 3, text: "_promo" }),
      });
    });
    rerenderPage(<RenamePage />);
    expect(screen.getByTestId("rename-insert-index")).toHaveValue("3");
    expect(screen.getByTestId("rename-insert-text")).toHaveValue("_promo");

    fireEvent.click(screen.getByTestId("rename-insert-kind-文件属性"));
    expect(mockRename.renameSetField).toHaveBeenCalledWith("insertKind", "修改时间");
    fireEvent.click(screen.getByTestId("rename-insert-pos-末尾"));
    expect(mockRename.renameSetField).toHaveBeenCalledWith("position", "末尾");

    // Mode 3: 自动编号 (3-column layout + prefix/start/width/suffix + dropdown format)
    act(() => {
      useAppStore.setState({ rename: makeRenameState({ mode: 3, numFormat: "阿拉伯数字" }) });
    });
    rerenderPage(<RenamePage />);
    expect(screen.getByTestId("rename-num-prefix")).toHaveValue("sku_");
    expect(screen.getByTestId("rename-num-start")).toHaveValue("1");
    expect(screen.getByTestId("rename-num-width")).toHaveValue("3");
    expect(screen.getByText("示例：sku_001")).toBeInTheDocument();

    const formatTrigger = screen.getByTestId("rename-num-format-trigger");
    fireEvent.keyDown(formatTrigger, { key: "Enter" });
    const zhLowerItem = await screen.findByTestId("rename-num-format-中文小写");
    fireEvent.click(zhLowerItem);
    expect(mockRename.renameSetField).toHaveBeenCalledWith("numFormat", "中文小写");

    // Mode 4: 智能重命名 with elevated prompt card, collapsible model config & inline editable rows
    act(() => {
      useAppStore.setState({
        rename: makeRenameState({ mode: 4, aiSettings: true, planErr: "文件名冲突" }),
      });
    });
    rerenderPage(<RenamePage />);
    expect(screen.getByTestId("rename-banner")).toHaveTextContent("文件名冲突");
    expect(screen.getByTestId("rename-ai-settings-panel")).toBeInTheDocument();
    expect(screen.getByTestId("rename-ai-url")).toHaveValue("https://api.example.com/v1");
    expect(screen.getByTestId("rename-ai-model")).toHaveValue("gpt-4o-mini");
    expect(screen.getByTestId("rename-draft-0")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("rename-ai-submit-btn"));
    expect(mockRename.renameSuggestAI).toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    expect(mockRename.renameSaveAIModel).toHaveBeenCalledWith(
      "https://api.example.com/v1",
      "sk-secret",
      "gpt-4o-mini",
    );
  });

  it("supports inline CJK IME editing and multi-line paste via Rename.renamePasteLines in mode 1", () => {
    useAppStore.setState({ rename: makeRenameState({ mode: 1 }) });
    renderPage(<RenamePage />);

    expect(screen.getByText(/点击任意新文件名即可修改；粘贴多行文本/)).toBeInTheDocument();
    const draftInput0 = screen.getByTestId("rename-draft-0") as HTMLInputElement;
    expect(draftInput0.value).toBe("draft_1");

    // CJK IME composition defers commit until compositionEnd
    fireEvent.compositionStart(draftInput0);
    fireEvent.change(draftInput0, { target: { value: "zhutu" } });
    expect(mockRename.renameSetDraft).not.toHaveBeenCalled();
    fireEvent.compositionEnd(draftInput0, { target: { value: "主图" } });
    expect(mockRename.renameSetDraft).toHaveBeenCalledWith(0, "主图");

    // Multi-line paste triggers renamePasteLines and prevents single-line input corruption
    fireEvent.paste(draftInput0, {
      clipboardData: {
        getData: () => "商品主图_01\n商品主图_02",
      },
    });
    expect(mockRename.renamePasteLines).toHaveBeenCalledWith(0, "商品主图_01\n商品主图_02");

    // Single-line paste with trailing newline should NOT trigger renamePasteLines
    mockRename.renamePasteLines.mockClear();
    fireEvent.paste(draftInput0, {
      clipboardData: {
        getData: () => "单行名称\n",
      },
    });
    expect(mockRename.renamePasteLines).not.toHaveBeenCalled();

    // Row actions: move down and remove
    const row0 = screen.getByTestId("rename-row-0");
    const downBtn = row0.querySelector('button[aria-label="下移"]') as HTMLButtonElement;
    fireEvent.click(downBtn);
    expect(mockRename.renameMoveRow).toHaveBeenCalledWith(0, 1);

    const removeBtn = row0.querySelector('button[aria-label="移除"]') as HTMLButtonElement;
    fireEvent.click(removeBtn);
    expect(mockRename.renameRemoveAt).toHaveBeenCalledWith(0);

    // Footer actions: Undo and Apply buttons
    fireEvent.click(screen.getByRole("button", { name: "撤销上次" }));
    expect(mockRename.renameUndo).toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("rename-apply-btn"));
    expect(mockRename.renameApply).toHaveBeenCalled();
  });
});
