import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import doubaoMark from "@/assets/providers/doubao-avatar.png";
import qwenMark from "@/assets/providers/qwen-color.svg";
import { HoverPreviewHost, clearHoverPreview } from "@/components/HoverPreview";
import { TooltipProvider } from "@/components/ui";
import { Browser, Core, ImageEdit, Prompts, Settings, Workbench, events } from "@/lib/ipc";
import { dispatchTopEscape } from "@/lib/shortcuts";
import { WorkbenchPage } from "@/pages/WorkbenchPage";
import {
  TAB_BROWSER,
  TAB_FISSION,
  TAB_GEN,
  TAB_IMAGE_EDIT,
  TAB_PROMPTS,
  TAB_WASH,
  refreshTopics,
  resetStoreForTests,
  useAppStore,
  type AccountLaneDTO,
  type FileDropEvent,
  type ResultTileDTO,
  type SourceItemDTO,
  type WorkbenchStateDTO,
} from "@/store";

let mockMyGoActive = false;
const fileDropListeners = new Set<(ev: FileDropEvent) => void>();

vi.mock("mygo-runtime", async (importOriginal) => {
  const actual = await importOriginal<typeof import("mygo-runtime")>();
  return {
    ...actual,
    isMyGo: () => mockMyGoActive,
    onFileDrop: (cb: (ev: FileDropEvent) => void) => {
      fileDropListeners.add(cb);
      return () => {
        fileDropListeners.delete(cb);
      };
    },
  };
});

function makeSource(overrides: Partial<SourceItemDTO> = {}): SourceItemDTO {
  const idx = overrides.index ?? 0;
  return {
    index: idx,
    path: `/tmp/src-${idx}.png`,
    name: `商品图-${idx + 1}.png`,
    fileUrl: `tulaimedia://localhost/file?t=src${idx}&v=1`,
    washed: false,
    washing: false,
    washOutput: "",
    outputUrl: "",
    selected: true,
    ...overrides,
  };
}

function makeLane(overrides: Partial<AccountLaneDTO> = {}): AccountLaneDTO {
  const id = overrides.id ?? 1;
  return {
    id,
    platform: "豆包",
    title: `豆包账号-${id}`,
    state: "空闲",
    action: "",
    usedToday: 3,
    successToday: 3,
    failedToday: 0,
    cap: 50,
    readyInSec: 0,
    cooldownAt: "",
    coolUntil: 0,
    usagePct: 6,
    usageLabel: "",
    usageErr: "",
    job: { taskIndex: 0, prompt: "", sourceUrl: "", status: "", elapsed: "" },
    hasJob: false,
    disabled: false,
    isRunning: true,
    isLoggedIn: true,
    pending: false,
    pendingOp: "",
    engine: "Fingerprint",
    lastError: "",
    ...overrides,
  };
}

function makeTile(overrides: Partial<ResultTileDTO> = {}): ResultTileDTO {
  const idx = overrides.taskIndex ?? 1;
  return {
    key: `tile-${idx}`,
    taskIndex: idx,
    title: `商品-${idx}.png`,
    status: "完成",
    path: `/tmp/out-${idx}.png`,
    fileUrl: `tulaimedia://localhost/file?t=out${idx}&v=2`,
    srcUrl: `tulaimedia://localhost/file?t=in${idx}&v=1`,
    platform: "豆包",
    accountId: 1,
    accountName: "豆包主号",
    engine: "Fingerprint",
    elapsed: "8.4s",
    prompt: "",
    rewritten: false,
    reason: "",
    actionText: "",
    canReplace: true,
    canRewash: true,
    rewashCount: 0,
    ...overrides,
  };
}

function makeWorkbench(overrides: Partial<WorkbenchStateDTO> = {}): WorkbenchStateDTO {
  const inferredPlatform =
    overrides.platform ??
    (overrides.qwenIntl ? "千问国际" : overrides.qianwen ? "千问" : "豆包");
  return {
    mode: "wash",
    page: TAB_WASH,
    batchMode: 0,
    platform: inferredPlatform,
    platforms: ["豆包", "千问", "千问国际"],
    doubao: true,
    qianwen: false,
    qwenIntl: false,
    prompts: "提取主画，纯白背景",
    promptChoice: "",
    promptOptions: ["电商白底", "极简棚拍"],
    promptTemplates: [],
    promptSuffix: "高清细节",
    useSuffix: false,
    washStyle: "纯白电商底",
    washStyleOptions: ["纯白电商底", "透明提取"],
    outputPreset: "1024×1024",
    outputPresets: ["1024×1024", "2048×2048"],
    outputMode: "preset",
    outputRatio: "1:1",
    outputRatios: ["自动", "1:1", "3:4"],
    outputWidth: "",
    outputHeight: "",
    outputCrop: false,
    sources: [],
    sourcesTotal: 0,
    washedCount: 0,
    selectedSource: 0,
    skipImages: false,
    running: false,
    runPage: TAB_WASH,
    progress: 0,
    progText: "",
    canStart: true,
    startLabel: "开始洗图",
    planMissing: [],
    outputNote: "",
    activeRows: [],
    issueRows: [],
    pendingTiles: [],
    doneTiles: [],
    pendingCount: 0,
    doneCount: 0,
    doneTasks: 0,
    failedCount: 0,
    needsCount: 0,
    activeCount: 0,
    totalCount: 0,
    resultsDoneOpen: true,
    stageTab: 0,
    lanes: [makeLane()],
    laneCollapsed: false,
    concurrencyAuto: true,
    concurrencyMax: 2,
    previewOn: false,
    previewPath: "",
    previewUrl: "",
    galleryOn: false,
    gallerySelected: 0,
    galleryAll: false,
    galleryItems: [],
    structuredOpen: false,
    structuredCount: 0,
    canUndoRewash: false,
    rewashUndoCount: 0,
    repeatCount: 1,
    imagePromptMode: "unified",
    plannedTasks: 0,
    promptUnitCount: 0,
    templateLocked: false,
    ...overrides,
  };
}

function renderWorkbench(wb: WorkbenchStateDTO) {
  useAppStore.setState({ workbench: wb });
  return render(
    <TooltipProvider>
      <WorkbenchPage />
    </TooltipProvider>,
  );
}

function makeMockCanvas2D(): CanvasRenderingContext2D {
  const noop = () => {};
  const grad = { addColorStop: noop };
  return {
    clearRect: noop,
    fillRect: noop,
    drawImage: noop,
    save: noop,
    restore: noop,
    scale: noop,
    translate: noop,
    rotate: noop,
    setTransform: noop,
    beginPath: noop,
    closePath: noop,
    moveTo: noop,
    lineTo: noop,
    quadraticCurveTo: noop,
    bezierCurveTo: noop,
    arc: noop,
    arcTo: noop,
    ellipse: noop,
    roundRect: noop,
    fill: noop,
    stroke: noop,
    clip: noop,
    createLinearGradient: () => grad,
    globalAlpha: 1,
    fillStyle: "",
    strokeStyle: "",
    lineWidth: 1,
    lineCap: "butt",
    lineJoin: "miter",
  } as unknown as CanvasRenderingContext2D;
}

beforeEach(() => {
  mockMyGoActive = false;
  fileDropListeners.clear();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation(
    () => makeMockCanvas2D() as never,
  );
  vi.spyOn(events.tulaiFileDrop, "on").mockImplementation(() => () => {});
  vi.spyOn(events.tulaiTopics, "on").mockImplementation(() => () => {});
  vi.spyOn(Workbench, "setWorkbenchField").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "startTasks").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "stopTasks").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "addWorkbenchSources").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "clearWashedWorkbenchSources").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "clearWorkbenchResults").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "deleteWorkbenchResult").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "clearWorkbenchSources").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "pickWorkbenchFiles").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "pickWorkbenchFolder").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "setPreviewPath").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "setResultsDoneOpen").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "setGalleryOpen").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "setAllGallerySelect").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "toggleGallerySelect").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "removeGallerySourceItem").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "requeueDoneTasks").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "undoRequeueTasks").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "replaceResultImage").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "setPromptPart").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "applyWorkbenchPrompt").mockResolvedValue(undefined);
  vi.spyOn(Workbench, "getWorkbenchState").mockImplementation(async () => {
    return useAppStore.getState().workbench ?? makeWorkbench();
  });
  vi.spyOn(Browser, "browserRowAction").mockResolvedValue(undefined);
  vi.spyOn(Browser, "setPlatformAccountsDisabled").mockResolvedValue(undefined);
  vi.spyOn(Browser, "createBrowser").mockResolvedValue(undefined);
  vi.spyOn(Browser, "openBrowserDrawer").mockResolvedValue(undefined);
  vi.spyOn(ImageEdit, "editOpenPath").mockResolvedValue(undefined);
  vi.spyOn(ImageEdit, "getImageEditState").mockResolvedValue({
    files: [],
    selected: -1,
    pickCount: 0,
    running: false,
  } as never);
  vi.spyOn(Prompts, "setPromptSuffix").mockResolvedValue(undefined);
  vi.spyOn(Settings, "setSettingField").mockResolvedValue(undefined);
  vi.spyOn(Core, "openPath").mockResolvedValue(undefined);
  vi.spyOn(Core, "revealPath").mockResolvedValue(undefined);
  vi.spyOn(Core, "copyText").mockResolvedValue(undefined);
  vi.spyOn(Core, "copyImage").mockResolvedValue(undefined);
  vi.spyOn(Core, "setTab").mockResolvedValue(undefined);
  vi.spyOn(Core, "getShellState").mockImplementation(async () => useAppStore.getState().shell);
});

afterEach(() => {
  cleanup();
  resetStoreForTests();
  vi.restoreAllMocks();
});

