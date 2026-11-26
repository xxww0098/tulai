import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ImageEditStage,
  buildCssFilter,
  clampCropBox,
  clampSplit,
  clampZoom,
  computeCenteredAspectCrop,
  drawCropBox,
  moveCropBox,
  resizeCropBox,
} from "@/components/imageedit/ImageEditStage";
import { TooltipProvider } from "@/components/ui";
import { ImageEdit } from "@/lib/ipc";
import { dispatchTopEscape } from "@/lib/shortcuts";
import { ImageEditPage } from "@/pages/ImageEditPage";
import { resetStoreForTests, useAppStore, type ImageEditStateDTO } from "@/store";

beforeAll(() => {
  // jsdom does not ship PointerEvent; polyfill from MouseEvent so clientX/clientY/button work
  if (typeof window !== "undefined" && typeof window.PointerEvent === "undefined") {
    class MockPointerEvent extends MouseEvent {
      pointerId: number;
      constructor(type: string, params: PointerEventInit = {}) {
        super(type, params);
        this.pointerId = params.pointerId ?? 1;
      }
    }
    (window as unknown as { PointerEvent: typeof MockPointerEvent }).PointerEvent =
      MockPointerEvent;
    (globalThis as unknown as { PointerEvent: typeof MockPointerEvent }).PointerEvent =
      MockPointerEvent;
  }

  if (typeof globalThis.ResizeObserver === "undefined") {
    class MockResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    globalThis.ResizeObserver = MockResizeObserver as unknown as typeof ResizeObserver;
  }

  Object.defineProperty(window, "innerWidth", {
    configurable: true,
    writable: true,
    value: 1280,
  });

  Object.defineProperty(HTMLElement.prototype, "clientHeight", {
    configurable: true,
    get() {
      return this.hasAttribute("data-index") ? 56 : 520;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get() {
      return this.hasAttribute("data-index") ? 56 : 520;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "clientWidth", {
    configurable: true,
    get() {
      return 720;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    get() {
      return 720;
    },
  });
  if (!HTMLElement.prototype.scrollIntoView) {
    HTMLElement.prototype.scrollIntoView = () => {};
  }
  Element.prototype.getBoundingClientRect = function () {
    const h = this.hasAttribute("data-index") ? 56 : 520;
    return {
      x: 0,
      y: 0,
      left: 0,
      top: 0,
      right: 720,
      bottom: h,
      width: 720,
      height: h,
      toJSON: () => ({}),
    } as DOMRect;
  };
});

function renderImageEditPage() {
  return render(
    <TooltipProvider>
      <ImageEditPage />
    </TooltipProvider>,
  );
}

function makeImageEditState(overrides: Partial<ImageEditStateDTO> = {}): ImageEditStateDTO {
  return {
    files: [
      {
        index: 0,
        path: "/tmp/hero.png",
        name: "hero.png",
        fileUrl: "tulaimedia://localhost/file?t=hero&v=1",
        status: "pending",
        err: "",
        output: "",
        outUrl: "",
        picked: false,
        hasCustomCrop: false,
      },
      {
        index: 1,
        path: "/tmp/detail.jpg",
        name: "detail.jpg",
        fileUrl: "tulaimedia://localhost/file?t=detail&v=2",
        status: "ok",
        err: "",
        output: "/tmp/out/detail_edited.jpg",
        outUrl: "tulaimedia://localhost/file?t=detail_out&v=2",
        picked: false,
        hasCustomCrop: false,
      },
    ],
    selected: 0,
    pickedCount: 0,
    hasSelection: true,
    selectedName: "hero.png",
    srcW: 1000,
    srcH: 800,
    outW: 1000,
    outH: 800,
    origVersion: 1,
    outVersion: 1,
    origUrl: "tulaimedia://localhost/edit/orig?v=1",
    previewUrl: "tulaimedia://localhost/edit/preview?v=1",
    showCropUi: false,
    cropAspect: 0,
    previewPending: false,
    zoom: 1,
    compare: false,
    split: 50,
    formatEnabled: true,
    cropEnabled: true,
    adjustEnabled: true,
    formatOpen: true,
    cropOpen: true,
    adjustOpen: true,
    format: "original",
    quality: "90",
    uniformCrop: true,
    customCropCount: 0,
    mode: "crop",
    dimTab: 0,
    ratioTab: 0,
    dimW: "800",
    dimH: "800",
    cropRatio: "",
    rotate: 0,
    flipH: false,
    flipV: false,
    brightness: 0,
    contrast: 0,
    saturation: 0,
    blur: 0,
    sharpen: false,
    grayscale: false,
    outDir: "/tmp/out",
    suffix: "_edited",
    notice: "",
    running: false,
    progressDone: 0,
    progressTotal: 0,
    progressCurrent: "",
    okCount: 0,
    failCount: 0,
    resultOpen: false,
    recipeOpen: false,
    recipeErr: "",
    chips: [{ id: "format", label: "品质·90" }],
    summaryFormat: "品质 90",
    summaryCrop: "裁剪 800×800",
    summaryAdjust: "亮度 / 对比 / 饱和 / 锐化",
    sizePresets: [
      { label: "自由调整", w: 0, h: 0 },
      { label: "方形主图 800×800", w: 800, h: 800 },
      { label: "竖版主图 800×1200", w: 800, h: 1200 },
    ],
    ratioPresets: [
      { ratio: "1:1", label: "正方形" },
      { ratio: "16:9", label: "视频横屏" },
    ],
    myRatios: [],
    mySizes: [{ label: "", w: 600, h: 600 }],
    recipes: [
      { index: 0, name: "电商主图" },
      { index: 1, name: "高对比夜景" },
    ],
    ...overrides,
  };
}

let spies: Array<{ mockRestore: () => void }> = [];

beforeEach(() => {
  spies = [
    vi.spyOn(ImageEdit, "editPickFiles").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editPickFolder").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editAddFromSources").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editAddPaths").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editSelectIndex").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editTogglePick").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editClearPicks").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editSelectAllPicks").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editInvertPicks").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editPickByStatus").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editRemovePicked").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editRemoveIndex").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editClearFiles").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editClearAll").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editSetField").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editSetFeature").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editToggleExpanded").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editResetFeature").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editPickRatio").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editApplyDims").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editSaveMyRatio").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editSaveMySize").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editSetCropBox").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editClearCropBox").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editSyncCropTo").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editClearCustomCrops").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editZoomStep").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editResetView").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editPickOutDir").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editOpenOutDir").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editSaveRecipe").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editApplyRecipe").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editDeleteRecipe").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editRunBatch").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editCancelBatch").mockResolvedValue(undefined),
    vi.spyOn(ImageEdit, "editRetryFailed").mockResolvedValue(undefined),
  ];
});