describe("WorkbenchPage modes, contracts & CJK IME", () => {
  it("renders clean empty Bento fallback outside MyGo runtime and switches modes via shell state, showing skeleton only while loading", () => {
    // Loading state with null workbench renders skeleton
    useAppStore.setState((st) => ({
      workbench: null,
      loadingPages: { ...st.loadingPages, workbench: true },
    }));
    const { rerender } = render(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(screen.getByTestId("wb-skeleton")).toBeInTheDocument();

    // Non-loading null workbench (e.g. standalone preview) falls back cleanly across modes & platforms
    act(() => {
      useAppStore.setState((st) => ({
        workbench: null,
        loadingPages: { ...st.loadingPages, workbench: false },
        shell: { ...st.shell, tab: TAB_WASH, platform: "豆包" },
      }));
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(screen.queryByTestId("wb-skeleton")).not.toBeInTheDocument();
    expect(screen.getByTestId("wb-composer")).toBeInTheDocument();
    expect(screen.getByTestId("wb-stage")).toBeInTheDocument();
    expect(screen.getByTestId("lane-table")).toBeInTheDocument();
    expect(screen.getByTestId("wb-result-gallery")).toBeInTheDocument();
    expect(within(screen.getByTestId("wb-config-card")).getByText("提示词")).toBeInTheDocument();
    expect(screen.getByTestId("wb-start-btn")).toHaveTextContent("开始洗图");
    expect(screen.queryByText("豆包 · 账号")).not.toBeInTheDocument();
    expect(screen.getByTestId("lane-platform-icon")).toHaveAttribute("src", doubaoMark);

    // Switching shell tab/platform updates fallback mode & provider cleanly
    act(() => {
      useAppStore.setState((st) => ({
        shell: { ...st.shell, tab: TAB_GEN, platform: "千问" },
      }));
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(within(screen.getByTestId("wb-config-card")).getByText("提示词")).toBeInTheDocument();
    expect(screen.getByTestId("wb-batch-mode-tabs")).toBeInTheDocument();
    expect(screen.getByTestId("wb-start-btn")).toHaveTextContent("开始文生图");
    expect(screen.queryByText("千问 · 账号")).not.toBeInTheDocument();
    expect(screen.getByTestId("lane-platform-icon")).toHaveAttribute("src", qwenMark);

    act(() => {
      useAppStore.setState((st) => ({
        shell: { ...st.shell, tab: TAB_FISSION, platform: "豆包" },
      }));
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(within(screen.getByTestId("wb-config-card")).getByText("提示词")).toBeInTheDocument();
    expect(screen.getByText("跳过参考图直接按提示词裂变")).toBeInTheDocument();
    expect(screen.getByTestId("wb-start-btn")).toHaveTextContent("开始裂变");
  });

  it("renders wash mode controls with expanded prompt editor, CJK IME safety, and omits forbidden chrome", () => {
    renderWorkbench(makeWorkbench({ mode: "wash" }));

    expect(screen.getByTestId("wb-composer")).toBeInTheDocument();
    expect(screen.getByTestId("wb-stage")).toBeInTheDocument();
    expect(screen.getByTestId("wb-wash-style-select")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "默认词" })).not.toBeInTheDocument();
    expect(screen.queryByTestId("wb-prompt-summary")).not.toBeInTheDocument();
    expect(screen.queryByTestId("wb-prompt-toggle")).not.toBeInTheDocument();
    expect(screen.getByTestId("wb-output-preset-select")).toBeInTheDocument();
    expect(screen.queryByTestId("wb-output-ratio-select")).not.toBeInTheDocument();
    expect(screen.getByTestId("wb-config-card")).toHaveClass("flex-1");
    expect(screen.getByTestId("wb-sources-card")).toHaveClass("h-[212px]", "shrink-0");
    expect(
      screen.getByTestId("wb-output-preset-select").compareDocumentPosition(
        screen.getByTestId("wb-prompt-editor"),
      ) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(screen.getByTestId("wb-import-files")).toHaveTextContent("导入文件");
    expect(screen.getByTestId("wb-import-folder")).toHaveTextContent("导入文件夹");
    expect(screen.getByTestId("wb-start-btn")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("wb-import-files"));
    expect(Workbench.pickWorkbenchFiles).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByTestId("wb-import-folder"));
    expect(Workbench.pickWorkbenchFolder).toHaveBeenCalledTimes(1);

    // Forbidden chrome must NEVER appear on Workbench
    for (const forbidden of [
      "page-title",
      "topbar-platform-badge",
      "wb-platform-tabs",
      "wb-history-btn",
      "wb-progress-bar",
      "wb-conveyor-track",
      "wb-staging-queue",
      "topbar-pool-summary",
      "topbar-running-badge",
    ]) {
      expect(screen.queryByTestId(forbidden)).not.toBeInTheDocument();
    }

    // Prompt editor is expanded by default; verify CJK IME composition defers commit until compositionEnd
    const editor = screen.getByTestId("wb-prompt-editor") as HTMLTextAreaElement;
    expect(editor).toBeInTheDocument();
    expect(editor).toHaveClass("flex-1", "min-h-[132px]");

    fireEvent.compositionStart(editor);
    fireEvent.change(editor, { target: { value: "纯白baidi" } });
    expect(Workbench.setWorkbenchField).not.toHaveBeenCalledWith("prompts", "纯白baidi");

    fireEvent.compositionEnd(editor, { target: { value: "纯白白底电商图" } });
    expect(Workbench.setWorkbenchField).toHaveBeenCalledWith("prompts", "纯白白底电商图");

    // The result area animates immediately, before IPC publishes the new queue.
    fireEvent.click(screen.getByTestId("wb-start-btn"));
    expect(Workbench.startTasks).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("wb-starting-animation").querySelector("canvas")).toBeInTheDocument();
  });

  it("disables start button without rendering a blocked warning banner when not ready", () => {
    renderWorkbench(
      makeWorkbench({
        mode: "wash",
        running: false,
        canStart: false,
      }),
    );

    const startBtn = screen.getByTestId("wb-start-btn");
    const actionCard = screen.getByTestId("wb-action-card");
    expect(startBtn).toBeDisabled();
    expect(startBtn).toHaveTextContent("开始洗图");
    expect(actionCard).toContainElement(startBtn);
    expect(screen.queryByTestId("wb-start-blocked-hint")).not.toBeInTheDocument();
  });

  it("renders free and fission mode controls and switches start/stop button", () => {
    const { rerender } = renderWorkbench(
      makeWorkbench({
        mode: "free",
        structuredCount: 2,
        running: true,
      }),
    );

    expect(within(screen.getByTestId("wb-config-card")).getByText("提示词")).toBeInTheDocument();
    expect(screen.getByText("结构化 (2)")).toBeInTheDocument();
    expect(screen.getByTestId("wb-prompt-template-btn")).toBeInTheDocument();
    expect(screen.getByTestId("wb-batch-mode-tabs")).toBeInTheDocument();
    expect(screen.getByTestId("wb-config-card")).toHaveClass("flex-1");
    expect(screen.getByTestId("wb-prompt-editor")).toHaveClass("flex-1", "min-h-[112px]");
    // 文生图模式隐藏参考素材卡以释放提示词编辑空间
    expect(screen.queryByTestId("wb-sources-card")).not.toBeInTheDocument();
    expect(screen.queryByTestId("wb-suffix-input")).not.toBeInTheDocument();
    expect(
      screen.getByTestId("wb-batch-mode-tabs").compareDocumentPosition(
        screen.getByTestId("wb-prompt-editor"),
      ) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      screen.getByTestId("wb-output-preset-select").compareDocumentPosition(
        screen.getByTestId("wb-prompt-editor"),
      ) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      screen.getByTestId("wb-structured-btn").compareDocumentPosition(
        screen.getByTestId("wb-prompt-editor"),
      ) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(screen.getByTestId("wb-stop-btn")).toBeInTheDocument();
    expect(screen.queryByTestId("wb-start-btn")).not.toBeInTheDocument();

    // Switching batch mode to 图生图 sends "1"
    fireEvent.click(within(screen.getByTestId("wb-batch-mode-tabs")).getByRole("tab", { name: /图生图/ }));
    expect(Workbench.setWorkbenchField).toHaveBeenCalledWith("batchMode", "1");

    fireEvent.click(screen.getByTestId("wb-stop-btn"));
    expect(Workbench.stopTasks).toHaveBeenCalledTimes(1);

    // Enable suffix & custom output size reveal inline controls progressively above the prompt editor;
    // switching to 图生图 (batchMode=1) reveals the reference images card
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "free",
          batchMode: 1,
          useSuffix: true,
          outputPreset: "自定义尺寸",
          outputPresets: ["原比例", "自定义尺寸"],
          outputMode: "固定尺寸",
          outputWidth: "1200",
          outputHeight: "900",
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(screen.getByTestId("wb-sources-card")).toHaveClass("h-[212px]", "shrink-0");
    expect(screen.getByTestId("wb-suffix-input")).toBeInTheDocument();
    expect(screen.getByTestId("wb-output-custom-row")).toBeInTheDocument();
    expect(screen.getByTestId("wb-output-width")).toHaveValue("1200");
    expect(screen.getByTestId("wb-output-height")).toHaveValue("900");
    expect(
      screen.getByTestId("wb-suffix-input").compareDocumentPosition(
        screen.getByTestId("wb-prompt-editor"),
      ) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      screen.getByTestId("wb-output-custom-row").compareDocumentPosition(
        screen.getByTestId("wb-prompt-editor"),
      ) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    // Switch to fission mode
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({ mode: "fission", running: false, sources: [], sourcesTotal: 0 }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );

    expect(within(screen.getByTestId("wb-config-card")).getByText("提示词")).toBeInTheDocument();
    expect(screen.getByTestId("wb-prompt-template-btn")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "默认词" })).not.toBeInTheDocument();
    expect(screen.getByText("跳过参考图直接按提示词裂变")).toBeInTheDocument();
    expect(screen.getByTestId("wb-config-card")).toHaveClass("flex-1");
    expect(screen.getByTestId("wb-prompt-editor")).toHaveClass("flex-1", "min-h-[116px]");
    expect(
      screen.getByTestId("wb-output-preset-select").compareDocumentPosition(
        screen.getByTestId("wb-prompt-editor"),
      ) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      screen.getByRole("switch", { name: "跳过参考图直接按提示词裂变" }).compareDocumentPosition(
        screen.getByTestId("wb-prompt-editor"),
      ) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(screen.getByTestId("wb-sources-card")).toHaveClass("h-[212px]", "shrink-0");
    expect(screen.getByTestId("wb-import-files")).toHaveTextContent("导入文件");
    expect(screen.queryByTestId("wb-import-folder")).not.toBeInTheDocument();
    expect(screen.getByText("单图裂变：支持拖入 1 张主图")).toBeInTheDocument();
  });

  it("renders prompt template dropdown in wash, free, and fission modes (including skipImages) and applies built-in or user templates", async () => {
    const washTemplates = [
      { id: "doubao_wash", label: "豆包洗图", kindLbl: "洗图", builtin: true, matched: true },
      { id: "u1", label: "电商纯白洗图", kindLbl: "洗图", builtin: false, matched: true },
      { id: "u2", label: "赛博生图词", kindLbl: "生图", builtin: false, matched: false },
    ];
    const { rerender } = renderWorkbench(
      makeWorkbench({
        mode: "wash",
        promptChoice: "选择提示词",
        promptOptions: ["选择提示词", "豆包洗图", "电商纯白洗图"],
        promptTemplates: washTemplates,
      }),
    );

    const washTplBtn = screen.getByTestId("wb-prompt-template-btn");
    expect(washTplBtn).toHaveTextContent("模板");
    fireEvent.keyDown(washTplBtn, { key: "Enter" });
    expect(await screen.findByText("恢复默认洗图词")).toBeInTheDocument();
    expect(screen.getByText("默认词")).toBeInTheDocument();
    expect(screen.getByText("豆包洗图")).toBeInTheDocument();
    expect(screen.getByText("内置")).toBeInTheDocument();
    expect(screen.getByText("其他分类")).toBeInTheDocument();
    expect(screen.getByText("赛博生图词")).toBeInTheDocument();
    fireEvent.click(screen.getByText("豆包洗图"));
    expect(Workbench.applyWorkbenchPrompt).toHaveBeenCalledWith("豆包洗图");

    fireEvent.keyDown(washTplBtn, { key: "Enter" });
    fireEvent.click(await screen.findByText("恢复默认洗图词"));
    expect(Workbench.applyWorkbenchPrompt).toHaveBeenCalledWith("选择提示词");

    // Fission mode (with skipImages=false) puts 默认词 inside the template dropdown
    vi.mocked(Workbench.applyWorkbenchPrompt).mockClear();
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "fission",
          skipImages: false,
          promptChoice: "四视角裂变",
          promptOptions: ["选择提示词", "四视角裂变"],
          promptTemplates: [
            { id: "u3", label: "四视角裂变", kindLbl: "裂变", builtin: false, matched: true },
          ],
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );

    const fissionDefaultBtn = screen.getByTestId("wb-prompt-template-btn");
    fireEvent.keyDown(fissionDefaultBtn, { key: "Enter" });
    expect(await screen.findByText("恢复默认裂变词")).toBeInTheDocument();
    expect(screen.getByText("默认词")).toBeInTheDocument();
    fireEvent.click(screen.getByText("恢复默认裂变词"));
    expect(Workbench.applyWorkbenchPrompt).toHaveBeenCalledWith("选择提示词");

    // Fission mode (even with skipImages=true) keeps template button and omits mother-image reset label
    vi.mocked(Workbench.applyWorkbenchPrompt).mockClear();
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "fission",
          skipImages: true,
          promptChoice: "四视角裂变",
          promptOptions: ["选择提示词", "四视角裂变"],
          promptTemplates: [
            { id: "u3", label: "四视角裂变", kindLbl: "裂变", builtin: false, matched: true },
          ],
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );

    const fissionTplBtn = screen.getByTestId("wb-prompt-template-btn");
    expect(fissionTplBtn).toHaveTextContent("四视角裂变");
    fireEvent.keyDown(fissionTplBtn, { key: "Enter" });
    expect(await screen.findByText("自由输入（不套用模板）")).toBeInTheDocument();
    expect(screen.queryByText("恢复默认裂变词")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("管理提示词模板..."));
    expect(useAppStore.getState().shell.tab).toBe(TAB_PROMPTS);
  });
});

describe("Workbench sources, thumbnails, washed bar & native file drop", () => {
  it("renders thumbnail URLs (&w=360), washed bar clear action, and ingests dropped OS paths", () => {
    mockMyGoActive = true;
    vi.spyOn(events.tulaiFileDrop, "on").mockImplementation(() => () => {});

    renderWorkbench(
      makeWorkbench({
        mode: "wash",
        sources: [
          makeSource({ index: 0, washed: true }),
          makeSource({ index: 1, washed: false }),
        ],
        sourcesTotal: 2,
        washedCount: 1,
      }),
    );

    const thumb = screen.getByTestId("wb-source-thumb-0") as HTMLImageElement;
    expect(thumb.src).toContain("w=360");

    expect(screen.getByTestId("wb-washed-bar")).toHaveTextContent("已洗 1 / 2");
    fireEvent.click(screen.getByTestId("wb-clear-washed-btn"));
    expect(Workbench.clearWashedWorkbenchSources).toHaveBeenCalledTimes(1);

    // Simulate OS native file drop via mygo-runtime callback
    act(() => {
      for (const cb of fileDropListeners) {
        cb({ paths: ["/Users/demo/a.png", "/Users/demo/folder"], x: 200, y: 200 });
      }
    });
    expect(Workbench.addWorkbenchSources).toHaveBeenCalledWith([
      "/Users/demo/a.png",
      "/Users/demo/folder",
    ]);
  });
});

describe("AccountLanes", () => {
  it("hides Fingerprint badge and renders toggle switches, enable/disable all, and collapsed pills", () => {
    const lanes = [
      makeLane({ id: 1, title: "豆包主号", engine: "Fingerprint", disabled: false }),
      makeLane({
        id: 2,
        title: "豆包副号",
        engine: "Fingerprint",
        disabled: true,
        state: "验证码",
      }),
    ];
    const { rerender } = renderWorkbench(makeWorkbench({ lanes, laneCollapsed: false }));

    expect(screen.getByTestId("lane-table")).toBeInTheDocument();
    expect(screen.getByTestId("lane-table-header")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "新建账号" })).not.toBeInTheDocument();
    expect(screen.getByTestId("wb-account-list")).toBeInTheDocument();
    expect(screen.getByTestId("lane-toggle-1")).toBeInTheDocument();
    expect(screen.getByTestId("lane-toggle-2")).toBeInTheDocument();
    expect(screen.queryByText("Fingerprint")).not.toBeInTheDocument();

    const viewBtn = screen.getByTestId("browser-view-btn-1");
    expect(screen.getByTestId("lane-actions-1")).toContainElement(viewBtn);
    expect(viewBtn).toHaveTextContent("");
    expect(screen.queryByText("可视")).not.toBeInTheDocument();
    fireEvent.click(viewBtn);
    expect(Browser.openBrowserDrawer).toHaveBeenCalledWith(1);

    fireEvent.click(screen.getByTestId("lanes-enable-all"));
    expect(Browser.setPlatformAccountsDisabled).toHaveBeenCalledWith("豆包", false);

    fireEvent.click(screen.getByTestId("lanes-disable-all"));
    expect(Browser.setPlatformAccountsDisabled).toHaveBeenCalledWith("豆包", true);

    fireEvent.click(screen.getByRole("button", { name: "去验证" }));
    expect(Browser.openBrowserDrawer).toHaveBeenCalledWith(2);

    // Collapsed mode (<= 4 renders inline inside header)
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          lanes,
          laneCollapsed: true,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    const header = screen.getByTestId("lane-table-header");
    expect(within(header).getByTestId("lane-pill-1")).toBeInTheDocument();
    expect(header).toHaveTextContent("待验证 1");
    const verifyPill = within(header).getByTestId("lane-pill-2");
    expect(verifyPill).toHaveTextContent("去验证");
    vi.mocked(Browser.openBrowserDrawer).mockClear();
    vi.mocked(Browser.browserRowAction).mockClear();
    fireEvent.click(verifyPill);
    expect(Browser.openBrowserDrawer).toHaveBeenCalledWith(2);
    expect(Browser.browserRowAction).not.toHaveBeenCalled();

    // Expanded mode does not render shared-exit hint on workbench
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          lanes,
          laneCollapsed: false,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(screen.queryByTestId("lanes-shared-exit-hint")).not.toBeInTheDocument();

    // Empty lanes redirects to 账号池 instead of creating inline
    act(() => {
      useAppStore.setState({ workbench: makeWorkbench({ lanes: [], laneCollapsed: false }) });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    const goPoolBtn = screen.getByTestId("lanes-go-pool-btn");
    expect(goPoolBtn).toHaveTextContent("前往账号池添加");
    fireEvent.click(goPoolBtn);
    expect(useAppStore.getState().shell.tab).toBe(TAB_BROWSER);
  });

  it("toggles account action between 打开, 打开中, 关闭, and 关闭中 (including unlogged running accounts)", () => {
    const lanes = [
      makeLane({ id: 1, title: "未开号", isRunning: false, isLoggedIn: true }),
      makeLane({ id: 2, title: "已开号", isRunning: true, isLoggedIn: true }),
      makeLane({ id: 3, title: "未登录已开", isRunning: true, isLoggedIn: false, state: "需要登录" }),
      makeLane({ id: 4, title: "启动中号", isRunning: false, pending: true, pendingOp: "starting" }),
      makeLane({ id: 5, title: "关闭中号", isRunning: true, pending: true, pendingOp: "stopping" }),
    ];
    renderWorkbench(makeWorkbench({ lanes, laneCollapsed: false, running: false }));

    const openBtn = within(screen.getByTestId("lane-actions-1")).getByRole("button", { name: "打开" });
    fireEvent.click(openBtn);
    expect(Browser.browserRowAction).toHaveBeenCalledWith(1, "launch");

    const closeBtn = within(screen.getByTestId("lane-actions-2")).getByRole("button", { name: "关闭" });
    fireEvent.click(closeBtn);
    expect(Browser.browserRowAction).toHaveBeenCalledWith(2, "stop");

    const unloggedCloseBtn = within(screen.getByTestId("lane-actions-3")).getByRole("button", {
      name: "关闭",
    });
    fireEvent.click(unloggedCloseBtn);
    expect(Browser.browserRowAction).toHaveBeenCalledWith(3, "stop");

    expect(
      within(screen.getByTestId("lane-actions-4")).getByRole("button", { name: "打开中" }),
    ).toBeDisabled();
    expect(
      within(screen.getByTestId("lane-actions-5")).getByRole("button", { name: "关闭中" }),
    ).toBeDisabled();
  });

  it("toggles independent Doubao/Qwen auto-concurrency and adjusts per-platform max within [2, 8] inside lane header", () => {
    renderWorkbench(
      makeWorkbench({
        doubao: true,
        qianwen: false,
        lanes: [makeLane({ id: 1, title: "豆包主号" }), makeLane({ id: 2, title: "豆包副号" })],
        concurrencyAuto: true,
        concurrencyMax: 2,
      }),
    );

    const header = screen.getByTestId("lane-table-header");
    const ctrl = within(header).getByTestId("lanes-concurrency-control");
    const toggle = within(ctrl).getByTestId("lanes-concurrency-switch");
    expect(toggle).toHaveAttribute("aria-checked", "true");
    expect(within(ctrl).getByTestId("lanes-concurrency-max")).toHaveTextContent("2");
    expect(within(ctrl).getByTestId("lanes-concurrency-dec")).toBeDisabled();
    expect(within(ctrl).getByTestId("lanes-concurrency-inc")).not.toBeDisabled();

    // Doubao increment 2 -> 3 -> 4 uses Doubao-scoped keys
    fireEvent.click(within(ctrl).getByTestId("lanes-concurrency-inc"));
    expect(Settings.setSettingField).toHaveBeenCalledWith("concurrencyMaxDoubao", "3");
    expect(within(ctrl).getByTestId("lanes-concurrency-max")).toHaveTextContent("3");
    expect(within(ctrl).getByTestId("lanes-concurrency-dec")).not.toBeDisabled();

    fireEvent.click(within(ctrl).getByTestId("lanes-concurrency-inc"));
    expect(Settings.setSettingField).toHaveBeenCalledWith("concurrencyMaxDoubao", "4");
    expect(within(ctrl).getByTestId("lanes-concurrency-max")).toHaveTextContent("4");

    // Decrement 4 -> 3
    fireEvent.click(within(ctrl).getByTestId("lanes-concurrency-dec"));
    expect(Settings.setSettingField).toHaveBeenCalledWith("concurrencyMaxDoubao", "3");
    expect(within(ctrl).getByTestId("lanes-concurrency-max")).toHaveTextContent("3");

    // Upper bound 8 disables increment
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({ doubao: true, qianwen: false, concurrencyAuto: true, concurrencyMax: 8 }),
      });
    });
    expect(within(ctrl).getByTestId("lanes-concurrency-max")).toHaveTextContent("8");
    expect(within(ctrl).getByTestId("lanes-concurrency-inc")).toBeDisabled();

    // Turning off Doubao auto-concurrency hides stepper and persists Doubao key
    fireEvent.click(toggle);
    expect(Settings.setSettingField).toHaveBeenCalledWith("concurrencyAutoDoubao", "false");
    expect(toggle).toHaveAttribute("aria-checked", "false");
    expect(within(ctrl).queryByTestId("lanes-concurrency-stepper")).not.toBeInTheDocument();

    // Turning back on restores stepper and persists Doubao key
    fireEvent.click(toggle);
    expect(Settings.setSettingField).toHaveBeenCalledWith("concurrencyAutoDoubao", "true");
    expect(toggle).toHaveAttribute("aria-checked", "true");
    expect(within(ctrl).getByTestId("lanes-concurrency-stepper")).toBeInTheDocument();

    // Switch to Qwen: writes independent Qwen-scoped keys
    vi.mocked(Settings.setSettingField).mockClear();
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          doubao: false,
          qianwen: true,
          lanes: [makeLane({ id: 9, platform: "千问", title: "千问主号" })],
          concurrencyAuto: false,
          concurrencyMax: 3,
        }),
      });
    });
    expect(toggle).toHaveAttribute("aria-checked", "false");
    expect(within(ctrl).queryByTestId("lanes-concurrency-stepper")).not.toBeInTheDocument();

    fireEvent.click(toggle);
    expect(Settings.setSettingField).toHaveBeenCalledWith("concurrencyAutoQianwen", "true");
    expect(within(ctrl).getByTestId("lanes-concurrency-max")).toHaveTextContent("3");

    fireEvent.click(within(ctrl).getByTestId("lanes-concurrency-inc"));
    expect(Settings.setSettingField).toHaveBeenCalledWith("concurrencyMaxQianwen", "4");
    expect(within(ctrl).getByTestId("lanes-concurrency-max")).toHaveTextContent("4");
  });

  it("renders only 空闲 (green/success) and 工作 (blue/accent) status badges in lane rows", () => {
    const lanes = [
      makeLane({ id: 1, title: "工位工作", state: "出图中" }),
      makeLane({ id: 2, title: "工位空闲", state: "空闲", readyInSec: 5 }),
      makeLane({ id: 3, title: "工位冷却", state: "冷却中", cooldownAt: "12:30" }),
      makeLane({ id: 4, title: "工位停用", state: "已禁用", disabled: true }),
      makeLane({ id: 5, title: "工位验证", state: "验证码" }),
    ];
    renderWorkbench(makeWorkbench({ lanes, laneCollapsed: false }));

    const list = screen.getByTestId("wb-account-list");
    const workingBadges = within(list).getAllByText("工作");
    expect(workingBadges).toHaveLength(1);
    expect(workingBadges[0]).toHaveClass("text-accent", "bg-accent-soft", "border-accent-border");

    const idleBadges = within(list).getAllByText("空闲");
    expect(idleBadges).toHaveLength(4);
    for (const badge of idleBadges) {
      expect(badge).toHaveClass("text-status-ok");
      expect(badge).not.toHaveClass("text-accent");
    }

    expect(within(list).queryByText("空闲就绪")).not.toBeInTheDocument();
    expect(within(list).queryByText("出图中")).not.toBeInTheDocument();
    expect(within(list).queryByText("冷却至 12:30")).not.toBeInTheDocument();
    expect(within(list).queryByText("已禁用")).not.toBeInTheDocument();
  });

  it("renders hairline-divided table rows without inner rounded rectangles", () => {
    const lanes = [
      makeLane({ id: 1, title: "1", state: "空闲" }),
      makeLane({ id: 2, title: "2", state: "空闲" }),
    ];
    renderWorkbench(makeWorkbench({ lanes, laneCollapsed: false }));

    const list = screen.getByTestId("wb-account-list");
    expect(within(list).getByTestId("lane-columns-header")).toHaveTextContent("账号");
    expect(within(list).getByTestId("lane-columns-header")).toHaveTextContent("状态");
    expect(within(list).getByTestId("lane-columns-header")).toHaveTextContent("今日用量");
    expect(within(list).getByTestId("lane-columns-header")).toHaveTextContent("操作");

    const row1 = within(list).getByTestId("lane-row-1");
    const row2 = within(list).getByTestId("lane-row-2");
    expect(row1).toHaveClass("border-b", "border-divider");
    expect(row1).not.toHaveClass("rounded-[12px]");
    expect(row2).not.toHaveClass("border-b");
    expect(row2).not.toHaveClass("rounded-[12px]");
  });
});

describe("GenerationStageCard (DotMatrix) & ResultGrid pipeline, density, Shift+Click multi-select & rewash", () => {
  it("renders procedural dot-matrix generation stage, moving-sheen status progression, animation pause toggle, issue frozen dots, and pending queue", () => {
    vi.useFakeTimers();
    const activeTile = makeTile({
      taskIndex: 1,
      status: "进行中",
      phase: "generating",
      fileUrl: "",
      accountName: "工位A",
    });
    const pendingTile = makeTile({
      taskIndex: 2,
      status: "排队中",
      fileUrl: "",
      rewashCount: 1,
      title: "二次图.png",
    });
    const failedTile = makeTile({
      taskIndex: 9,
      status: "失败",
      fileUrl: "",
      reason: "上游模型超时",
      actionText: "重试",
      title: "失败图.png",
    });

    renderWorkbench(
      makeWorkbench({
        activeCount: 1,
        activeRows: [{ key: "r1", header: false, issue: false, title: "工位A", summary: "", tiles: [activeTile] }],
        pendingTiles: [pendingTile],
        issueRows: [{ key: "i1", header: false, issue: true, title: "待处理", summary: "1 项", tiles: [failedTile] }],
        doneTiles: [],
      }),
    );

    // Legacy robot canvas is gone; replaced by procedural dot-matrix stage
    expect(screen.queryByTestId("washbot-canvas-1")).not.toBeInTheDocument();
    expect(within(screen.getByTestId("result-tile-card-1")).queryByText("Fingerprint")).not.toBeInTheDocument();
    const stage = screen.getByTestId("dotmatrix-stage-1");
    expect(stage.querySelector("canvas")).toBeInTheDocument();

    // Status line starts at "正在生成初稿…" then progresses to "正在细化细节…" after 1.3s
    expect(stage).toHaveTextContent("正在生成初稿…");
    act(() => {
      vi.advanceTimersByTime(1350);
    });
    expect(stage).toHaveTextContent("正在细化细节…");

    // Toggle animation pause freezes dot-matrix stage
    fireEvent.click(screen.getByTestId("wb-bot-anim-toggle"));
    expect(stage).toHaveTextContent("点阵已暂停");
    fireEvent.click(screen.getByTestId("wb-bot-anim-toggle"));
    expect(stage.querySelector("canvas")).toBeInTheDocument();

    // Pending tile has #02 and 二次洗 chip, and NO redundant 等待 label
    const pendingCard = screen.getByTestId("pending-tile-2");
    expect(pendingCard).toHaveTextContent("#02");
    expect(pendingCard).toHaveTextContent("二次洗");
    expect(pendingCard).not.toHaveTextContent("等待");

    // Failed issue card freezes dots on error, displays reason, and offers retry
    const issueBox = screen.getByTestId("issue-dotmatrix-9");
    expect(issueBox).toHaveAttribute("data-phase", "error");
    expect(issueBox).toHaveTextContent("上游模型超时");

    vi.useRealTimers();
  });

  it("holds each queued position when tasks finish out of order and one task returns multiple images", () => {
    const first = makeTile({ taskIndex: 1, fileUrl: "", path: "" });
    const second = makeTile({ taskIndex: 2, fileUrl: "", path: "" });
    const third = makeTile({ taskIndex: 3, fileUrl: "", path: "" });
    const run = makeWorkbench({ running: true, pendingTiles: [first, second, third] });
    renderWorkbench(run);

    const firstSlot = screen.getByTestId("result-slot-1");
    const secondSlot = screen.getByTestId("result-slot-2");
    const thirdSlot = screen.getByTestId("result-slot-3");
    const grid = firstSlot.parentElement!;
    expect(Array.from(grid.children)).toEqual([firstSlot, secondSlot, thirdSlot]);
    expect(firstSlot.querySelector("canvas")).toBeInTheDocument();
    expect(firstSlot.querySelector("img")).toBeNull();

    act(() => useAppStore.setState({ workbench: {
      ...run, pendingTiles: [first, third], doneTiles: [
        makeTile({ taskIndex: 2 }), makeTile({ taskIndex: 2, key: "second-output", fileUrl: "second.png" }),
      ],
    } }));
    expect(Array.from(grid.children).slice(0, 3)).toEqual([firstSlot, secondSlot, thirdSlot]);
    expect(grid.children).toHaveLength(4);
    expect(secondSlot.querySelector("img")).toHaveAttribute("src", expect.stringContaining("out2"));
    expect(firstSlot.querySelector("canvas")).toBeInTheDocument();
    expect(thirdSlot.querySelector("img")).toBeNull();
  });

  it("reveals every output when a run starts and finishes between task snapshots", () => {
    const run = makeWorkbench({ running: true, pendingTiles: [], activeRows: [] });
    renderWorkbench(run);
    const doneTiles = [makeTile(), makeTile({ key: "extra", fileUrl: "second.png" })];
    act(() => useAppStore.setState({ workbench: {
      ...run, running: false, doneTiles, doneCount: 2, doneTasks: 1,
    } }));
    const images = screen.getAllByTestId("done-dotmatrix-1");
    expect(images).toHaveLength(2);
    for (const image of images) expect(image).toHaveAttribute("data-phase", "loading");
  });

  it("supports density toggle, Shift+Click range selection, batch/single rewash, undo, and zero inline text buttons", async () => {
    mockMyGoActive = true;
    const doneTiles = [
      makeTile({ taskIndex: 1, title: "结果1" }),
      makeTile({ taskIndex: 2, title: "结果2" }),
      makeTile({ taskIndex: 3, title: "结果3" }),
      makeTile({ taskIndex: 4, title: "结果4" }),
    ];

    renderWorkbench(
      makeWorkbench({
        doneTiles,
        doneCount: 4,
        doneTasks: 4,
        resultsDoneOpen: true,
        canUndoRewash: true,
        rewashUndoCount: 2,
      }),
    );

    const gallery = screen.getByTestId("wb-result-gallery");
    expect(gallery).toBeInTheDocument();

    // Thumbnail uses w=360
    const img1 = screen.getByTestId("result-tile-image-1") as HTMLImageElement;
    expect(img1.src).toContain("w=360");

    // Zero inline text action buttons (定位/打开/替换/完成) or corner checkbox/eye button on done tile card
    const tile1Card = screen.getByTestId("result-tile-card-1");
    expect(within(tile1Card).queryByRole("button", { name: "定位" })).not.toBeInTheDocument();
    expect(within(tile1Card).queryByRole("button", { name: "打开" })).not.toBeInTheDocument();
    expect(within(tile1Card).queryByRole("button", { name: "替换" })).not.toBeInTheDocument();
    expect(within(tile1Card).queryByRole("button", { name: "完成" })).not.toBeInTheDocument();
    expect(screen.queryByTestId("done-select-1")).not.toBeInTheDocument();
    expect(screen.queryByTestId("rewash-tile-1")).not.toBeInTheDocument();
    expect(within(tile1Card).queryByRole("button", { name: "大图预览" })).not.toBeInTheDocument();
    expect(screen.queryByTestId("result-tile-menu-1")).not.toBeInTheDocument();

    // Clicking the image directly opens lightbox preview
    fireEvent.click(screen.getByTestId("result-tile-image-1"));
    expect(Workbench.setPreviewPath).toHaveBeenCalledWith("/tmp/out-1.png");

    // Density toggle (小图 -> 中图)
    const densityTabs = screen.getByTestId("wb-density-tabs");
    const mdTab = within(densityTabs).getByRole("tab", { name: "中图" });
    fireEvent.click(mdTab);
    expect(mdTab).toHaveAttribute("aria-selected", "true");

    // Enter multi-select mode and Shift+Click range 1..3
    fireEvent.click(screen.getByTestId("wb-batch-select-toggle"));
    fireEvent.click(screen.getByTestId("result-tile-image-1"));
    fireEvent.click(screen.getByTestId("result-tile-image-3"), { shiftKey: true });

    const batchBtn = screen.getByTestId("batch-rewash-btn");
    expect(batchBtn).toHaveTextContent("放回待洗 · 二次洗 (3)");

    fireEvent.click(batchBtn);
    expect(Workbench.requeueDoneTasks).toHaveBeenCalledWith([1, 2, 3]);

    // Single tile actions are accessed via right-click context menu (no card overflow button)
    expect(screen.queryByTestId("rewash-tile-4")).not.toBeInTheDocument();
    expect(screen.queryByTestId("result-tile-menu-4")).not.toBeInTheDocument();
    fireEvent.contextMenu(screen.getByTestId("result-tile-card-4"));
    expect(await screen.findByText("系统默认打开")).toBeInTheDocument();
    expect(screen.getByText("在文件夹中显示")).toBeInTheDocument();
    expect(screen.queryByText("大图预览")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("放回待洗 · 二次洗"));
    expect(Workbench.requeueDoneTasks).toHaveBeenCalledWith([4]);

    // Undo rewash button
    fireEvent.click(screen.getByTestId("wb-undo-rewash-btn"));
    expect(Workbench.undoRequeueTasks).toHaveBeenCalledTimes(1);

    // Clear results button
    fireEvent.click(screen.getByTestId("wb-clear-results-btn"));
    expect(Workbench.clearWorkbenchResults).toHaveBeenCalledTimes(1);
  });

  it("hides clear results button when running or empty, and shows washed empty notice when all wash sources are already washed", () => {
    mockMyGoActive = true;
    const { rerender } = renderWorkbench(
      makeWorkbench({
        running: true,
        doneTiles: [makeTile({ taskIndex: 1 })],
        doneCount: 1,
        totalCount: 1,
      }),
    );

    expect(screen.queryByTestId("wb-clear-results-btn")).not.toBeInTheDocument();

    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "wash",
          running: false,
          doneTiles: [],
          doneCount: 0,
          totalCount: 0,
          sources: [
            makeSource({ index: 0, washed: true }),
            makeSource({ index: 1, washed: true }),
          ],
          sourcesTotal: 2,
          washedCount: 2,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );

    expect(screen.queryByTestId("wb-clear-results-btn")).not.toBeInTheDocument();
    expect(screen.getByText("当前素材已全部洗完")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("wb-empty-clear-washed-btn"));
    expect(Workbench.clearWashedWorkbenchSources).toHaveBeenCalledTimes(1);
  });
});