afterEach(() => {
  cleanup();
  resetStoreForTests();
  for (const s of spies) s.mockRestore();
  spies = [];
  vi.useRealTimers();
});

describe("ImageEditStage geometry & filter math", () => {
  it("normalizes split to 5..95 and zoom to 0.1..8.0", () => {
    expect(clampSplit(0.5)).toBe(5);
    expect(clampSplit(-10)).toBe(5);
    expect(clampSplit(50)).toBe(50);
    expect(clampSplit(99)).toBe(95);

    expect(clampZoom(0.01)).toBe(0.1);
    expect(clampZoom(1.5)).toBe(1.5);
    expect(clampZoom(12)).toBe(8.0);
  });

  it("clamps crop boxes to source pixels and >= 8px minimum", () => {
    expect(clampCropBox({ x: -20, y: 795, w: 300, h: 100 }, 1000, 800)).toEqual({
      x: 0,
      y: 700,
      w: 300,
      h: 100,
    });
    expect(clampCropBox({ x: 998, y: 799, w: 2, h: 3 }, 1000, 800)).toEqual({
      x: 992,
      y: 792,
      w: 8,
      h: 8,
    });
  });

  it("moves crop box without exceeding image edges", () => {
    const start = { x: 100, y: 100, w: 400, h: 300 };
    expect(moveCropBox(start, -250, 900, 1000, 800)).toEqual({
      x: 0,
      y: 500,
      w: 400,
      h: 300,
    });
  });

  it("resizes crop box across corner and edge handles with and without aspect lock", () => {
    const start = { x: 200, y: 150, w: 400, h: 300 };

    // Free resize on NW handle
    const nwFree = resizeCropBox(start, "nw", -50, -30, 1000, 800, 0);
    expect(nwFree).toEqual({ x: 150, y: 120, w: 450, h: 330 });

    // Aspect-locked 16:9 resize on SE handle keeps ratio and stays in bounds
    const ratio16x9 = 16 / 9;
    const seLocked = resizeCropBox(
      { x: 100, y: 100, w: 320, h: 180 },
      "se",
      2000,
      2000,
      1000,
      800,
      ratio16x9,
    );
    expect(seLocked.x).toBe(100);
    expect(seLocked.y).toBe(100);
    expect(seLocked.x + seLocked.w).toBeLessThanOrEqual(1000);
    expect(seLocked.y + seLocked.h).toBeLessThanOrEqual(800);
    expect(Math.abs(seLocked.w / seLocked.h - ratio16x9)).toBeLessThan(0.02);

    // Aspect-locked edge handle (E) expands symmetrically in Y
    const eLocked = resizeCropBox(
      { x: 200, y: 310, w: 320, h: 180 },
      "e",
      160,
      0,
      1000,
      800,
      ratio16x9,
    );
    expect(Math.round(eLocked.w)).toBe(480);
    expect(Math.round(eLocked.h)).toBe(270);
    expect(Math.abs(eLocked.w / eLocked.h - ratio16x9)).toBeLessThan(0.02);
  });

  it("draws new crop boxes in reversed quadrants and computes centered ratio crop", () => {
    const drawn = drawCropBox(600, 500, 200, 100, 1000, 800, 1);
    expect(drawn).toEqual({ x: 200, y: 100, w: 400, h: 400 });

    const centered = computeCenteredAspectCrop(1000, 800, 16 / 9);
    expect(centered.w).toBe(1000);
    expect(centered.h).toBe(563);
    expect(centered.x).toBe(0);
    expect(centered.y).toBe(119);
  });

  it("builds absolute and delta CSS filters matching Go tone order", () => {
    const abs = buildCssFilter({
      brightness: 20,
      contrast: -10,
      saturation: 30,
      blur: 2,
      grayscale: true,
    });
    expect(abs).toBe("brightness(1.200) saturate(1.300) contrast(0.900) blur(2.00px) grayscale(1)");

    const delta = buildCssFilter(
      { brightness: 50, contrast: 0, saturation: 0, blur: 4, grayscale: false },
      { brightness: 20, contrast: 0, saturation: 0, blur: 1, grayscale: false },
    );
    expect(delta).toBe("brightness(1.250) blur(3.00px)");
  });
});