describe("WorkbenchDialogs & Upgraded Lightbox", () => {
  it("opens full-res Lightbox with arrow navigation, zoom toggle, before/after compare toggle, and Escape priority", () => {
    const doneTiles = [
      makeTile({
        taskIndex: 1,
        path: "/tmp/out-1.png",
        fileUrl: "tulaimedia://localhost/file?t=out1&v=2&w=360",
        srcUrl: "tulaimedia://localhost/file?t=in1&v=1",
        title: "图1",
      }),
      makeTile({
        taskIndex: 2,
        path: "/tmp/out-2.png",
        fileUrl: "tulaimedia://localhost/file?t=out2&v=2",
        srcUrl: "tulaimedia://localhost/file?t=in2&v=1",
        title: "图2",
      }),
    ];

    renderWorkbench(
      makeWorkbench({
        doneTiles,
        previewOn: true,
        previewPath: "/tmp/out-1.png",
        previewUrl: "tulaimedia://localhost/file?t=out1&v=2&w=360",
        galleryOn: true,
        gallerySelected: 1,
        galleryItems: [makeSource({ index: 0, washed: true })],
        washedCount: 1,
      }),
    );

    expect(screen.getByTestId("lightbox-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("gallery-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("gallery-dialog-body")).toBeInTheDocument();
    expect(screen.getByTestId("gallery-clear-washed-btn")).toBeInTheDocument();
    expect(screen.getByTestId("gallery-thumb-0")).toBeInTheDocument();

    // Lightbox uses full-res URL (strips w=360)
    const previewImg = screen.getByTestId("lightbox-preview-image") as HTMLImageElement;
    expect(previewImg.src).not.toContain("w=");

    // Keyboard ArrowRight navigates to next tile
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(Workbench.setPreviewPath).toHaveBeenCalledWith("/tmp/out-2.png");

    // Zoom toggle switches 100% -> 200%
    const zoomBtn = screen.getByTestId("lightbox-zoom-toggle");
    expect(zoomBtn).toHaveTextContent("100%");
    fireEvent.click(zoomBtn);
    expect(zoomBtn).toHaveTextContent("200%");

    // Before/after compare toggle mounts original source overlay
    const compareBtn = screen.getByTestId("lightbox-compare-toggle");
    fireEvent.click(compareBtn);
    expect(screen.getByTestId("lightbox-compare-source")).toBeInTheDocument();
    expect(screen.getByTestId("lightbox-preview-image")).toBeInTheDocument();

    // Lightbox replace action
    fireEvent.click(screen.getByTestId("lightbox-replace-btn"));
    expect(Workbench.replaceResultImage).toHaveBeenCalledWith(1);

    // Priority Escape closes Lightbox (priority 50) BEFORE Gallery dialog (priority 40)
    vi.mocked(Workbench.setPreviewPath).mockClear();
    vi.mocked(Workbench.setGalleryOpen).mockClear();
    act(() => {
      dispatchTopEscape();
    });
    expect(Workbench.setPreviewPath).toHaveBeenCalledWith("");
    expect(Workbench.setGalleryOpen).not.toHaveBeenCalled();
  });

  it("applies the user-selected default dot matrix effect from shell state to generation stage and result tiles", () => {
    useAppStore.setState((st) => ({
      shell: {
        ...st.shell,
        dotMatrixEffect: "snowflakes",
      },
    }));

    const activeTile = makeTile({
      taskIndex: 1,
      status: "进行中",
      phase: "generating",
      fileUrl: "",
      accountName: "工位A",
    });
    const doneTile = makeTile({
      taskIndex: 2,
      status: "完成",
      title: "雪花结果.png",
    });

    renderWorkbench(
      makeWorkbench({
        activeCount: 1,
        activeRows: [{ key: "r1", header: false, issue: false, title: "工位A", summary: "", tiles: [activeTile] }],
        doneTiles: [doneTile],
        doneCount: 1,
        doneTasks: 1,
        resultsDoneOpen: true,
      }),
    );

    const stageBox = screen.getByTestId("dotmatrix-stage-1").querySelector("[data-pattern]");
    expect(stageBox).toHaveAttribute("data-pattern", "snowflakes");
    expect(stageBox).toHaveAttribute("data-reveal", "cellbloom");

    const doneBox = screen.getByTestId("done-dotmatrix-2");
    expect(doneBox).toHaveAttribute("data-pattern", "snowflakes");
    expect(doneBox).toHaveAttribute("data-reveal", "cellbloom");
  });

  it("animates the same task slot through upload, generation and download, then reveals only the saved result", () => {
    const uploadingTile = makeTile({
      taskIndex: 1,
      status: "进行中",
      phase: "uploading",
      fileUrl: "",
      accountName: "工位A",
    });

    const { rerender, unmount } = renderWorkbench(
      makeWorkbench({
        mode: "wash",
        running: true,
        activeCount: 1,
        activeRows: [{ key: "r1", header: false, issue: false, title: "工位A", summary: "", tiles: [uploadingTile] }],
        doneTiles: [],
        resultsDoneOpen: true,
      }),
    );

    const stageUploading = screen.getByTestId("dotmatrix-stage-1");
    const canvas = stageUploading.querySelector("canvas");
    expect(canvas).toBeInTheDocument();
    const slot = screen.getByTestId("result-slot-1");
    expect(stageUploading).toHaveTextContent("正在上传原图…");

    const generatingTile = { ...uploadingTile, phase: "generating" };
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "wash",
          running: true,
          activeCount: 1,
          activeRows: [{ key: "r1", header: false, issue: false, title: "工位A", summary: "", tiles: [generatingTile] }],
          doneTiles: [],
          resultsDoneOpen: true,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    const stageGenerating = screen.getByTestId("dotmatrix-stage-1");
    expect(stageGenerating.querySelector("canvas")).toBe(canvas);
    expect(stageGenerating.querySelector("[data-pattern]")).not.toBeNull();

    // Downloading keeps dot-matrix running until saved
    const downloadingTile = { ...uploadingTile, phase: "downloading", fileUrl: "partial.png" };
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "wash",
          running: true,
          activeCount: 1,
          activeRows: [{ key: "r1", header: false, issue: false, title: "工位A", summary: "", tiles: [downloadingTile] }],
          doneTiles: [],
          resultsDoneOpen: true,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(screen.getByTestId("dotmatrix-stage-1")).toHaveTextContent("已出图，正在保存");
    expect(slot.querySelector("img")).toBeNull();
    expect(screen.getByTestId("dotmatrix-stage-1").querySelector("[data-pattern]")).not.toBeNull();

    // Image downloaded -> moves to doneTiles -> triggers non-settled dot-matrix reveal
    const downloadedDone = makeTile({
      taskIndex: 1,
      status: "完成",
      title: "刚下载成图.png",
      path: "/tmp/fresh-1.png",
      fileUrl: "tulaimedia://localhost/file?t=fresh1&v=1",
    });
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "wash",
          running: false,
          activeCount: 0,
          activeRows: [],
          doneTiles: [downloadedDone],
          doneCount: 1,
          doneTasks: 1,
          resultsDoneOpen: true,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(screen.getByTestId("result-slot-1")).toBe(slot);
    expect(slot.querySelector("img")).toHaveAttribute("src", expect.stringContaining("fresh1"));
    expect(screen.getByTestId("done-dotmatrix-1")).not.toHaveAttribute("data-phase", "settled");

    // Switching mode (wash -> free -> wash) MUST NOT replay dot-matrix animation
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "free",
          running: false,
          doneTiles: [],
          resultsDoneOpen: true,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "wash",
          running: false,
          doneTiles: [downloadedDone],
          doneCount: 1,
          doneTasks: 1,
          resultsDoneOpen: true,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(screen.getByTestId("done-dotmatrix-1")).toHaveAttribute("data-phase", "settled");

    // Unmounting & remounting WorkbenchPage (switching sidebar tabs) MUST NOT replay dot-matrix animation even while running
    unmount();
    renderWorkbench(
      makeWorkbench({
        mode: "wash",
        running: true,
        activeCount: 1,
        activeRows: [
          {
            key: "r2",
            header: false,
            issue: false,
            title: "工位A",
            summary: "",
            tiles: [makeTile({ taskIndex: 2, status: "进行中", phase: "generating", fileUrl: "" })],
          },
        ],
        doneTiles: [downloadedDone],
        doneCount: 1,
        doneTasks: 1,
        resultsDoneOpen: true,
      }),
    );
    expect(screen.getByTestId("done-dotmatrix-1")).toHaveAttribute("data-phase", "settled");
  });

  it("keeps completed thumbnails image-only and shows name, elapsed time, and qualityNote in enlarged hover preview", () => {
    vi.useFakeTimers();
    try {
      const cleanTile = makeTile({
        taskIndex: 1,
        title: "干净图.png",
        accountName: "1",
        elapsed: "耗时 19秒",
      });
      const warnTile = makeTile({
        taskIndex: 2,
        title: "留白图.png",
        accountName: "2",
        elapsed: "耗时 22秒",
        qualityNote: "边缘疑似留白/黑边(left/right)",
      });

      render(
        <TooltipProvider>
          <WorkbenchPage />
          <HoverPreviewHost />
        </TooltipProvider>,
      );
      act(() => {
        useAppStore.setState({
          workbench: makeWorkbench({
            doneTiles: [cleanTile, warnTile],
            doneCount: 2,
            doneTasks: 2,
            resultsDoneOpen: true,
          }),
        });
      });

      const card1 = screen.getByTestId("result-tile-card-1");
      const card2 = screen.getByTestId("result-tile-card-2");

      // Completed thumbnails show only the image — no title, elapsed, or quality badge/note text
      expect(within(card1).queryByText("干净图.png")).not.toBeInTheDocument();
      expect(within(card1).queryByText(/耗时/)).not.toBeInTheDocument();
      expect(within(card2).queryByText("留白图.png")).not.toBeInTheDocument();
      expect(within(card2).queryByText(/耗时/)).not.toBeInTheDocument();
      expect(screen.queryByTestId("quality-badge-2")).not.toBeInTheDocument();
      expect(screen.queryByTestId("quality-note-2")).not.toBeInTheDocument();

      // Hovering over a completed thumbnail shows title, elapsed, badge, and qualityNote in HoverPreview after 900ms
      const hoverTarget2 = card2.parentElement!;
      fireEvent.mouseEnter(hoverTarget2);
      act(() => {
        vi.advanceTimersByTime(890);
      });
      expect(screen.queryByTestId("hover-preview-card")).not.toBeInTheDocument();
      act(() => {
        vi.advanceTimersByTime(20);
      });
      const hoverCard = screen.getByTestId("hover-preview-card");
      expect(hoverCard).toHaveTextContent("留白图.png");
      expect(hoverCard).toHaveTextContent("质检提醒");
      expect(hoverCard).toHaveTextContent("2 · 耗时 22秒");
      expect(screen.getByTestId("hover-preview-note")).toHaveTextContent("边缘疑似留白/黑边(left/right)");

      act(() => {
        clearHoverPreview();
      });
    } finally {
      vi.useRealTimers();
    }
  });

  it("shows metadata and qualityNote inside Lightbox preview and hides mother-image reset button in skipImages fission mode", () => {
    const warnTile = makeTile({
      taskIndex: 1,
      title: "质检大图.png",
      accountName: "1",
      elapsed: "耗时 19秒",
      path: "/tmp/warn-out.png",
      fileUrl: "tulaimedia://localhost/file?t=warn1",
      qualityNote: "画面与源图几乎一致(pHash差1位)",
    });

    renderWorkbench(
      makeWorkbench({
        mode: "fission",
        skipImages: true,
        prompts: "",
        previewOn: true,
        previewPath: "/tmp/warn-out.png",
        previewUrl: "tulaimedia://localhost/file?t=warn1",
        doneTiles: [warnTile],
        doneCount: 1,
        doneTasks: 1,
      }),
    );

    expect(screen.getByTestId("lightbox-dialog")).toHaveTextContent("质检大图.png");
    expect(screen.getByTestId("lightbox-meta")).toHaveTextContent("豆包 · 1 · 耗时 19秒");
    expect(screen.getByTestId("lightbox-quality-note")).toHaveTextContent("质检提醒");
    expect(screen.getByTestId("lightbox-quality-note")).toHaveTextContent("画面与源图几乎一致(pHash差1位)");
    expect(screen.queryByRole("button", { name: "默认词" })).not.toBeInTheDocument();
    expect(screen.getByTestId("wb-prompt-editor")).toHaveAttribute(
      "placeholder",
      "输入裂变主题（自动展开为 4 个方向），或每行 1 个方向共 4 行...",
    );
  });

  it("hides rewash controls and batch select when canRewash is false (free/fission modes)", () => {
    const nonWashTile = makeTile({
      taskIndex: 1,
      title: "自由生图.png",
      path: "/tmp/free-1.png",
      fileUrl: "tulaimedia://localhost/file?t=free1",
      canRewash: false,
    });

    renderWorkbench(
      makeWorkbench({
        mode: "free",
        batchMode: 1,
        previewOn: true,
        previewPath: "/tmp/free-1.png",
        previewUrl: "tulaimedia://localhost/file?t=free1",
        doneTiles: [nonWashTile],
        doneCount: 1,
        doneTasks: 1,
        resultsDoneOpen: true,
      }),
    );

    expect(screen.queryByTestId("wb-batch-select-toggle")).not.toBeInTheDocument();
    expect(screen.queryByTestId("rewash-tile-1")).not.toBeInTheDocument();
    expect(screen.queryByTestId("done-select-1")).not.toBeInTheDocument();
    expect(screen.queryByTestId("lightbox-rewash-btn")).not.toBeInTheDocument();
    expect(screen.getByTestId("wb-prompt-editor")).toHaveAttribute(
      "placeholder",
      "输入统一改图描述（支持多行），将整段搭配下方每张参考图批量生成...",
    );

    // Shift+Click on non-rewashable done tile must open preview rather than entering batch selection
    const tileCard = screen.getByTestId("result-tile-card-1");
    const clickableArtwork = tileCard.querySelector('[role="button"]') as HTMLElement;
    expect(clickableArtwork).toBeTruthy();
    fireEvent.click(clickableArtwork, { shiftKey: true });
    expect(Workbench.setPreviewPath).toHaveBeenCalledWith("/tmp/free-1.png");
    expect(screen.queryByTestId("batch-rewash-btn")).not.toBeInTheDocument();
    expect(tileCard.className).not.toContain("bg-accent-soft");
  });

  it("refreshes workbench in parallel with shell on topic invalidation and follows backend tab switches", async () => {
    mockMyGoActive = true;
    try {
      useAppStore.setState((st) => ({
        shell: { ...st.shell, tab: TAB_WASH },
      }));

      let resolveShell!: (val: ReturnType<typeof useAppStore.getState>["shell"]) => void;
      const shellPending = new Promise<ReturnType<typeof useAppStore.getState>["shell"]>((res) => {
        resolveShell = res;
      });
      const getShellSpy = vi.spyOn(Core, "getShellState").mockReturnValueOnce(shellPending);
      const getWbSpy = vi
        .spyOn(Workbench, "getWorkbenchState")
        .mockResolvedValue(makeWorkbench({ mode: "wash" }));
      const getBrowserSpy = vi.spyOn(Browser, "getBrowserState").mockResolvedValue({
        backend: "fingerprint",
        backendLabel: "Fingerprint",
        engines: ["fingerprint"],
        ready: true,
        busy: false,
        badgeLabel: "就绪",
        badgeTone: "ok",
        status: "已就绪",
        detail: "",
        percent: 0,
        done: 0,
        total: 0,
        speed: 0,
        supported: true,
        running: false,
        rows: [],
        selectedId: 0,
        checkedCount: 0,
        batchActive: false,
        batchKind: "",
        batchText: "",
        missingHint: "",
        sharedExitHints: [],
        dlBrowserBusy: false,
        dlBrowserOpen: false,
        dlBrowserUrl: "",
      });

      const inflight = refreshTopics("shell,workbench");
      // Workbench fetch must start before getShellState resolves (no serial waterfall)
      expect(getShellSpy).toHaveBeenCalledTimes(1);
      expect(getWbSpy).toHaveBeenCalledTimes(1);

      resolveShell({ ...useAppStore.getState().shell, tab: TAB_BROWSER });
      await inflight;
      // Backend switched tab to TAB_BROWSER, so browser state is refreshed after shell resolves
      expect(getBrowserSpy).toHaveBeenCalledTimes(1);
    } finally {
      mockMyGoActive = false;
    }
  });

  it("resets prompt draft and done multi-select state cleanly when switching between Doubao and Qwen workspaces", () => {
    const doubaoSrc = makeSource({ index: 0, path: "/tmp/doubao.png", name: "豆包原图.png" });
    const doubaoDone = makeTile({
      taskIndex: 1,
      platform: "豆包",
      title: "豆包成图.png",
      path: "/tmp/doubao-out.png",
      canRewash: true,
    });
    const qwenSrc = makeSource({ index: 0, path: "/tmp/qwen.png", name: "千问原图.png" });
    const qwenDone = makeTile({
      taskIndex: 1,
      platform: "千问",
      title: "千问成图.png",
      path: "/tmp/qwen-out.png",
      canRewash: true,
    });

    const { rerender } = renderWorkbench(
      makeWorkbench({
        doubao: true,
        qianwen: false,
        prompts: "豆包洗图提示词",
        sources: [doubaoSrc],
        sourcesTotal: 1,
        doneTiles: [doubaoDone],
        doneCount: 1,
        doneTasks: 1,
        totalCount: 1,
        resultsDoneOpen: true,
      }),
    );

    expect(screen.getByTestId("wb-prompt-editor")).toHaveValue("豆包洗图提示词");
    expect(screen.getByText("豆包原图.png")).toBeInTheDocument();
    expect(screen.getByAltText("豆包成图.png")).toBeInTheDocument();

    // Enter multi-select mode and select tile #1 on Doubao
    fireEvent.click(screen.getByTestId("wb-batch-select-toggle"));
    fireEvent.click(screen.getByTestId("result-tile-image-1"));
    expect(screen.getByTestId("batch-rewash-btn")).toBeInTheDocument();

    // Switch to Qwen's independent page snapshot: prompt, sources, results update and multi-select resets
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          doubao: false,
          qianwen: true,
          prompts: "千问洗图提示词",
          sources: [qwenSrc],
          sourcesTotal: 1,
          doneTiles: [qwenDone],
          doneCount: 1,
          doneTasks: 1,
          totalCount: 1,
          resultsDoneOpen: true,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );

    expect(screen.getByTestId("wb-prompt-editor")).toHaveValue("千问洗图提示词");
    expect(screen.getByText("千问原图.png")).toBeInTheDocument();
    expect(screen.getByAltText("千问成图.png")).toBeInTheDocument();
    expect(screen.queryByText("豆包原图.png")).not.toBeInTheDocument();
    expect(screen.queryByAltText("豆包成图.png")).not.toBeInTheDocument();
    expect(screen.queryByTestId("batch-rewash-btn")).not.toBeInTheDocument();
  });

  it("optimizes 文生图 and 图生图 workflows: tabs, ratio labels, repeat count, TXT import, dedup, template detach, and pairing mode", () => {
    const pickTxtSpy = vi.spyOn(Workbench, "pickPromptTextFile").mockResolvedValue();
    const { rerender } = renderWorkbench(
      makeWorkbench({
        mode: "free",
        page: TAB_GEN,
        batchMode: 0,
        doubao: true,
        qianwen: false,
        prompts: "多啦A梦\n蜡笔小新\n多啦A梦",
        outputPreset: "原比例",
        outputPresets: ["原比例", "1:1（正方形）", "16:9（宽屏）"],
        sources: [makeSource({ index: 0, name: "参考图1.png" })],
        sourcesTotal: 1,
        repeatCount: 2,
        plannedTasks: 6,
        promptUnitCount: 3,
        startLabel: "开始文生图 (6)",
      }),
    );

    const tabs = screen.getByTestId("wb-batch-mode-tabs");
    expect(within(tabs).getByRole("tab", { name: "文生图" })).toHaveAttribute("aria-selected", "true");
    expect(within(tabs).getByRole("tab", { name: /图生图/ })).toHaveTextContent("1");
    expect(screen.queryByTestId("wb-sources-card")).not.toBeInTheDocument();

    // 文生图 shows 自动比例 instead of 原比例, and ignores 图生图 staged sources in empty state
    expect(screen.getByTestId("wb-output-preset-select")).toHaveTextContent("自动比例");
    expect(screen.getByText("输出尺寸")).toBeInTheDocument();
    expect(screen.getByText("已就绪，共 6 个文生图任务")).toBeInTheDocument();
    expect(screen.getByTestId("wb-prompt-meta-bar")).toHaveTextContent("3 条提示词");
    expect(screen.getAllByTestId("wb-prompt-line-num").map((el) => el.textContent).join("")).toBe("123");
    expect(screen.getByTestId("wb-planned-tasks")).toHaveTextContent("共 6 个任务");
    expect(screen.getByTestId("wb-start-btn")).toHaveTextContent("开始文生图 (6)");

    // Blank lines stay ignored in prompt count & numbered gutter
    fireEvent.change(screen.getByTestId("wb-prompt-editor"), {
      target: { value: "多啦A梦\n\n蜡笔小新\n多啦A梦" },
    });
    expect(screen.getByTestId("wb-prompt-meta-bar")).toHaveTextContent("3 条提示词");
    expect(screen.getAllByTestId("wb-prompt-line-num").map((el) => el.textContent).join("")).toBe("1·23");

    // Repeat count stepper
    fireEvent.click(screen.getByTestId("wb-repeat-inc"));
    expect(Workbench.setWorkbenchField).toHaveBeenCalledWith("repeatCount", "3");
    fireEvent.click(screen.getByTestId("wb-repeat-dec"));
    expect(Workbench.setWorkbenchField).toHaveBeenCalledWith("repeatCount", "1");

    // TXT import button & dedup lines (flushes pending edits before opening picker)
    fireEvent.click(screen.getByTestId("wb-import-txt-btn"));
    expect(Workbench.setWorkbenchField).toHaveBeenCalledWith("prompts", "多啦A梦\n\n蜡笔小新\n多啦A梦");
    expect(pickTxtSpy).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByTestId("wb-dedup-prompts-btn"));
    expect(Workbench.setWorkbenchField).toHaveBeenCalledWith("prompts", "多啦A梦\n蜡笔小新");

    // Template lock state exposes 转为逐行 action and unlocks immediately upon editing
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "free",
          page: TAB_GEN,
          batchMode: 0,
          prompts: "完整模板第一段\n第二段",
          templateLocked: true,
          plannedTasks: 1,
          promptUnitCount: 1,
          doubao: false,
          qianwen: true,
          outputPreset: "原比例",
          outputPresets: ["原比例", "1:1（正方形）", "16:9（宽屏）", "自定义尺寸"],
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(screen.getByTestId("wb-output-preset-select")).toHaveTextContent("自动比例");
    expect(screen.getByText("输出尺寸")).toBeInTheDocument();
    expect(screen.getByTestId("wb-prompt-meta-bar")).toHaveTextContent("整段发送 · 1 条");
    fireEvent.click(screen.getByTestId("wb-detach-template-btn"));
    expect(Workbench.setWorkbenchField).toHaveBeenCalledWith("detachTemplate", "");

    // Editing a locked template immediately restores line-by-line counting & gutter
    fireEvent.change(screen.getByTestId("wb-prompt-editor"), {
      target: { value: "完整模板第一段\n第二段\n第三段" },
    });
    expect(screen.getByTestId("wb-prompt-meta-bar")).toHaveTextContent("3 条提示词");
    expect(screen.getAllByTestId("wb-prompt-line-num").map((el) => el.textContent).join("")).toBe("123");

    // Switch to 图生图 on Qianwen: shows 参考图 card, unified 原比例 label, custom size inputs, and 统一描述/逐行配对 toggle
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "free",
          page: TAB_GEN,
          batchMode: 1,
          doubao: false,
          qianwen: true,
          prompts: "水彩插画风\n油画厚涂风",
          imagePromptMode: "unified",
          sources: [makeSource({ index: 0, name: "参考图1.png" })],
          sourcesTotal: 1,
          outputPreset: "原比例",
          outputPresets: ["原比例", "1:1（正方形）", "16:9（宽屏）", "自定义尺寸"],
          repeatCount: 2,
          plannedTasks: 2,
          startLabel: "开始图生图 (2)",
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(screen.getByTestId("wb-sources-card")).toBeInTheDocument();
    expect(screen.getByText("参考图 (1)")).toBeInTheDocument();
    expect(screen.getByTestId("wb-output-preset-select")).toHaveTextContent("原比例");
    expect(screen.getByTestId("wb-repeat-row")).toHaveTextContent("每图次数");
    expect(screen.getByTestId("wb-start-btn")).toHaveTextContent("开始图生图 (2)");
    fireEvent.click(screen.getByTestId("wb-img-prompt-mode-btn"));
    expect(Workbench.setWorkbenchField).toHaveBeenCalledWith("imagePromptMode", "per_line");

    // Custom dimensions & crop inputs are available on Qianwen identically to Doubao
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "wash",
          page: TAB_WASH,
          doubao: false,
          qianwen: true,
          outputPreset: "自定义尺寸",
          outputPresets: ["原比例", "自定义尺寸"],
          outputWidth: "1200",
          outputHeight: "900",
          outputCrop: true,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    expect(screen.getByTestId("wb-output-custom-row")).toBeInTheDocument();
    expect(screen.getByTestId("wb-output-width")).toHaveValue("1200");
    expect(screen.getByTestId("wb-output-height")).toHaveValue("900");
    expect(screen.getByTestId("wb-output-crop")).toBeChecked();
  });

  it("provides copy, put-back-to-source, send-to-edit, and remove actions in result tile right-click menu", async () => {
    mockMyGoActive = true;
    const tile = makeTile({
      taskIndex: 7,
      title: "多啦A梦_01.png",
      path: "/tmp/doraemon.png",
      prompt: "可爱的蓝色机器猫 3D 手办",
      canRewash: false,
      canReplace: false,
    });

    const { rerender } = renderWorkbench(
      makeWorkbench({
        mode: "free",
        page: TAB_GEN,
        batchMode: 0,
        running: false,
        doneTiles: [tile],
        doneCount: 1,
        doneTasks: 1,
        resultsDoneOpen: true,
      }),
    );

    const card = screen.getByTestId("result-tile-card-7");

    // 1. Copy image & verify menu labels (uses 「移除」 rather than 「删除」)
    fireEvent.contextMenu(card);
    expect(await screen.findByText("复制图片")).toBeInTheDocument();
    expect(screen.getByText("复制路径")).toBeInTheDocument();
    expect(screen.getByText("复制提示词")).toBeInTheDocument();
    expect(screen.getByText("作为参考图")).toBeInTheDocument();
    expect(screen.getByText("发送到图片编辑")).toBeInTheDocument();
    expect(screen.getByText("移除")).toBeInTheDocument();
    expect(screen.queryByText("删除图片")).not.toBeInTheDocument();
    expect(screen.queryByText("大图预览")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("复制图片"));
    expect(Core.copyImage).toHaveBeenCalledWith("/tmp/doraemon.png");

    // 2. Copy path & prompt
    fireEvent.contextMenu(card);
    fireEvent.click(await screen.findByText("复制路径"));
    expect(Core.copyText).toHaveBeenCalledWith("/tmp/doraemon.png");

    fireEvent.contextMenu(card);
    fireEvent.click(await screen.findByText("复制提示词"));
    expect(Core.copyText).toHaveBeenCalledWith("可爱的蓝色机器猫 3D 手办");

    // 3. Put generated image back to reference images for continued generation
    fireEvent.contextMenu(card);
    fireEvent.click(await screen.findByText("作为参考图"));
    expect(Workbench.addWorkbenchSources).toHaveBeenCalledWith(["/tmp/doraemon.png"]);

    // 4. Send to ImageEdit
    fireEvent.contextMenu(card);
    fireEvent.click(await screen.findByText("发送到图片编辑"));
    await act(async () => {
      await Promise.resolve();
    });
    expect(ImageEdit.editOpenPath).toHaveBeenCalledWith("/tmp/doraemon.png");
    expect(Core.setTab).toHaveBeenCalledWith(TAB_IMAGE_EDIT);

    // 5. Remove from result list only (keeps local file, trashFile = false)
    fireEvent.contextMenu(card);
    fireEvent.click(await screen.findByText("移除"));
    expect(Workbench.deleteWorkbenchResult).toHaveBeenCalledWith(7, "/tmp/doraemon.png", false);

    // 6. While running, mutating items (移除 / 放回参考图) are hidden
    act(() => {
      useAppStore.setState({
        workbench: makeWorkbench({
          mode: "free",
          page: TAB_GEN,
          batchMode: 0,
          running: true,
          doneTiles: [tile],
          doneCount: 1,
          doneTasks: 1,
          resultsDoneOpen: true,
        }),
      });
    });
    rerender(
      <TooltipProvider>
        <WorkbenchPage />
      </TooltipProvider>,
    );
    fireEvent.contextMenu(screen.getByTestId("result-tile-card-7"));
    expect(await screen.findByText("复制图片")).toBeInTheDocument();
    expect(screen.queryByText("移除")).not.toBeInTheDocument();
    expect(screen.queryByText("作为参考图")).not.toBeInTheDocument();
  });

  it("isolates 千问国际 lanes and dispatches concurrencyAutoQwenIntl / concurrencyMaxQwenIntl", () => {
    const setSettingSpy = vi.spyOn(Settings, "setSettingField").mockResolvedValue(undefined);
    renderWorkbench(
      makeWorkbench({
        mode: "free",
        page: TAB_GEN,
        platform: "千问国际",
        doubao: false,
        qianwen: false,
        qwenIntl: true,
        concurrencyAuto: true,
        concurrencyMax: 3,
        lanes: [
          makeLane({ id: 10, platform: "千问", title: "国内千问号" }),
          makeLane({ id: 20, platform: "千问国际", title: "国际站主号" }),
        ],
      }),
    );

    expect(screen.getByTestId("lane-row-20")).toBeInTheDocument();
    expect(screen.queryByTestId("lane-row-10")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("lanes-concurrency-inc"));
    expect(setSettingSpy).toHaveBeenCalledWith("concurrencyMaxQwenIntl", "4");

    fireEvent.click(screen.getByTestId("lanes-concurrency-switch"));
    expect(setSettingSpy).toHaveBeenCalledWith("concurrencyAutoQwenIntl", "false");
  });
});