describe("ImageEditPage darkroom studio, split 5..95 fix, inspector, and dialogs", () => {
  it("renders 3-column darkroom studio and normalizes compare split slider to 5..95%", () => {
    useAppStore.setState({
      imageEdit: makeImageEditState({
        compare: true,
        split: 62,
      }),
    });

    renderImageEditPage();

    const inspector = screen.getByTestId("edit-inspector");
    const mainArea = screen.getByTestId("edit-workspace-main");
    const canvas = screen.getByTestId("edit-canvas");
    const filesStrip = screen.getByTestId("edit-files");

    expect(inspector).toBeInTheDocument();
    expect(canvas).toBeInTheDocument();
    expect(filesStrip).toBeInTheDocument();
    expect(mainArea).toContainElement(canvas);
    expect(mainArea).toContainElement(filesStrip);

    // Inspector is placed on the left (before mainArea), canvas above bottom carousel
    expect(
      inspector.compareDocumentPosition(mainArea) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      canvas.compareDocumentPosition(filesStrip) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    expect(screen.getByTestId("edit-dims-pill")).toHaveTextContent("1000×800 → 1000×800");
    expect(screen.getByTestId("edit-zoom-label")).toHaveTextContent("100%");

    // Verify split slider uses 5..95% instead of legacy broken 0.05..0.95
    const splitSliderRoot = screen.getByTestId("edit-split-slider");
    const thumb = within(splitSliderRoot).getByRole("slider");
    expect(thumb).toHaveAttribute("aria-valuemin", "5");
    expect(thumb).toHaveAttribute("aria-valuemax", "95");
    expect(thumb).toHaveAttribute("aria-valuenow", "62");
    expect(screen.getByTestId("edit-split-value")).toHaveTextContent("62%");

    // Before/after split curtain renders with orig & preview clip
    expect(screen.getByTestId("edit-split-handle")).toBeInTheDocument();
    expect(screen.getByTestId("edit-compare-orig-clip")).toHaveStyle({
      clipPath: "inset(0 38% 0 0)",
    });

    // Keyboard stepping on split slider commits normalized integer 5..95 to Go IPC
    fireEvent.keyDown(thumb, { key: "ArrowRight" });
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("split", "63");
    expect(ImageEdit.editSelectIndex).not.toHaveBeenCalled();
  });

  it("syncs dragging the split curtain handle directly on canvas to 5..95% bounds", () => {
    const origRect = Element.prototype.getBoundingClientRect;
    Element.prototype.getBoundingClientRect = function () {
      return {
        x: 0,
        y: 0,
        left: 0,
        top: 0,
        right: 720,
        bottom: 520,
        width: 720,
        height: 520,
        toJSON: () => ({}),
      } as DOMRect;
    };

    try {
      useAppStore.setState({
        imageEdit: makeImageEditState({
          compare: true,
          split: 50,
        }),
      });

      renderImageEditPage();

      const handle = screen.getByTestId("edit-split-handle");
      // Drag far to the right -> must clamp at 95%
      fireEvent.pointerDown(handle, { button: 0, clientX: 360, clientY: 260, pointerId: 1 });
      fireEvent.pointerMove(window, { clientX: 900, clientY: 260, pointerId: 1 });
      fireEvent.pointerUp(window, { clientX: 900, clientY: 260, pointerId: 1 });

      expect(screen.getByTestId("edit-split-value")).toHaveTextContent("95%");
      expect(ImageEdit.editSetField).toHaveBeenCalledWith("split", "95");
    } finally {
      Element.prototype.getBoundingClientRect = origRect;
    }
  });

  it("wires inspector crop, adjust, format controls, and keyboard shortcuts to ImageEdit IPC", () => {
    useAppStore.setState({
      imageEdit: makeImageEditState(),
    });

    renderImageEditPage();

    // Crop card controls
    fireEvent.click(screen.getByLabelText("逆时针旋转"));
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("rotateLeft", "1");

    fireEvent.click(screen.getByLabelText("顺时针旋转"));
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("rotateRight", "1");

    fireEvent.click(screen.getByLabelText("水平翻转"));
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("flipH", "1");

    fireEvent.click(screen.getByLabelText("垂直翻转"));
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("flipV", "1");

    fireEvent.click(screen.getByRole("button", { name: "正方形" }));
    expect(ImageEdit.editPickRatio).toHaveBeenCalledWith("1:1");

    // Custom saved size pill triggers editApplyDims
    fireEvent.click(screen.getByRole("button", { name: "600×600" }));
    expect(ImageEdit.editApplyDims).toHaveBeenCalledWith("crop", 600, 600, "");

    // Apply current W × H dims
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    expect(ImageEdit.editApplyDims).toHaveBeenCalledWith("crop", 800, 800, "");

    fireEvent.click(screen.getByLabelText("保存为我的常用尺寸"));
    expect(ImageEdit.editSaveMySize).toHaveBeenCalledTimes(1);

    // Adjust card: brightness slider updates live CSS filter immediately and commits
    const brightnessThumb = within(screen.getByTestId("edit-slider-brightness")).getByRole(
      "slider",
    );
    fireEvent.keyDown(brightnessThumb, { key: "ArrowRight" });
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("brightness", "1");
    expect(ImageEdit.editSelectIndex).not.toHaveBeenCalled();
    expect(screen.getByTestId("edit-stage-preview-img")).toHaveAttribute(
      "data-live-filter",
      expect.stringContaining("brightness("),
    );

    // Sharpen & grayscale checkboxes
    fireEvent.click(screen.getByLabelText("轻度锐化"));
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("sharpen", "true");

    fireEvent.click(screen.getByLabelText("黑白灰度"));
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("grayscale", "true");

    // Format card directory controls
    fireEvent.click(screen.getByRole("button", { name: "更改" }));
    expect(ImageEdit.editPickOutDir).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "打开" }));
    expect(ImageEdit.editOpenOutDir).toHaveBeenCalledTimes(1);

    // Keyboard shortcuts: 'c' toggles compare, 'ArrowDown' selects next filmstrip image
    fireEvent.keyDown(window, { key: "c" });
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("compare", "true");

    fireEvent.keyDown(window, { key: "ArrowDown" });
    expect(ImageEdit.editSelectIndex).toHaveBeenCalledWith(1);
  });

  it("handles recipe dialog and batch export result dialog with priority-40 Escape stack", () => {
    useAppStore.setState({
      imageEdit: makeImageEditState({
        recipeOpen: true,
      }),
    });

    renderImageEditPage();
    const recipeDialog = screen.getByTestId("edit-recipe-dialog");
    expect(recipeDialog).toBeInTheDocument();
    expect(within(recipeDialog).getByText("电商主图")).toBeInTheDocument();

    const nameInput = within(recipeDialog).getByLabelText("新配方名称");
    fireEvent.change(nameInput, { target: { value: "小红书竖图" } });
    fireEvent.click(within(recipeDialog).getByRole("button", { name: "保存当前" }));
    expect(ImageEdit.editSaveRecipe).toHaveBeenCalledWith("小红书竖图");

    const applyButtons = within(recipeDialog).getAllByRole("button", { name: "套用" });
    fireEvent.click(applyButtons[0]);
    expect(ImageEdit.editApplyRecipe).toHaveBeenCalledWith(0);

    const deleteButtons = within(recipeDialog).getAllByRole("button", { name: "删除" });
    fireEvent.click(deleteButtons[1]);
    expect(ImageEdit.editDeleteRecipe).toHaveBeenCalledWith(1);

    // Priority Escape stack closes recipe dialog
    expect(dispatchTopEscape()).toBe(true);
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("recipeOpen", "false");

    // Switch to batch export result dialog
    act(() => {
      useAppStore.setState({
        imageEdit: makeImageEditState({
          recipeOpen: false,
          resultOpen: true,
          okCount: 4,
          failCount: 1,
          outDir: "/tmp/out",
          files: [
            {
              index: 0,
              path: "/tmp/bad.png",
              name: "bad.png",
              fileUrl: "tulaimedia://localhost/file?t=bad&v=1",
              status: "failed",
              err: "解码失败",
              output: "",
              outUrl: "",
              picked: false,
              hasCustomCrop: false,
            },
          ],
        }),
      });
    });

    const resultDialog = screen.getByTestId("edit-result-dialog");
    expect(resultDialog).toBeInTheDocument();
    expect(within(resultDialog).getByText("/tmp/out")).toBeInTheDocument();
    expect(within(resultDialog).getByText("bad.png")).toBeInTheDocument();
    expect(within(resultDialog).getByText("解码失败")).toBeInTheDocument();

    fireEvent.click(within(resultDialog).getByRole("button", { name: "重试失败" }));
    expect(ImageEdit.editRetryFailed).toHaveBeenCalledTimes(1);

    expect(dispatchTopEscape()).toBe(true);
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("resultOpen", "false");
  });

  it("supports filmstrip search/status filtering and batch pick controls", () => {
    useAppStore.setState({
      imageEdit: makeImageEditState({
        pickedCount: 1,
        failCount: 1,
        customCropCount: 1,
        files: [
          {
            index: 0,
            path: "/tmp/hero.png",
            name: "hero.png",
            fileUrl: "tulaimedia://localhost/file?t=hero&v=1",
            status: "pending",
            err: "",
            output: "",
            outUrl: "",
            picked: false,
            hasCustomCrop: false,
          },
          {
            index: 1,
            path: "/tmp/detail.jpg",
            name: "detail.jpg",
            fileUrl: "tulaimedia://localhost/file?t=detail&v=2",
            status: "ok",
            err: "",
            output: "/tmp/out/detail_edited.jpg",
            outUrl: "tulaimedia://localhost/file?t=detail_out&v=2",
            picked: true,
            hasCustomCrop: true,
          },
          {
            index: 2,
            path: "/tmp/broken.webp",
            name: "broken.webp",
            fileUrl: "tulaimedia://localhost/file?t=broken&v=3",
            status: "failed",
            err: "不支持的色彩配置",
            output: "",
            outUrl: "",
            picked: false,
            hasCustomCrop: false,
          },
        ],
      }),
    });

    renderImageEditPage();

    // Toolbar displays picked export scope
    expect(screen.getByText("仅导出已选 1 张")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "批量导出 (1)" })).toBeInTheDocument();

    const filmstrip = screen.getByTestId("edit-files");
    expect(within(filmstrip).getByTestId("edit-file-item-0")).toBeInTheDocument();
    expect(within(filmstrip).getByTestId("edit-file-item-1")).toBeInTheDocument();
    expect(within(filmstrip).getByTestId("edit-file-item-2")).toBeInTheDocument();
    expect(within(within(filmstrip).getByTestId("edit-file-item-1")).getByText("独立")).toBeInTheDocument();

    // Filename search & #N index lookup
    const searchInput = within(filmstrip).getByLabelText("筛选文件名");
    fireEvent.change(searchInput, { target: { value: "detail" } });
    expect(within(filmstrip).queryByTestId("edit-file-item-0")).not.toBeInTheDocument();
    expect(within(filmstrip).getByTestId("edit-file-item-1")).toBeInTheDocument();
    expect(within(filmstrip).queryByTestId("edit-file-item-2")).not.toBeInTheDocument();

    fireEvent.change(searchInput, { target: { value: "#3" } });
    expect(within(filmstrip).queryByTestId("edit-file-item-1")).not.toBeInTheDocument();
    expect(within(filmstrip).getByTestId("edit-file-item-2")).toBeInTheDocument();

    fireEvent.click(within(filmstrip).getByLabelText("清空筛选关键词"));
    expect(within(filmstrip).getByTestId("edit-file-item-0")).toBeInTheDocument();
    expect(within(filmstrip).getByTestId("edit-file-item-1")).toBeInTheDocument();
    expect(within(filmstrip).getByTestId("edit-file-item-2")).toBeInTheDocument();

    // Status filter pills (失败 / 独立裁剪 / 全部)
    fireEvent.click(within(filmstrip).getByRole("button", { name: /失败 1/ }));
    expect(within(filmstrip).queryByTestId("edit-file-item-0")).not.toBeInTheDocument();
    expect(within(filmstrip).getByTestId("edit-file-item-2")).toBeInTheDocument();

    fireEvent.click(within(filmstrip).getByRole("button", { name: /独立裁剪 1/ }));
    expect(within(filmstrip).queryByTestId("edit-file-item-2")).not.toBeInTheDocument();
    expect(within(filmstrip).getByTestId("edit-file-item-1")).toBeInTheDocument();

    fireEvent.click(within(filmstrip).getByRole("button", { name: /全部 3/ }));
    expect(within(filmstrip).getByTestId("edit-file-item-0")).toBeInTheDocument();

    // Direct pick checkbox on item #0 and batch pick bar buttons
    fireEvent.click(
      within(within(filmstrip).getByTestId("edit-file-item-0")).getByLabelText("多选此图"),
    );
    expect(ImageEdit.editTogglePick).toHaveBeenCalledWith(0, false, true);

    fireEvent.click(within(filmstrip).getByRole("button", { name: "全选" }));
    expect(ImageEdit.editSelectAllPicks).toHaveBeenCalledTimes(1);

    fireEvent.click(within(filmstrip).getByRole("button", { name: "反选" }));
    expect(ImageEdit.editInvertPicks).toHaveBeenCalledTimes(1);

    fireEvent.click(within(filmstrip).getByRole("button", { name: "选中失败" }));
    expect(ImageEdit.editPickByStatus).toHaveBeenCalledWith("failed");

    fireEvent.click(within(filmstrip).getByRole("button", { name: "清空选区" }));
    expect(ImageEdit.editClearPicks).toHaveBeenCalledTimes(1);

    fireEvent.click(within(filmstrip).getByRole("button", { name: "移除已选" }));
    expect(ImageEdit.editRemovePicked).toHaveBeenCalledTimes(1);

    // Bottom horizontal carousel prev/next buttons and collapse toggle
    const carouselNext = within(filmstrip).getByTestId("edit-carousel-next");
    const carouselPrev = within(filmstrip).getByTestId("edit-carousel-prev");
    expect(carouselPrev).toBeDisabled();
    expect(carouselNext).not.toBeDisabled();
    fireEvent.click(carouselNext);
    expect(ImageEdit.editSelectIndex).toHaveBeenCalledWith(1);

    fireEvent.click(within(filmstrip).getByLabelText("折叠胶片列表"));
    expect(within(filmstrip).queryByTestId("edit-filmstrip-list")).not.toBeInTheDocument();
    fireEvent.click(within(filmstrip).getByLabelText("展开胶片列表"));
    expect(within(filmstrip).getByTestId("edit-filmstrip-list")).toBeInTheDocument();
  });

  it("toggles Contact Sheet batch grid, syncs filters, handles multi-select, double-click inspect, and keyboard shortcuts", () => {
    useAppStore.setState({
      imageEdit: makeImageEditState({
        pickedCount: 1,
        failCount: 1,
        customCropCount: 1,
        files: [
          {
            index: 0,
            path: "/tmp/hero.png",
            name: "hero.png",
            fileUrl: "tulaimedia://localhost/file?t=hero&v=1",
            status: "pending",
            err: "",
            output: "",
            outUrl: "",
            picked: false,
            hasCustomCrop: false,
          },
          {
            index: 1,
            path: "/tmp/detail.jpg",
            name: "detail.jpg",
            fileUrl: "tulaimedia://localhost/file?t=detail&v=2",
            status: "ok",
            err: "",
            output: "/tmp/out/detail_edited.jpg",
            outUrl: "tulaimedia://localhost/file?t=detail_out&v=2",
            picked: true,
            hasCustomCrop: true,
          },
          {
            index: 2,
            path: "/tmp/broken.webp",
            name: "broken.webp",
            fileUrl: "tulaimedia://localhost/file?t=broken&v=3",
            status: "failed",
            err: "解码失败",
            output: "",
            outUrl: "",
            picked: false,
            hasCustomCrop: false,
          },
        ],
      }),
    });

    renderImageEditPage();

    // Stage context pill shortcuts: pick & next image
    const contextPill = screen.getByTestId("edit-stage-context-pill");
    expect(contextPill).toHaveTextContent("#1/3");
    expect(contextPill).toHaveTextContent("hero.png");
    fireEvent.click(screen.getByTestId("edit-stage-pick-btn"));
    expect(ImageEdit.editTogglePick).toHaveBeenCalledWith(0, false, true);

    // P shortcut toggles pick on current image, ArrowRight navigates to next in darkroom
    fireEvent.keyDown(window, { key: "p" });
    expect(ImageEdit.editTogglePick).toHaveBeenCalledTimes(2);
    expect(ImageEdit.editTogglePick).toHaveBeenLastCalledWith(0, false, true);

    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(ImageEdit.editSelectIndex).toHaveBeenCalledWith(1);

    // Switch to Contact Sheet via toolbar toggle
    fireEvent.click(screen.getByTestId("edit-mode-grid"));
    const batchGrid = screen.getByTestId("edit-batch-grid");
    expect(batchGrid).toBeInTheDocument();
    expect(screen.queryByTestId("edit-canvas")).not.toBeInTheDocument();

    // Filmstrip search stays synchronized with Contact Sheet search
    fireEvent.change(within(screen.getByTestId("edit-files")).getByLabelText("筛选文件名"), {
      target: { value: "broken" },
    });
    const gridSearch = within(batchGrid).getByLabelText("批览筛选文件名") as HTMLInputElement;
    expect(gridSearch.value).toBe("broken");
    expect(within(batchGrid).getByTestId("edit-grid-item-2")).toBeInTheDocument();
    expect(within(batchGrid).queryByTestId("edit-grid-item-0")).not.toBeInTheDocument();

    // Non-matching search shows empty mismatch state with "清空筛选"
    fireEvent.change(gridSearch, { target: { value: "does-not-exist" } });
    expect(within(batchGrid).getByText("没有符合筛选条件的图片")).toBeInTheDocument();
    fireEvent.click(within(batchGrid).getByRole("button", { name: "清空筛选" }));
    expect(within(batchGrid).getByTestId("edit-grid-item-0")).toBeInTheDocument();
    expect(within(batchGrid).getByTestId("edit-grid-item-1")).toBeInTheDocument();
    expect(within(batchGrid).getByTestId("edit-grid-item-2")).toBeInTheDocument();

    // Clearing via search clear button also stays synchronized
    fireEvent.change(gridSearch, { target: { value: "hero" } });
    expect((within(screen.getByTestId("edit-files")).getByLabelText("筛选文件名") as HTMLInputElement).value).toBe("hero");
    fireEvent.click(within(batchGrid).getByLabelText("清空批览搜索"));
    expect(within(batchGrid).getByTestId("edit-grid-item-1")).toBeInTheDocument();

    // ArrowRight in grid mode must NOT trigger darkroom next-image selection
    vi.mocked(ImageEdit.editSelectIndex).mockClear();
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(ImageEdit.editSelectIndex).not.toHaveBeenCalled();

    // Card multi-select via card checkbox & Cmd-click
    const card0 = within(batchGrid).getByTestId("edit-grid-item-0");
    fireEvent.click(within(card0).getByLabelText("多选此图"));
    expect(ImageEdit.editTogglePick).toHaveBeenCalledWith(0, false, true);

    const card2 = within(batchGrid).getByTestId("edit-grid-item-2");
    fireEvent.click(card2, { metaKey: true });
    expect(ImageEdit.editTogglePick).toHaveBeenCalledWith(2, false, true);

    // Grid batch action buttons
    fireEvent.click(within(batchGrid).getByRole("button", { name: "选中失败" }));
    expect(ImageEdit.editPickByStatus).toHaveBeenCalledWith("failed");

    fireEvent.click(within(batchGrid).getByRole("button", { name: "选中独立裁剪" }));
    expect(ImageEdit.editPickByStatus).toHaveBeenCalledWith("customCrop");

    fireEvent.click(within(batchGrid).getByRole("button", { name: "移除已选" }));
    expect(ImageEdit.editRemovePicked).toHaveBeenCalledTimes(1);

    // Double-clicking a card selects that file and returns to darkroom
    const card1 = within(batchGrid).getByTestId("edit-grid-item-1");
    fireEvent.doubleClick(card1);
    expect(ImageEdit.editSelectIndex).toHaveBeenCalledWith(1);
    expect(screen.getByTestId("edit-canvas")).toBeInTheDocument();
    expect(screen.queryByTestId("edit-batch-grid")).not.toBeInTheDocument();

    // G keyboard shortcut toggles back to grid, and priority-10 Escape returns to darkroom
    fireEvent.keyDown(window, { key: "g" });
    expect(screen.getByTestId("edit-batch-grid")).toBeInTheDocument();
    act(() => {
      expect(dispatchTopEscape()).toBe(true);
    });
    expect(screen.getByTestId("edit-canvas")).toBeInTheDocument();

    // Quick grid button inside darkroom toolbar also switches to grid
    fireEvent.click(screen.getByTestId("edit-canvas-grid-btn"));
    expect(screen.getByTestId("edit-batch-grid")).toBeInTheDocument();

    // Clearing files resets back to darkroom and disables toolbar view toggle
    act(() => {
      useAppStore.setState({
        imageEdit: makeImageEditState({
          files: [],
          selected: -1,
          hasSelection: false,
          selectedName: "",
          pickedCount: 0,
          customCropCount: 0,
        }),
      });
    });
    expect(screen.getByTestId("edit-stage-empty")).toBeInTheDocument();
    expect(screen.getByTestId("edit-mode-grid")).toBeDisabled();
  });

  it("wires inspector quick-recipe strip, crop sync, 2x2 tone double-click reset, and result pick-failed", () => {
    useAppStore.setState({
      imageEdit: makeImageEditState({
        pickedCount: 1,
        customCropCount: 1,
        uniformCrop: false,
        mode: "crop",
        cropRatio: "1:1",
        brightness: 25,
        contrast: -15,
        saturation: 10,
        blur: 3,
        files: [
          {
            index: 0,
            path: "/tmp/hero.png",
            name: "hero.png",
            fileUrl: "tulaimedia://localhost/file?t=hero&v=1",
            status: "pending",
            err: "",
            output: "",
            outUrl: "",
            picked: false,
            hasCustomCrop: false,
          },
          {
            index: 1,
            path: "/tmp/detail.jpg",
            name: "detail.jpg",
            fileUrl: "tulaimedia://localhost/file?t=detail&v=2",
            status: "failed",
            err: "编码失败",
            output: "",
            outUrl: "",
            picked: true,
            hasCustomCrop: true,
          },
        ],
      }),
    });

    renderImageEditPage();

    const inspector = screen.getByTestId("edit-inspector");

    // Batch bar scope & quick recipe strip
    const batchBar = within(inspector).getByTestId("edit-inspector-batch-bar");
    expect(within(batchBar).getByText("已多选 1 张")).toBeInTheDocument();
    fireEvent.click(within(batchBar).getByRole("button", { name: "清空选区" }));
    expect(ImageEdit.editClearPicks).toHaveBeenCalledTimes(1);

    fireEvent.click(within(batchBar).getByRole("button", { name: "电商主图" }));
    expect(ImageEdit.editApplyRecipe).toHaveBeenCalledWith(0);

    fireEvent.click(within(batchBar).getByRole("button", { name: "配方管理" }));
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("recipeOpen", "true");

    // Save custom ratio button
    fireEvent.click(within(inspector).getByRole("button", { name: "存为常用" }));
    expect(ImageEdit.editSaveMyRatio).toHaveBeenCalledTimes(1);

    // Per-image crop sync controls
    const syncBox = within(inspector).getByTestId("edit-crop-sync-box");
    expect(within(syncBox).getByText("自定义裁剪 1 张")).toBeInTheDocument();

    fireEvent.click(within(syncBox).getByRole("button", { name: "同步到已选" }));
    expect(ImageEdit.editSyncCropTo).toHaveBeenCalledWith("picked");

    fireEvent.click(within(syncBox).getByRole("button", { name: "同步到全部" }));
    expect(ImageEdit.editSyncCropTo).toHaveBeenCalledWith("all");

    fireEvent.click(within(syncBox).getByRole("button", { name: "恢复统一裁剪" }));
    expect(ImageEdit.editClearCustomCrops).toHaveBeenCalledWith("all");

    // 2x2 tone grid double-click label resets single adjust field to 0
    const adjustCard = within(inspector).getByTestId("edit-card-adjust");
    expect(within(adjustCard).getByText("+25")).toBeInTheDocument();
    expect(within(adjustCard).getByText("-15")).toBeInTheDocument();
    expect(within(adjustCard).getByText("+10")).toBeInTheDocument();
    expect(within(adjustCard).getByText("3.0")).toBeInTheDocument();

    fireEvent.doubleClick(within(adjustCard).getByText("亮度"));
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("brightness", "0");
    expect(within(adjustCard).getByText("0")).toBeInTheDocument();

    fireEvent.doubleClick(within(adjustCard).getByText("模糊"));
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("blur", "0");
    expect(within(adjustCard).getByText("0.0")).toBeInTheDocument();

    // ResultDialog "选中失败图片" picks failed files & closes dialog
    act(() => {
      const prev = useAppStore.getState().imageEdit!;
      useAppStore.setState({
        imageEdit: {
          ...prev,
          resultOpen: true,
          okCount: 1,
          failCount: 1,
          outDir: "/tmp/export_out",
        },
      });
    });

    const resultDialog = screen.getByTestId("edit-result-dialog");
    expect(within(resultDialog).getByText("/tmp/export_out")).toBeInTheDocument();
    fireEvent.click(within(resultDialog).getByRole("button", { name: "选中失败图片" }));
    expect(ImageEdit.editPickByStatus).toHaveBeenCalledWith("failed");
    expect(ImageEdit.editSetField).toHaveBeenCalledWith("resultOpen", "false");
  });

  it("handles IPC rejections outside MyGo runtime without unhandled rejections", async () => {
    const rejections: unknown[] = [];
    const onUnhandled = (e: PromiseRejectionEvent) => {
      rejections.push(e.reason);
    };
    window.addEventListener("unhandledrejection", onUnhandled);

    try {
      vi.mocked(ImageEdit.editRunBatch).mockRejectedValueOnce(new Error("mygo: runtime not found"));
      vi.mocked(ImageEdit.editOpenOutDir).mockRejectedValueOnce(new Error("mygo: runtime not found"));
      vi.mocked(ImageEdit.editSetField).mockRejectedValue(new Error("mygo: runtime not found"));

      useAppStore.setState({
        imageEdit: makeImageEditState({
          compare: true,
          split: 50,
        }),
      });

      renderImageEditPage();

      fireEvent.click(screen.getByRole("button", { name: "批量导出 (2)" }));
      fireEvent.click(screen.getByRole("button", { name: "打开" }));
      fireEvent.pointerDown(screen.getByTestId("edit-split-handle"), {
        button: 0,
        clientX: 360,
        clientY: 260,
        pointerId: 1,
      });
      fireEvent.pointerMove(window, { clientX: 400, clientY: 260, pointerId: 1 });
      fireEvent.pointerUp(window, { clientX: 400, clientY: 260, pointerId: 1 });

      await act(async () => {
        await new Promise((r) => setTimeout(r, 140));
      });

      expect(rejections).toHaveLength(0);
    } finally {
      window.removeEventListener("unhandledrejection", onUnhandled);
    }
  });

  it("renders interactive 8-handle crop overlay, commits clamped source pixel coordinates, and clears crop", () => {
    const onCommitCropBox = vi.fn();
    const onClearCropBox = vi.fn();
    const origRect = Element.prototype.getBoundingClientRect;
    Element.prototype.getBoundingClientRect = function () {
      return {
        x: 0,
        y: 0,
        left: 0,
        top: 0,
        right: 720,
        bottom: 520,
        width: 720,
        height: 520,
        toJSON: () => ({}),
      } as DOMRect;
    };

    try {
      render(
        <ImageEditStage
          hasSelection
          selectedKey="/tmp/hero.png"
          srcW={1000}
          srcH={800}
          outW={1000}
          outH={800}
          origUrl="tulaimedia://localhost/edit/orig?v=1"
          origVersion={1}
          previewUrl="tulaimedia://localhost/edit/preview?v=1"
          outVersion={1}
          previewPending={false}
          zoom={1}
          compare={false}
          split={50}
          showCropUi
          cropAspect={0}
          cropBox={{ x: 100, y: 100, w: 400, h: 300 }}
          hasGeometryTransform
          adjustEnabled={false}
          serverAdjust={{ brightness: 0, contrast: 0, saturation: 0, blur: 0, grayscale: false }}
          liveAdjust={{ brightness: 0, contrast: 0, saturation: 0, blur: 0, grayscale: false }}
          isLiveAdjusting={false}
          onZoomChange={vi.fn()}
          onResetView={vi.fn()}
          onSplitChange={vi.fn()}
          onSplitCommit={vi.fn()}
          onCommitCropBox={onCommitCropBox}
          onClearCropBox={onClearCropBox}
        />,
      );

      expect(screen.getByTestId("edit-crop-overlay")).toBeInTheDocument();
      for (const h of ["nw", "n", "ne", "e", "se", "s", "sw", "w"]) {
        expect(screen.getByTestId(`edit-crop-handle-${h}`)).toBeInTheDocument();
      }
      expect(screen.getByTestId("edit-crop-readout")).toHaveTextContent("400 × 300 px");

      // Drag SE handle way beyond canvas right/bottom edges -> must clamp cleanly within 1000×800
      const seHandle = screen.getByTestId("edit-crop-handle-se");
      fireEvent.pointerDown(seHandle, { button: 0, clientX: 300, clientY: 250, pointerId: 1 });
      fireEvent.pointerMove(window, { clientX: 1800, clientY: 1600, pointerId: 1 });
      fireEvent.pointerUp(window, { clientX: 1800, clientY: 1600, pointerId: 1 });

      expect(onCommitCropBox).toHaveBeenCalledTimes(1);
      const [cx, cy, cw, ch] = onCommitCropBox.mock.calls[0];
      expect(cx).toBe(100);
      expect(cy).toBe(100);
      expect(cx + cw).toBeLessThanOrEqual(1000);
      expect(cy + ch).toBeLessThanOrEqual(800);
      expect(cw).toBe(900);
      expect(ch).toBe(700);

      // Drag inside crop box to move -> preserves width/height and clamps to edge
      const moveRect = screen.getByTestId("edit-crop-move");
      fireEvent.pointerDown(moveRect, { button: 0, clientX: 250, clientY: 200, pointerId: 2 });
      fireEvent.pointerMove(window, { clientX: 50, clientY: 50, pointerId: 2 });
      fireEvent.pointerUp(window, { clientX: 50, clientY: 50, pointerId: 2 });

      expect(onCommitCropBox).toHaveBeenCalledTimes(2);
      const [mx, my, mw, mh] = onCommitCropBox.mock.calls[1];
      expect(mx).toBeGreaterThanOrEqual(0);
      expect(my).toBeGreaterThanOrEqual(0);
      expect(mw).toBe(900);
      expect(mh).toBe(700);

      // Clear crop button triggers editClearCropBox
      fireEvent.click(screen.getByTestId("edit-crop-clear"));
      expect(onClearCropBox).toHaveBeenCalledTimes(1);
    } finally {
      Element.prototype.getBoundingClientRect = origRect;
    }
  });

  it("supports wheel zoom, double-click fit reset, and zero-flicker preview swap", () => {
    const onZoomChange = vi.fn();
    const onResetView = vi.fn();
    const baseProps = {
      hasSelection: true,
      selectedKey: "/tmp/hero.png",
      srcW: 1000,
      srcH: 800,
      outW: 1000,
      outH: 800,
      origUrl: "tulaimedia://localhost/edit/orig?v=1",
      origVersion: 1,
      previewUrl: "tulaimedia://localhost/edit/preview?v=1",
      outVersion: 1,
      previewPending: false,
      zoom: 1,
      compare: false,
      split: 50,
      showCropUi: false,
      cropAspect: 0,
      hasGeometryTransform: false,
      adjustEnabled: true,
      serverAdjust: { brightness: 0, contrast: 0, saturation: 0, blur: 0, grayscale: false },
      liveAdjust: { brightness: 0, contrast: 0, saturation: 0, blur: 0, grayscale: false },
      isLiveAdjusting: false,
      onZoomChange,
      onResetView,
      onSplitChange: vi.fn(),
      onSplitCommit: vi.fn(),
      onCommitCropBox: vi.fn(),
      onClearCropBox: vi.fn(),
    };

    const { rerender, container } = render(<ImageEditStage {...baseProps} />);
    const stage = screen.getByTestId("image-edit-stage");

    // Wheel up zooms in above 1.0
    fireEvent.wheel(stage, { deltaY: -120, clientX: 360, clientY: 260 });
    expect(onZoomChange).toHaveBeenCalledTimes(1);
    expect(onZoomChange.mock.calls[0][0]).toBeGreaterThan(1);

    // Double-click resets/fits view
    fireEvent.doubleClick(stage);
    expect(onResetView).toHaveBeenCalledTimes(1);

    const visibleImg = screen.getByTestId("edit-stage-preview-img") as HTMLImageElement;
    expect(visibleImg.getAttribute("src")).toBe("tulaimedia://localhost/edit/preview?v=1");

    // Go finishes rendering v=2: visible image MUST keep v=1 until hidden preloader finishes load
    rerender(
      <ImageEditStage
        {...baseProps}
        previewUrl="tulaimedia://localhost/edit/preview?v=2"
        outVersion={2}
      />,
    );
    expect(screen.getByTestId("edit-stage-preview-img").getAttribute("src")).toBe(
      "tulaimedia://localhost/edit/preview?v=1",
    );

    const hiddenPreloader = container.querySelector(
      'img.hidden[src="tulaimedia://localhost/edit/preview?v=2"]',
    );
    expect(hiddenPreloader).not.toBeNull();
    fireEvent.load(hiddenPreloader!);

    // Now visible image atomically swaps to v=2
    expect(screen.getByTestId("edit-stage-preview-img").getAttribute("src")).toBe(
      "tulaimedia://localhost/edit/preview?v=2",
    );
  });
});
