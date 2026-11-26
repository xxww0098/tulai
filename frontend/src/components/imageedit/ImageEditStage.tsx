import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type WheelEvent as ReactWheelEvent,
} from "react";
import { MoveHorizontal, X } from "lucide-react";
import { Button, Icon, Tooltip, TooltipGroup } from "@/components/ui";
import { getEditOrigUrl, getEditPreviewUrl } from "@/lib/assets";
import { cn } from "@/lib/cn";
import type { EditCropBoxDTO } from "@/store";

export type CropHandle = "nw" | "n" | "ne" | "e" | "se" | "s" | "sw" | "w";

export interface LiveAdjustValues {
  brightness: number;
  contrast: number;
  saturation: number;
  blur: number;
  grayscale: boolean;
}

export interface ImageEditStageProps {
  hasSelection: boolean;
  selectedKey: string;
  srcW: number;
  srcH: number;
  outW: number;
  outH: number;
  origUrl: string;
  origVersion: number;
  previewUrl: string;
  outVersion: number;
  previewPending: boolean;
  zoom: number;
  compare: boolean;
  split: number;
  showCropUi: boolean;
  cropAspect: number;
  cropRatio?: string;
  cropBox?: EditCropBoxDTO;
  hasGeometryTransform: boolean;
  adjustEnabled: boolean;
  serverAdjust: LiveAdjustValues;
  liveAdjust: LiveAdjustValues;
  isLiveAdjusting: boolean;
  onZoomChange: (nextZoom: number) => void;
  onResetView: () => void;
  onSplitChange: (pct: number) => void;
  onSplitCommit: (pct: number) => void;
  onCommitCropBox: (x: number, y: number, w: number, h: number) => void;
  onClearCropBox: () => void;
  onPickFiles?: () => void;
  fileName?: string;
  fileIndex?: number;
  totalFiles?: number;
  isPicked?: boolean;
  hasCustomCrop?: boolean;
  uniformCrop?: boolean;
  onTogglePickCurrent?: () => void;
  onPrevFile?: () => void;
  onNextFile?: () => void;
  className?: string;
}

export const MIN_ZOOM = 0.1;
export const MAX_ZOOM = 8.0;
export const MIN_SPLIT = 5;
export const MAX_SPLIT = 95;
export const MIN_CROP_SIZE = 8;

const STAGE_PADDING = 24;

export function clampZoom(zoom: number): number {
  if (!Number.isFinite(zoom) || zoom <= 0) return 1;
  return Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, zoom));
}

export function clampSplit(pct: number): number {
  if (!Number.isFinite(pct)) return 50;
  return Math.max(MIN_SPLIT, Math.min(MAX_SPLIT, pct));
}

/**
 * Clamps a crop box to source-image pixel bounds `[0..srcW, 0..srcH]`
 * while enforcing minimum size `minSize` (matching Go's `editSetCropBox` >= 8px).
 */
export function clampCropBox(
  box: EditCropBoxDTO,
  srcW: number,
  srcH: number,
  minSize = MIN_CROP_SIZE,
): EditCropBoxDTO {
  const safeW = Math.max(1, srcW);
  const safeH = Math.max(1, srcH);
  const effMinW = Math.min(minSize, safeW);
  const effMinH = Math.min(minSize, safeH);

  let x = Number.isFinite(box.x) ? box.x : 0;
  let y = Number.isFinite(box.y) ? box.y : 0;
  let w = Number.isFinite(box.w) ? box.w : effMinW;
  let h = Number.isFinite(box.h) ? box.h : effMinH;

  w = Math.max(effMinW, Math.min(safeW, w));
  h = Math.max(effMinH, Math.min(safeH, h));
  x = Math.max(0, Math.min(safeW - w, x));
  y = Math.max(0, Math.min(safeH - h, y));

  return { x, y, w, h };
}

/**
 * Computes the centered maximum crop rectangle matching `aspect = rw / rh`
 * inside `(srcW, srcH)` (matches Go `processor.go` centered ratio crop).
 */
export function computeCenteredAspectCrop(
  srcW: number,
  srcH: number,
  aspect: number,
): EditCropBoxDTO {
  if (srcW <= 0 || srcH <= 0 || !Number.isFinite(aspect) || aspect <= 0) {
    return { x: 0, y: 0, w: Math.max(0, srcW), h: Math.max(0, srcH) };
  }
  let w = srcW;
  let h = srcH;
  if (srcW / srcH > aspect) {
    h = srcH;
    w = Math.max(1, Math.min(srcW, Math.round(srcH * aspect)));
  } else {
    w = srcW;
    h = Math.max(1, Math.min(srcH, Math.round(srcW / aspect)));
  }
  return {
    x: Math.max(0, Math.round((srcW - w) / 2)),
    y: Math.max(0, Math.round((srcH - h) / 2)),
    w,
    h,
  };
}

export function moveCropBox(
  start: EditCropBoxDTO,
  dx: number,
  dy: number,
  srcW: number,
  srcH: number,
): EditCropBoxDTO {
  const safeW = Math.max(1, srcW);
  const safeH = Math.max(1, srcH);
  const w = Math.max(1, Math.min(safeW, start.w));
  const h = Math.max(1, Math.min(safeH, start.h));
  const x = Math.max(0, Math.min(safeW - w, start.x + dx));
  const y = Math.max(0, Math.min(safeH - h, start.y + dy));
  return { x, y, w, h };
}

export function fitToAspect(
  rawW: number,
  rawH: number,
  maxW: number,
  maxH: number,
  aspect: number,
  minSize: number,
  preferWidth: boolean | null,
): { w: number; h: number } {
  const safeMaxW = Math.max(1, maxW);
  const safeMaxH = Math.max(1, maxH);
  let w = Math.max(1, rawW);
  let h = Math.max(1, rawH);

  if (preferWidth === true) {
    h = w / aspect;
  } else if (preferWidth === false) {
    w = h * aspect;
  } else {
    if (w / Math.max(1, h) > aspect) {
      h = w / aspect;
    } else {
      w = h * aspect;
    }
  }

  // Enforce minimum dimensions while preserving aspect
  const minW = Math.min(safeMaxW, Math.max(minSize, minSize * aspect));
  const minH = Math.min(safeMaxH, Math.max(minSize, minSize / aspect));
  if (w < minW) {
    w = minW;
    h = w / aspect;
  }
  if (h < minH) {
    h = minH;
    w = h * aspect;
  }

  // Clamp to available bounds while preserving aspect
  if (w > safeMaxW) {
    w = safeMaxW;
    h = w / aspect;
  }
  if (h > safeMaxH) {
    h = safeMaxH;
    w = h * aspect;
  }
  if (w > safeMaxW) {
    w = safeMaxW;
  }
  if (h > safeMaxH) {
    h = safeMaxH;
  }

  return { w: Math.max(1, w), h: Math.max(1, h) };
}

/**
 * Resizes a crop box from any of the 8 handles (`nw, n, ne, e, se, s, sw, w`),
 * clamping strictly within `[0..srcW, 0..srcH]` and honoring `aspect > 0`.
 */
export function resizeCropBox(
  start: EditCropBoxDTO,
  handle: CropHandle,
  dx: number,
  dy: number,
  srcW: number,
  srcH: number,
  aspect: number,
  minSize = MIN_CROP_SIZE,
): EditCropBoxDTO {
  const safeW = Math.max(1, srcW);
  const safeH = Math.max(1, srcH);
  const minW = Math.min(minSize, safeW);
  const minH = Math.min(minSize, safeH);

  const left = start.x;
  const top = start.y;
  const right = start.x + start.w;
  const bottom = start.y + start.h;

  const hasW = handle.includes("w");
  const hasE = handle.includes("e");
  const hasN = handle.includes("n");
  const hasS = handle.includes("s");

  // Free resize (no locked aspect ratio)
  if (!Number.isFinite(aspect) || aspect <= 0) {
    let nextL = left;
    let nextR = right;
    let nextT = top;
    let nextB = bottom;

    if (hasW) {
      nextL = Math.max(0, Math.min(right - minW, left + dx));
    }
    if (hasE) {
      nextR = Math.min(safeW, Math.max(left + minW, right + dx));
    }
    if (hasN) {
      nextT = Math.max(0, Math.min(bottom - minH, top + dy));
    }
    if (hasS) {
      nextB = Math.min(safeH, Math.max(top + minH, bottom + dy));
    }

    return clampCropBox(
      { x: nextL, y: nextT, w: nextR - nextL, h: nextB - nextT },
      safeW,
      safeH,
      minSize,
    );
  }

  // Aspect-locked corner handles
  if ((hasW || hasE) && (hasN || hasS)) {
    const anchorX = hasW ? right : left;
    const anchorY = hasN ? bottom : top;
    const maxW = hasW ? anchorX : safeW - anchorX;
    const maxH = hasN ? anchorY : safeH - anchorY;

    const rawW = hasW ? anchorX - (left + dx) : right + dx - anchorX;
    const rawH = hasN ? anchorY - (top + dy) : bottom + dy - anchorY;
    const fitted = fitToAspect(
      Math.max(minW, rawW),
      Math.max(minH, rawH),
      maxW,
      maxH,
      aspect,
      minSize,
      null,
    );

    const x = hasW ? anchorX - fitted.w : anchorX;
    const y = hasN ? anchorY - fitted.h : anchorY;
    return clampCropBox({ x, y, w: fitted.w, h: fitted.h }, safeW, safeH, minSize);
  }

  // Aspect-locked horizontal edge handles (e / w): anchor opposite X edge, center Y
  if (hasW || hasE) {
    const anchorX = hasW ? right : left;
    const centerY = (top + bottom) / 2;
    const maxW = hasW ? anchorX : safeW - anchorX;
    const rawW = hasW ? anchorX - (left + dx) : right + dx - anchorX;

    const fitted = fitToAspect(
      Math.max(minW, rawW),
      Math.max(minH, rawW / aspect),
      maxW,
      safeH,
      aspect,
      minSize,
      true,
    );
    const x = hasW ? anchorX - fitted.w : anchorX;
    const y = Math.max(0, Math.min(safeH - fitted.h, centerY - fitted.h / 2));
    return clampCropBox({ x, y, w: fitted.w, h: fitted.h }, safeW, safeH, minSize);
  }

  // Aspect-locked vertical edge handles (n / s): anchor opposite Y edge, center X
  const anchorY = hasN ? bottom : top;
  const centerX = (left + right) / 2;
  const maxH = hasN ? anchorY : safeH - anchorY;
  const rawH = hasN ? anchorY - (top + dy) : bottom + dy - anchorY;

  const fitted = fitToAspect(
    Math.max(minW, rawH * aspect),
    Math.max(minH, rawH),
    safeW,
    maxH,
    aspect,
    minSize,
    false,
  );
  const x = Math.max(0, Math.min(safeW - fitted.w, centerX - fitted.w / 2));
  const y = hasN ? anchorY - fitted.h : anchorY;
  return clampCropBox({ x, y, w: fitted.w, h: fitted.h }, safeW, safeH, minSize);
}

/**
 * Computes a newly drawn crop box from `(anchorX, anchorY)` to `(curX, curY)`
 * in source-image pixel coordinates, clamped to `[0..srcW, 0..srcH]`.
 */
export function drawCropBox(
  anchorX: number,
  anchorY: number,
  curX: number,
  curY: number,
  srcW: number,
  srcH: number,
  aspect: number,
): EditCropBoxDTO {
  const safeW = Math.max(1, srcW);
  const safeH = Math.max(1, srcH);
  const ax = Math.max(0, Math.min(safeW, anchorX));
  const ay = Math.max(0, Math.min(safeH, anchorY));
  const cx = Math.max(0, Math.min(safeW, curX));
  const cy = Math.max(0, Math.min(safeH, curY));

  const dirX = cx >= ax ? 1 : -1;
  const dirY = cy >= ay ? 1 : -1;
  let w = Math.abs(cx - ax);
  let h = Math.abs(cy - ay);

  if (Number.isFinite(aspect) && aspect > 0 && (w > 0 || h > 0)) {
    const maxW = dirX > 0 ? safeW - ax : ax;
    const maxH = dirY > 0 ? safeH - ay : ay;
    if (w / Math.max(1e-6, h) > aspect) {
      h = w / aspect;
    } else {
      w = h * aspect;
    }
    if (w > maxW) {
      w = maxW;
      h = w / aspect;
    }
    if (h > maxH) {
      h = maxH;
      w = h * aspect;
    }
  }

  const x = dirX > 0 ? ax : Math.max(0, ax - w);
  const y = dirY > 0 ? ay : Math.max(0, ay - h);
  return { x, y, w, h };
}

/**
 * Builds a CSS `filter` string closely matching `internal/imageedit/tone.go`.
 * When `baked` is provided, computes a safe delta filter over an already-rendered preview.
 */
export function buildCssFilter(
  values: LiveAdjustValues,
  baked?: LiveAdjustValues | null,
): string {
  const parts: string[] = [];

  if (!baked) {
    if (values.brightness !== 0) {
      parts.push(`brightness(${Math.max(0, 1 + values.brightness / 100).toFixed(3)})`);
    }
    if (values.saturation !== 0) {
      parts.push(`saturate(${Math.max(0, 1 + values.saturation / 100).toFixed(3)})`);
    }
    if (values.contrast !== 0) {
      parts.push(`contrast(${Math.max(0, 1 + values.contrast / 100).toFixed(3)})`);
    }
    if (values.blur > 0) {
      parts.push(`blur(${Math.min(24, values.blur).toFixed(2)}px)`);
    }
    if (values.grayscale) {
      parts.push("grayscale(1)");
    }
    return parts.join(" ");
  }

  if (values.brightness !== baked.brightness) {
    const baseF = Math.max(0.1, 1 + baked.brightness / 100);
    const targetF = Math.max(0, 1 + values.brightness / 100);
    parts.push(`brightness(${(targetF / baseF).toFixed(3)})`);
  }
  if (values.saturation !== baked.saturation) {
    const baseF = Math.max(0.1, 1 + baked.saturation / 100);
    const targetF = Math.max(0, 1 + values.saturation / 100);
    parts.push(`saturate(${(targetF / baseF).toFixed(3)})`);
  }
  if (values.contrast !== baked.contrast) {
    const baseF = Math.max(0.1, 1 + baked.contrast / 100);
    const targetF = Math.max(0, 1 + values.contrast / 100);
    parts.push(`contrast(${(targetF / baseF).toFixed(3)})`);
  }
  if (values.blur > baked.blur) {
    parts.push(`blur(${Math.min(24, values.blur - baked.blur).toFixed(2)}px)`);
  }
  if (values.grayscale && !baked.grayscale) {
    parts.push("grayscale(1)");
  }
  return parts.join(" ");
}

const HANDLE_CURSORS: Record<CropHandle, string> = {
  nw: "nwse-resize",
  n: "ns-resize",
  ne: "nesw-resize",
  e: "ew-resize",
  se: "nwse-resize",
  s: "ns-resize",
  sw: "nesw-resize",
  w: "ew-resize",
};

const ALL_HANDLES: CropHandle[] = ["nw", "n", "ne", "e", "se", "s", "sw", "w"];

interface CropDragSession {
  kind: "move" | "draw" | "resize";
  handle?: CropHandle;
  startClientX: number;
  startClientY: number;
  anchorImgX: number;
  anchorImgY: number;
  startBox: EditCropBoxDTO;
}

interface PanDragSession {
  startClientX: number;
  startClientY: number;
  originPanX: number;
  originPanY: number;
}

export function ImageEditStage({
  hasSelection,
  selectedKey,
  srcW,
  srcH,
  outW,
  outH,
  origUrl,
  origVersion,
  previewUrl,
  outVersion,
  previewPending,
  zoom,
  compare,
  split,
  showCropUi,
  cropAspect,
  cropRatio,
  cropBox,
  hasGeometryTransform,
  adjustEnabled,
  serverAdjust,
  liveAdjust,
  isLiveAdjusting,
  onZoomChange,
  onResetView,
  onSplitChange,
  onSplitCommit,
  onCommitCropBox,
  onClearCropBox,
  onPickFiles,
  fileName,
  fileIndex,
  totalFiles,
  isPicked,
  hasCustomCrop,
  uniformCrop,
  onTogglePickCurrent,
  onPrevFile,
  onNextFile,
  className,
}: ImageEditStageProps) {
  const stageRef = useRef<HTMLDivElement | null>(null);
  const [viewport, setViewport] = useState({ width: 720, height: 520 });
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const [spacePanning, setSpacePanning] = useState(false);
  const [isPanning, setIsPanning] = useState(false);
  const [isDraggingSplit, setIsDraggingSplit] = useState(false);

  // Zero-flicker double-buffered image URLs
  const resolvedOrigUrl = useMemo(
    () => getEditOrigUrl(origUrl, origVersion),
    [origUrl, origVersion],
  );
  const resolvedPreviewUrl = useMemo(
    () => getEditPreviewUrl(previewUrl, outVersion),
    [previewUrl, outVersion],
  );

  const [committedOrigUrl, setCommittedOrigUrl] = useState<string>(resolvedOrigUrl);
  const [committedPreviewUrl, setCommittedPreviewUrl] = useState<string>(resolvedPreviewUrl);
  const [naturalOrig, setNaturalOrig] = useState<{ w: number; h: number }>({ w: 0, h: 0 });
  const [naturalPreview, setNaturalPreview] = useState<{ w: number; h: number }>({ w: 0, h: 0 });

  // Reset pan & buffered preview when switching to a different image file
  const lastSelectedKeyRef = useRef(selectedKey);
  useEffect(() => {
    if (lastSelectedKeyRef.current !== selectedKey) {
      lastSelectedKeyRef.current = selectedKey;
      setPan({ x: 0, y: 0 });
      setCommittedOrigUrl(resolvedOrigUrl);
      setCommittedPreviewUrl(resolvedPreviewUrl);
      setNaturalOrig({ w: 0, h: 0 });
      setNaturalPreview({ w: 0, h: 0 });
    }
  }, [selectedKey, resolvedOrigUrl, resolvedPreviewUrl]);

  // Keep committed URLs initialized if they were empty
  useEffect(() => {
    if (!committedOrigUrl && resolvedOrigUrl) {
      setCommittedOrigUrl(resolvedOrigUrl);
    }
    if (!resolvedOrigUrl) {
      setCommittedOrigUrl("");
    }
  }, [committedOrigUrl, resolvedOrigUrl]);

  useEffect(() => {
    if (!committedPreviewUrl && resolvedPreviewUrl) {
      setCommittedPreviewUrl(resolvedPreviewUrl);
    }
    if (!resolvedPreviewUrl) {
      setCommittedPreviewUrl("");
    }
  }, [committedPreviewUrl, resolvedPreviewUrl]);

  // Measure viewport via ResizeObserver
  useEffect(() => {
    const el = stageRef.current;
    if (!el) return;

    const update = () => {
      const rect = el.getBoundingClientRect();
      if (rect.width > 0 && rect.height > 0) {
        setViewport((prev) =>
          Math.abs(prev.width - rect.width) > 0.5 || Math.abs(prev.height - rect.height) > 0.5
            ? { width: rect.width, height: rect.height }
            : prev,
        );
      }
    };
    update();

    if (typeof ResizeObserver !== "undefined") {
      const ro = new ResizeObserver(() => update());
      ro.observe(el);
      return () => ro.disconnect();
    }
    window.addEventListener("resize", update);
    return () => window.removeEventListener("resize", update);
  }, []);

  // Spacebar temporary pan mode (even while crop box is active)
  useEffect(() => {
    const isEditable = (target: EventTarget | null) => {
      if (!(target instanceof HTMLElement)) return false;
      const tag = target.tagName;
      return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable;
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.code === "Space" && !e.repeat && !isEditable(e.target)) {
        setSpacePanning(true);
      }
    };
    const onKeyUp = (e: KeyboardEvent) => {
      if (e.code === "Space") {
        setSpacePanning(false);
      }
    };
    const onBlur = () => setSpacePanning(false);
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("keyup", onKeyUp);
    window.addEventListener("blur", onBlur);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("keyup", onKeyUp);
      window.removeEventListener("blur", onBlur);
    };
  }, []);

  // Reset pan when zoom resets to 1 via external reset
  const prevZoomRef = useRef(zoom);
  useEffect(() => {
    if (zoom === 1 && prevZoomRef.current !== 1) {
      setPan({ x: 0, y: 0 });
    }
    prevZoomRef.current = zoom;
  }, [zoom]);

  const isCropMode = Boolean(hasSelection && showCropUi && !compare);
  const safeZoom = clampZoom(zoom);
  const safeSplit = clampSplit(split);

  const effectiveSrcW = srcW > 0 ? srcW : naturalOrig.w || outW || 1000;
  const effectiveSrcH = srcH > 0 ? srcH : naturalOrig.h || outH || 1000;
  const effectiveOutW = outW > 0 ? outW : naturalPreview.w || effectiveSrcW;
  const effectiveOutH = outH > 0 ? outH : naturalPreview.h || effectiveSrcH;

  const artW = isCropMode ? effectiveSrcW : effectiveOutW;
  const artH = isCropMode ? effectiveSrcH : effectiveOutH;

  // Compute fitted base size & screen-space artwork rect
  const { baseW, baseH, imgLeft, imgTop, imgW, imgH } = useMemo(() => {
    const availW = Math.max(60, viewport.width - STAGE_PADDING * 2);
    const availH = Math.max(60, viewport.height - STAGE_PADDING * 2);
    const safeArtW = Math.max(1, artW);
    const safeArtH = Math.max(1, artH);
    const fitScale = Math.min(availW / safeArtW, availH / safeArtH);
    const bW = safeArtW * fitScale;
    const bH = safeArtH * fitScale;
    const w = bW * safeZoom;
    const h = bH * safeZoom;
    const left = (viewport.width - w) / 2 + pan.x;
    const top = (viewport.height - h) / 2 + pan.y;
    return { baseW: bW, baseH: bH, imgLeft: left, imgTop: top, imgW: w, imgH: h };
  }, [viewport.width, viewport.height, artW, artH, safeZoom, pan.x, pan.y]);

  const clampPanOffset = useCallback(
    (px: number, py: number, nextZoom: number) => {
      const maxX = Math.max(0, viewport.width / 2 + (baseW * nextZoom) / 2 - 48);
      const maxY = Math.max(0, viewport.height / 2 + (baseH * nextZoom) / 2 - 48);
      return {
        x: Math.max(-maxX, Math.min(maxX, px)),
        y: Math.max(-maxY, Math.min(maxY, py)),
      };
    },
    [viewport.width, viewport.height, baseW, baseH],
  );

  // ---- Interactive Crop Box State ----
  const [draftCrop, setDraftCrop] = useState<EditCropBoxDTO | null>(null);
  const draftCropRef = useRef<EditCropBoxDTO | null>(null);
  draftCropRef.current = draftCrop;

  const cropDragRef = useRef<CropDragSession | null>(null);
  const panDragRef = useRef<PanDragSession | null>(null);
  const splitDragActiveRef = useRef(false);

  // Effective crop box to render: local live drag > server cropBox > centered aspect preset
  const activeCropBox = useMemo<EditCropBoxDTO | null>(() => {
    if (!isCropMode) return null;
    if (draftCrop) return draftCrop;
    if (cropBox && cropBox.w > 0 && cropBox.h > 0) {
      return clampCropBox(cropBox, effectiveSrcW, effectiveSrcH);
    }
    if (cropAspect > 0) {
      return computeCenteredAspectCrop(effectiveSrcW, effectiveSrcH, cropAspect);
    }
    return null;
  }, [isCropMode, draftCrop, cropBox, cropAspect, effectiveSrcW, effectiveSrcH]);

  // Clear local draft when server cropBox updates cleanly after commit
  useEffect(() => {
    if (!cropDragRef.current) {
      setDraftCrop(null);
    }
  }, [cropBox?.x, cropBox?.y, cropBox?.w, cropBox?.h, selectedKey, showCropUi]);

  // ---- Cursor-Anchored Wheel Zoom ----
  const handleWheel = useCallback(
    (e: ReactWheelEvent<HTMLDivElement>) => {
      if (!hasSelection) return;
      e.preventDefault();
      const el = stageRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      const cx = e.clientX - rect.left - viewport.width / 2;
      const cy = e.clientY - rect.top - viewport.height / 2;
      const factor = Math.exp(-e.deltaY * 0.0018);
      const nextZoom = clampZoom(safeZoom * factor);
      const ratio = nextZoom / safeZoom;
      const rawPanX = pan.x + (cx - pan.x) * (1 - ratio);
      const rawPanY = pan.y + (cy - pan.y) * (1 - ratio);
      setPan(clampPanOffset(rawPanX, rawPanY, nextZoom));
      onZoomChange(nextZoom);
    },
    [hasSelection, viewport.width, viewport.height, safeZoom, pan.x, pan.y, clampPanOffset, onZoomChange],
  );

  // ---- Split Curtain Drag ----
  const clientXToSplitPct = useCallback(
    (clientX: number): number => {
      const el = stageRef.current;
      if (!el || imgW <= 0) return safeSplit;
      const rect = el.getBoundingClientRect();
      const localX = clientX - rect.left - imgLeft;
      const pct = (localX / imgW) * 100;
      return Math.round(clampSplit(pct));
    },
    [imgLeft, imgW, safeSplit],
  );

  const startSplitDrag = useCallback(
    (e: ReactPointerEvent<HTMLElement>) => {
      e.preventDefault();
      e.stopPropagation();
      splitDragActiveRef.current = true;
      setIsDraggingSplit(true);
      e.currentTarget.setPointerCapture?.(e.pointerId);
      onSplitChange(clientXToSplitPct(e.clientX));
    },
    [clientXToSplitPct, onSplitChange],
  );

  // ---- Helper: convert viewport client coords to source-image pixels ----
  const clientToSourcePixel = useCallback(
    (clientX: number, clientY: number): { sx: number; sy: number } => {
      const el = stageRef.current;
      const rect = el?.getBoundingClientRect() ?? { left: 0, top: 0 };
      const vx = clientX - rect.left;
      const vy = clientY - rect.top;
      const sx = imgW > 0 ? ((vx - imgLeft) / imgW) * effectiveSrcW : 0;
      const sy = imgH > 0 ? ((vy - imgTop) / imgH) * effectiveSrcH : 0;
      return { sx, sy };
    },
    [imgLeft, imgTop, imgW, imgH, effectiveSrcW, effectiveSrcH],
  );

  // ---- Crop Drag Start (move / resize / draw) ----
  const startCropMove = useCallback(
    (e: ReactPointerEvent<SVGElement>) => {
      if (spacePanning || e.button !== 0 || !activeCropBox) return;
      e.preventDefault();
      e.stopPropagation();
      cropDragRef.current = {
        kind: "move",
        startClientX: e.clientX,
        startClientY: e.clientY,
        anchorImgX: 0,
        anchorImgY: 0,
        startBox: activeCropBox,
      };
      draftCropRef.current = activeCropBox;
      setDraftCrop(activeCropBox);
    },
    [spacePanning, activeCropBox],
  );

  const startCropResize = useCallback(
    (handle: CropHandle, e: ReactPointerEvent<SVGElement>) => {
      if (spacePanning || e.button !== 0 || !activeCropBox) return;
      e.preventDefault();
      e.stopPropagation();
      cropDragRef.current = {
        kind: "resize",
        handle,
        startClientX: e.clientX,
        startClientY: e.clientY,
        anchorImgX: 0,
        anchorImgY: 0,
        startBox: activeCropBox,
      };
      draftCropRef.current = activeCropBox;
      setDraftCrop(activeCropBox);
    },
    [spacePanning, activeCropBox],
  );

  // ---- Stage Background Pointer Down (Pan or Draw New Crop Box) ----
  const handleStagePointerDown = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      if (!hasSelection) return;

      // Middle click or Space+Left click always pans
      if (e.button === 1 || (e.button === 0 && spacePanning)) {
        e.preventDefault();
        panDragRef.current = {
          startClientX: e.clientX,
          startClientY: e.clientY,
          originPanX: pan.x,
          originPanY: pan.y,
        };
        setIsPanning(true);
        e.currentTarget.setPointerCapture?.(e.pointerId);
        return;
      }

      if (e.button !== 0) return;

      // In crop mode, dragging on the image draws a new crop rectangle
      if (isCropMode) {
        const { sx, sy } = clientToSourcePixel(e.clientX, e.clientY);
        const insideImage = sx >= 0 && sx <= effectiveSrcW && sy >= 0 && sy <= effectiveSrcH;
        if (insideImage) {
          e.preventDefault();
          const initial: EditCropBoxDTO = { x: sx, y: sy, w: 0, h: 0 };
          cropDragRef.current = {
            kind: "draw",
            startClientX: e.clientX,
            startClientY: e.clientY,
            anchorImgX: sx,
            anchorImgY: sy,
            startBox: initial,
          };
          draftCropRef.current = initial;
          setDraftCrop(initial);
          return;
        }
      }

      // Otherwise pan the canvas
      e.preventDefault();
      panDragRef.current = {
        startClientX: e.clientX,
        startClientY: e.clientY,
        originPanX: pan.x,
        originPanY: pan.y,
      };
      setIsPanning(true);
      e.currentTarget.setPointerCapture?.(e.pointerId);
    },
    [
      hasSelection,
      spacePanning,
      pan.x,
      pan.y,
      isCropMode,
      clientToSourcePixel,
      effectiveSrcW,
      effectiveSrcH,
    ],
  );

  // ---- Global Pointer Move & Up Listeners for smooth drag sessions ----
  useEffect(() => {
    const onPointerMove = (e: PointerEvent) => {
      if (splitDragActiveRef.current) {
        onSplitChange(clientXToSplitPct(e.clientX));
        return;
      }

      if (panDragRef.current) {
        const s = panDragRef.current;
        const nextPan = clampPanOffset(
          s.originPanX + (e.clientX - s.startClientX),
          s.originPanY + (e.clientY - s.startClientY),
          safeZoom,
        );
        setPan(nextPan);
        return;
      }

      if (cropDragRef.current) {
        const s = cropDragRef.current;
        const scaleX = imgW > 0 ? effectiveSrcW / imgW : 1;
        const scaleY = imgH > 0 ? effectiveSrcH / imgH : 1;
        const dx = (e.clientX - s.startClientX) * scaleX;
        const dy = (e.clientY - s.startClientY) * scaleY;

        let next: EditCropBoxDTO;
        if (s.kind === "move") {
          next = moveCropBox(s.startBox, dx, dy, effectiveSrcW, effectiveSrcH);
        } else if (s.kind === "resize" && s.handle) {
          next = resizeCropBox(
            s.startBox,
            s.handle,
            dx,
            dy,
            effectiveSrcW,
            effectiveSrcH,
            cropAspect,
          );
        } else {
          const { sx, sy } = clientToSourcePixel(e.clientX, e.clientY);
          next = drawCropBox(
            s.anchorImgX,
            s.anchorImgY,
            sx,
            sy,
            effectiveSrcW,
            effectiveSrcH,
            cropAspect,
          );
        }
        draftCropRef.current = next;
        setDraftCrop(next);
      }
    };

    const onPointerUp = (e: PointerEvent) => {
      if (splitDragActiveRef.current) {
        splitDragActiveRef.current = false;
        setIsDraggingSplit(false);
        onSplitCommit(clientXToSplitPct(e.clientX));
        return;
      }

      if (panDragRef.current) {
        panDragRef.current = null;
        setIsPanning(false);
        return;
      }

      if (cropDragRef.current) {
        const session = cropDragRef.current;
        cropDragRef.current = null;
        const latest = draftCropRef.current;
        if (latest && latest.w >= MIN_CROP_SIZE && latest.h >= MIN_CROP_SIZE) {
          const clamped = clampCropBox(latest, effectiveSrcW, effectiveSrcH);
          const rx = Math.min(
            Math.round(clamped.x),
            Math.max(0, effectiveSrcW - MIN_CROP_SIZE),
          );
          const ry = Math.min(
            Math.round(clamped.y),
            Math.max(0, effectiveSrcH - MIN_CROP_SIZE),
          );
          const rw = Math.max(MIN_CROP_SIZE, Math.round(clamped.w));
          const rh = Math.max(MIN_CROP_SIZE, Math.round(clamped.h));
          const finalW = Math.min(rw, Math.max(MIN_CROP_SIZE, effectiveSrcW - rx));
          const finalH = Math.min(rh, Math.max(MIN_CROP_SIZE, effectiveSrcH - ry));
          onCommitCropBox(rx, ry, finalW, finalH);
        } else if (session.kind === "draw") {
          // Tiny accidental click on empty area: discard draft without clearing existing box
          setDraftCrop(null);
        }
      }
    };

    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
    };
  }, [
    clientXToSplitPct,
    clampPanOffset,
    safeZoom,
    imgW,
    imgH,
    effectiveSrcW,
    effectiveSrcH,
    cropAspect,
    clientToSourcePixel,
    onSplitChange,
    onSplitCommit,
    onCommitCropBox,
  ]);

  const handleDoubleClick = useCallback(() => {
    if (!hasSelection) return;
    setPan({ x: 0, y: 0 });
    onResetView();
  }, [hasSelection, onResetView]);

  // ---- Live 60fps CSS Filter Computation ----
  const effectiveLiveAdjust: LiveAdjustValues = adjustEnabled
    ? liveAdjust
    : {
        brightness: 0,
        contrast: 0,
        saturation: 0,
        blur: 0,
        grayscale: liveAdjust.grayscale,
      };

  const effectiveServerAdjust: LiveAdjustValues = adjustEnabled
    ? serverAdjust
    : {
        brightness: 0,
        contrast: 0,
        saturation: 0,
        blur: 0,
        grayscale: serverAdjust.grayscale,
      };

  const adjustDiffers =
    effectiveLiveAdjust.brightness !== effectiveServerAdjust.brightness ||
    effectiveLiveAdjust.contrast !== effectiveServerAdjust.contrast ||
    effectiveLiveAdjust.saturation !== effectiveServerAdjust.saturation ||
    effectiveLiveAdjust.blur !== effectiveServerAdjust.blur ||
    effectiveLiveAdjust.grayscale !== effectiveServerAdjust.grayscale;

  const useDirectOrigForLiveAdjust =
    !hasGeometryTransform && (isLiveAdjusting || (adjustDiffers && previewPending));

  const previewCssFilter = useMemo(() => {
    if (isCropMode) {
      return buildCssFilter(effectiveLiveAdjust, null);
    }
    if (useDirectOrigForLiveAdjust) {
      return buildCssFilter(effectiveLiveAdjust, null);
    }
    if (isLiveAdjusting || adjustDiffers) {
      return buildCssFilter(effectiveLiveAdjust, effectiveServerAdjust);
    }
    return "";
  }, [
    isCropMode,
    useDirectOrigForLiveAdjust,
    isLiveAdjusting,
    adjustDiffers,
    effectiveLiveAdjust,
    effectiveServerAdjust,
  ]);

  const activeOrigSrc = committedOrigUrl || resolvedOrigUrl;
  const activePreviewSrc = useDirectOrigForLiveAdjust
    ? activeOrigSrc
    : committedPreviewUrl || resolvedPreviewUrl || activeOrigSrc;

  const needsOrigPreload = Boolean(resolvedOrigUrl && resolvedOrigUrl !== committedOrigUrl);
  const needsPreviewPreload = Boolean(
    resolvedPreviewUrl && resolvedPreviewUrl !== committedPreviewUrl,
  );

  // Convert active crop box into viewport CSS pixel coordinates (crisp 1:1 handles at all zooms)
  const cropScreenRect = useMemo(() => {
    if (!activeCropBox || effectiveSrcW <= 0 || effectiveSrcH <= 0) return null;
    const x = imgLeft + (activeCropBox.x / effectiveSrcW) * imgW;
    const y = imgTop + (activeCropBox.y / effectiveSrcH) * imgH;
    const w = (activeCropBox.w / effectiveSrcW) * imgW;
    const h = (activeCropBox.h / effectiveSrcH) * imgH;
    return { x, y, w, h };
  }, [activeCropBox, effectiveSrcW, effectiveSrcH, imgLeft, imgTop, imgW, imgH]);

  const handleCoords = useMemo(() => {
    if (!cropScreenRect) return null;
    const { x, y, w, h } = cropScreenRect;
    return {
      nw: { cx: x, cy: y },
      n: { cx: x + w / 2, cy: y },
      ne: { cx: x + w, cy: y },
      e: { cx: x + w, cy: y + h / 2 },
      se: { cx: x + w, cy: y + h },
      s: { cx: x + w / 2, cy: y + h },
      sw: { cx: x, cy: y + h },
      w: { cx: x, cy: y + h / 2 },
    } as Record<CropHandle, { cx: number; cy: number }>;
  }, [cropScreenRect]);

  const stageCursor = isPanning
    ? "cursor-grabbing"
    : spacePanning
      ? "cursor-grab"
      : isCropMode
        ? "cursor-crosshair"
        : hasSelection
          ? "cursor-grab"
          : "cursor-default";

  return (
    <div
      ref={stageRef}
      data-testid="image-edit-stage"
      onWheel={handleWheel}
      onPointerDown={handleStagePointerDown}
      onDoubleClick={handleDoubleClick}
      className={cn(
        "relative min-h-0 w-full flex-1 overflow-hidden bg-[#0F172A] select-none",
        stageCursor,
        className,
      )}
    >
      {/* Hidden double-buffer preloaders for zero-flicker swap on new Go renders */}
      {needsOrigPreload ? (
        <img
          src={resolvedOrigUrl}
          alt=""
          aria-hidden="true"
          className="hidden"
          onLoad={(e) => {
            const el = e.currentTarget;
            if (el.naturalWidth > 0 && el.naturalHeight > 0) {
              setNaturalOrig({ w: el.naturalWidth, h: el.naturalHeight });
            }
            setCommittedOrigUrl(resolvedOrigUrl);
          }}
          onError={() => setCommittedOrigUrl(resolvedOrigUrl)}
        />
      ) : null}
      {needsPreviewPreload ? (
        <img
          src={resolvedPreviewUrl}
          alt=""
          aria-hidden="true"
          className="hidden"
          onLoad={(e) => {
            const el = e.currentTarget;
            if (el.naturalWidth > 0 && el.naturalHeight > 0) {
              setNaturalPreview({ w: el.naturalWidth, h: el.naturalHeight });
            }
            setCommittedPreviewUrl(resolvedPreviewUrl);
          }}
          onError={() => setCommittedPreviewUrl(resolvedPreviewUrl)}
        />
      ) : null}

      {!hasSelection ? (
        <div
          data-testid="edit-stage-empty"
          className="flex h-full w-full flex-col items-center justify-center gap-3.5 p-6 text-center"
        >
          <div className="flex size-12 items-center justify-center rounded-2xl border border-slate-700/80 bg-slate-800/70 text-slate-300">
            <Icon name="wand-sparkles" size={22} />
          </div>
          <div className="flex max-w-[440px] flex-col gap-1.5">
            <div className="text-[13px] font-semibold text-slate-200">
              选择、导入或拖入图片开始精修
            </div>
            <div className="text-[11px] leading-relaxed text-slate-400">
              单次最多导入 5000 张图片或文件夹 · 网格批览 · 统一或单图独立裁剪 · 实时调色
            </div>
            <div className="tnum mt-0.5 flex flex-wrap items-center justify-center gap-1.5 text-[10.5px] text-slate-400">
              <span className="rounded-full border border-slate-700/80 bg-slate-800/70 px-2 py-0.5">
                ← / → 切换图片
              </span>
              <span className="rounded-full border border-slate-700/80 bg-slate-800/70 px-2 py-0.5">
                C 原图对比
              </span>
              <span className="rounded-full border border-slate-700/80 bg-slate-800/70 px-2 py-0.5">
                [ / ] 旋转
              </span>
              <span className="rounded-full border border-slate-700/80 bg-slate-800/70 px-2 py-0.5">
                滚轮缩放 · 双击适应
              </span>
            </div>
          </div>
          {onPickFiles ? (
            <Button
              size="small"
              variant="primary"
              icon="plus"
              text="选择图片"
              onClick={(e) => {
                e.stopPropagation();
                onPickFiles();
              }}
            />
          ) : null}
        </div>
      ) : (
        <>
          {/* Top-left Floating Batch Context Pill */}
          {totalFiles !== undefined && totalFiles > 0 ? (
            <div
              data-testid="edit-stage-context-pill"
              onPointerDown={(e) => e.stopPropagation()}
              onDoubleClick={(e) => e.stopPropagation()}
              className="absolute top-3 left-3 z-30 flex max-w-[min(380px,calc(100%-120px))] items-center gap-1.5 rounded-full border border-slate-700/80 bg-[#0F172A]/88 px-2.5 py-1 text-[11px] text-slate-200 shadow-md backdrop-blur-[2px]"
            >
              {totalFiles > 1 && (onPrevFile || onNextFile) ? (
                <TooltipGroup className="flex items-center gap-0.5">
                  <Tooltip content="上一张" shortcut="←" side="bottom">
                    <button
                      type="button"
                      aria-label="上一张"
                      disabled={!onPrevFile || (fileIndex ?? 0) <= 0}
                      onClick={(e) => {
                        e.stopPropagation();
                        onPrevFile?.();
                      }}
                      className="inline-flex size-5 cursor-pointer items-center justify-center rounded-full text-slate-300 transition-colors hover:bg-white/10 hover:text-white disabled:pointer-events-none disabled:opacity-35"
                    >
                      <Icon name="chevron-left" size={13} />
                    </button>
                  </Tooltip>
                  <Tooltip content="下一张" shortcut="→" side="bottom">
                    <button
                      type="button"
                      aria-label="下一张"
                      disabled={!onNextFile || (fileIndex ?? 0) >= totalFiles - 1}
                      onClick={(e) => {
                        e.stopPropagation();
                        onNextFile?.();
                      }}
                      className="inline-flex size-5 cursor-pointer items-center justify-center rounded-full text-slate-300 transition-colors hover:bg-white/10 hover:text-white disabled:pointer-events-none disabled:opacity-35"
                    >
                      <Icon name="chevron-right" size={13} />
                    </button>
                  </Tooltip>
                </TooltipGroup>
              ) : null}

              <span className="tnum shrink-0 font-semibold text-slate-100">
                {`#${(fileIndex ?? 0) + 1}/${totalFiles}`}
              </span>

              {fileName ? (
                <Tooltip content={fileName} side="bottom">
                  <span tabIndex={0} className="min-w-0 truncate rounded text-slate-300 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]">
                    {`· ${fileName}`}
                  </span>
                </Tooltip>
              ) : null}

              {onTogglePickCurrent ? (
                <button
                  type="button"
                  data-testid="edit-stage-pick-btn"
                  aria-label={isPicked ? "取消多选当前图片" : "多选当前图片"}
                  aria-pressed={Boolean(isPicked)}
                  onClick={(e) => {
                    e.stopPropagation();
                    onTogglePickCurrent();
                  }}
                  className={cn(
                    "ml-0.5 inline-flex shrink-0 cursor-pointer items-center gap-1 rounded-full border px-2 py-0.5 text-[10.5px] font-medium transition-colors",
                    isPicked
                      ? "border-[#60A5FA]/60 bg-[#2563EB]/30 text-[#93C5FD] hover:bg-[#2563EB]/40"
                      : "border-slate-600/80 bg-slate-800/80 text-slate-300 hover:border-slate-500 hover:text-white",
                  )}
                >
                  <Icon name={isPicked ? "circle-check" : "check"} size={11} />
                  <span>{isPicked ? "已选" : "多选"}</span>
                </button>
              ) : null}
            </div>
          ) : null}

          {/* Artwork Matte Frame */}
          <div
            data-testid="edit-stage-artwork"
            style={{
              left: `${imgLeft}px`,
              top: `${imgTop}px`,
              width: `${imgW}px`,
              height: `${imgH}px`,
            }}
            className="bg-checkerboard-dark absolute overflow-hidden rounded-[2px] shadow-[0_10px_34px_rgba(0,0,0,0.5)] ring-1 ring-white/10"
          >
            {isCropMode ? (
              activeOrigSrc ? (
                <img
                  data-testid="edit-stage-orig-img"
                  data-live-filter={previewCssFilter || undefined}
                  src={activeOrigSrc}
                  alt="待裁剪原图"
                  draggable={false}
                  style={previewCssFilter ? { filter: previewCssFilter } : undefined}
                  onLoad={(e) => {
                    const el = e.currentTarget;
                    if (el.naturalWidth > 0 && el.naturalHeight > 0) {
                      setNaturalOrig({ w: el.naturalWidth, h: el.naturalHeight });
                    }
                  }}
                  className="pointer-events-none h-full w-full object-fill select-none"
                />
              ) : null
            ) : (
              <>
                {/* Processed Preview Image (Full Layer) */}
                {activePreviewSrc ? (
                  <img
                    data-testid="edit-stage-preview-img"
                    data-live-filter={previewCssFilter || undefined}
                    src={activePreviewSrc}
                    alt="编辑预览图"
                    draggable={false}
                    style={previewCssFilter ? { filter: previewCssFilter } : undefined}
                    onLoad={(e) => {
                      const el = e.currentTarget;
                      if (el.naturalWidth > 0 && el.naturalHeight > 0) {
                        setNaturalPreview({ w: el.naturalWidth, h: el.naturalHeight });
                      }
                    }}
                    className="pointer-events-none h-full w-full object-fill select-none"
                  />
                ) : null}

                {/* Before/After Compare Split Overlay (Original Image clipped on Left) */}
                {compare && activeOrigSrc ? (
                  <>
                    <div
                      data-testid="edit-compare-orig-clip"
                      style={{ clipPath: `inset(0 ${100 - safeSplit}% 0 0)` }}
                      className="pointer-events-none absolute inset-0"
                    >
                      <img
                        data-testid="edit-stage-compare-orig-img"
                        src={activeOrigSrc}
                        alt="原图对比"
                        draggable={false}
                        className="h-full w-full object-contain select-none"
                      />
                    </div>

                    {/* Floating Original / Output micro-tags */}
                    <span className="pointer-events-none absolute top-2.5 left-2.5 z-10 rounded-full border border-white/15 bg-[#0F172A]/80 px-2 py-0.5 text-[10px] font-medium text-slate-200 backdrop-blur-[2px]">
                      原图
                    </span>
                    <span className="pointer-events-none absolute top-2.5 right-2.5 z-10 rounded-full border border-white/15 bg-[#0F172A]/80 px-2 py-0.5 text-[10px] font-medium text-slate-200 backdrop-blur-[2px]">
                      成图
                    </span>

                    {/* Draggable Split Divider & Grab Pill */}
                    <div
                      data-testid="edit-split-divider"
                      style={{ left: `${safeSplit}%` }}
                      onPointerDown={startSplitDrag}
                      className="group absolute top-0 bottom-0 z-20 w-5 -translate-x-1/2 cursor-ew-resize"
                    >
                      <div
                        className={cn(
                          "pointer-events-none absolute top-0 bottom-0 left-1/2 w-[2px] -translate-x-1/2 shadow-[0_0_0_1px_rgba(15,23,42,0.55)] transition-colors",
                          isDraggingSplit ? "bg-[#60A5FA]" : "bg-[#3B82F6] group-hover:bg-[#60A5FA]",
                        )}
                      />
                      <button
                        type="button"
                        data-testid="edit-split-handle"
                        aria-label="拖动原图对比分割线"
                        onPointerDown={startSplitDrag}
                        className={cn(
                          "absolute top-1/2 left-1/2 flex size-8 -translate-x-1/2 -translate-y-1/2 cursor-ew-resize items-center justify-center rounded-full border-2 border-[#60A5FA] bg-[#0F172A]/95 text-white shadow-[0_2px_10px_rgba(0,0,0,0.45)] transition-transform",
                          isDraggingSplit ? "scale-105" : "group-hover:scale-105",
                        )}
                      >
                        <MoveHorizontal size={14} />
                      </button>
                    </div>
                  </>
                ) : null}
              </>
            )}
          </div>

          {/* Interactive 8-Handle Crop Box SVG Overlay (1:1 Viewport CSS Pixels) */}
          {isCropMode && cropScreenRect && activeCropBox && activeCropBox.w >= 2 && activeCropBox.h >= 2 ? (
            <>
              <svg
                data-testid="edit-crop-overlay"
                width={viewport.width}
                height={viewport.height}
                viewBox={`0 0 ${viewport.width} ${viewport.height}`}
                className="pointer-events-none absolute inset-0 z-20 overflow-visible"
              >
                {/* Exterior Darkened Mask (clipped strictly to artwork bounds) */}
                <path
                  d={`M ${imgLeft} ${imgTop} H ${imgLeft + imgW} V ${imgTop + imgH} H ${imgLeft} Z M ${cropScreenRect.x} ${cropScreenRect.y} V ${cropScreenRect.y + cropScreenRect.h} H ${cropScreenRect.x + cropScreenRect.w} V ${cropScreenRect.y} Z`}
                  fill="rgba(9, 13, 22, 0.58)"
                  fillRule="evenodd"
                />

                {/* Moveable Interior Crop Region */}
                <rect
                  data-testid="edit-crop-move"
                  x={cropScreenRect.x}
                  y={cropScreenRect.y}
                  width={cropScreenRect.w}
                  height={cropScreenRect.h}
                  fill="transparent"
                  onPointerDown={startCropMove}
                  className={cn(
                    spacePanning ? "pointer-events-none" : "pointer-events-auto cursor-move",
                  )}
                />

                {/* Rule-of-Thirds Grid Hairlines */}
                {cropScreenRect.w >= 36 && cropScreenRect.h >= 36 ? (
                  <g stroke="rgba(255,255,255,0.42)" strokeWidth={1}>
                    <line
                      x1={cropScreenRect.x + cropScreenRect.w / 3}
                      y1={cropScreenRect.y}
                      x2={cropScreenRect.x + cropScreenRect.w / 3}
                      y2={cropScreenRect.y + cropScreenRect.h}
                    />
                    <line
                      x1={cropScreenRect.x + (cropScreenRect.w * 2) / 3}
                      y1={cropScreenRect.y}
                      x2={cropScreenRect.x + (cropScreenRect.w * 2) / 3}
                      y2={cropScreenRect.y + cropScreenRect.h}
                    />
                    <line
                      x1={cropScreenRect.x}
                      y1={cropScreenRect.y + cropScreenRect.h / 3}
                      x2={cropScreenRect.x + cropScreenRect.w}
                      y2={cropScreenRect.y + cropScreenRect.h / 3}
                    />
                    <line
                      x1={cropScreenRect.x}
                      y1={cropScreenRect.y + (cropScreenRect.h * 2) / 3}
                      x2={cropScreenRect.x + cropScreenRect.w}
                      y2={cropScreenRect.y + (cropScreenRect.h * 2) / 3}
                    />
                  </g>
                ) : null}

                {/* Outer Crisp Border */}
                <rect
                  x={cropScreenRect.x}
                  y={cropScreenRect.y}
                  width={cropScreenRect.w}
                  height={cropScreenRect.h}
                  fill="none"
                  stroke="#0F172A"
                  strokeOpacity={0.65}
                  strokeWidth={3}
                />
                <rect
                  x={cropScreenRect.x}
                  y={cropScreenRect.y}
                  width={cropScreenRect.w}
                  height={cropScreenRect.h}
                  fill="none"
                  stroke="#FFFFFF"
                  strokeWidth={1.5}
                />

                {/* 8 Resize Handles (4 Corners + 4 Edges) */}
                {handleCoords
                  ? ALL_HANDLES.map((handle) => {
                      const pt = handleCoords[handle];
                      const isCorner = handle.length === 2;
                      const isHorizEdge = handle === "n" || handle === "s";
                      const visualW = isCorner ? 10 : isHorizEdge ? 20 : 6;
                      const visualH = isCorner ? 10 : isHorizEdge ? 6 : 20;
                      return (
                        <g key={handle}>
                          <rect
                            x={pt.cx - visualW / 2}
                            y={pt.cy - visualH / 2}
                            width={visualW}
                            height={visualH}
                            rx={isCorner ? 2.5 : 3}
                            fill="#FFFFFF"
                            stroke="#2563EB"
                            strokeWidth={1.5}
                          />
                          <rect
                            data-testid={`edit-crop-handle-${handle}`}
                            x={pt.cx - 10}
                            y={pt.cy - 10}
                            width={20}
                            height={20}
                            fill="transparent"
                            style={{ cursor: HANDLE_CURSORS[handle] }}
                            onPointerDown={(e) => startCropResize(handle, e)}
                            className={spacePanning ? "pointer-events-none" : "pointer-events-auto"}
                          />
                        </g>
                      );
                    })
                  : null}
              </svg>

              {/* Floating Live Pixel Size Readout + Clear Crop Action */}
              <div
                data-testid="edit-crop-readout"
                style={{
                  left: `${Math.max(12, Math.min(viewport.width - 170, cropScreenRect.x))}px`,
                  top: `${Math.max(10, cropScreenRect.y > 36 ? cropScreenRect.y - 30 : cropScreenRect.y + 8)}px`,
                }}
                onPointerDown={(e) => e.stopPropagation()}
                className="tnum absolute z-30 flex items-center gap-1.5 rounded-full border border-white/15 bg-[#0F172A]/88 px-2.5 py-0.5 text-[11px] font-medium text-slate-100 shadow-md backdrop-blur-[2px]"
              >
                <span>
                  {`${Math.round(activeCropBox.w)} × ${Math.round(activeCropBox.h)} px`}
                </span>
                {cropRatio ? (
                  <span className="rounded bg-[#2563EB]/30 px-1 text-[10px] text-[#93C5FD]">
                    {cropRatio}
                  </span>
                ) : null}
                {cropBox || cropRatio ? (
                  <Tooltip content="清除裁剪框" side="top">
                    <button
                      type="button"
                      data-testid="edit-crop-clear"
                      aria-label="清除裁剪框"
                      onClick={(e) => {
                        e.stopPropagation();
                        setDraftCrop(null);
                        onClearCropBox();
                      }}
                      className="ml-0.5 inline-flex size-4 cursor-pointer items-center justify-center rounded-full text-slate-300 transition-colors hover:bg-white/15 hover:text-white"
                    >
                      <X size={11} />
                    </button>
                  </Tooltip>
                ) : null}
              </div>
            </>
          ) : null}

          {/* Non-blocking Top-Right Render Pending Pill */}
          {previewPending ? (
            <div
              data-testid="edit-preview-pending"
              className="pointer-events-none absolute top-3 right-3 z-30 flex items-center gap-1.5 rounded-full border border-slate-700/80 bg-[#0F172A]/85 px-2.5 py-1 text-[11px] text-slate-200 backdrop-blur-[2px]"
            >
              <span className="size-1.5 animate-pulse rounded-full bg-[#60A5FA]" />
              <span>渲染中…</span>
            </div>
          ) : null}

          {/* Subtle Bottom-Left Interaction Hint Pill */}
          {spacePanning ? (
            <div className="pointer-events-none absolute bottom-3 left-3 z-30 rounded-full border border-slate-700/80 bg-[#0F172A]/85 px-2.5 py-1 text-[11px] text-slate-300">
              抓手平移模式 · 拖动移动画布
            </div>
          ) : isCropMode && !activeCropBox ? (
            <div className="pointer-events-none absolute bottom-3 left-3 z-30 rounded-full border border-slate-700/80 bg-[#0F172A]/85 px-2.5 py-1 text-[11px] text-slate-300">
              在画面上拖拽框选裁剪区域 · 按住空格键可拖动画布
            </div>
          ) : null}

          {/* Subtle Bottom-Right Crop Scope & Zoom/Fit Hint */}
          <div
            data-testid="edit-stage-bottom-hint"
            className="pointer-events-none absolute right-3 bottom-3 z-30 flex items-center gap-1.5"
          >
            {isCropMode && uniformCrop !== undefined ? (
              <span
                className={cn(
                  "rounded-full border px-2.5 py-1 text-[10.5px] font-medium backdrop-blur-[2px]",
                  !uniformCrop && hasCustomCrop
                    ? "border-[#60A5FA]/50 bg-[#1E3A8A]/70 text-[#BFDBFE]"
                    : "border-slate-700/80 bg-[#0F172A]/85 text-slate-300",
                )}
              >
                {uniformCrop
                  ? "全批统一裁剪"
                  : hasCustomCrop
                    ? "单图独立裁剪 · 已自定义"
                    : "单图独立裁剪"}
              </span>
            ) : null}
            <span className="tnum hidden rounded-full border border-slate-700/80 bg-[#0F172A]/85 px-2.5 py-1 text-[10.5px] text-slate-400 backdrop-blur-[2px] sm:inline-flex">
              {Math.round(safeZoom * 100) !== 100
                ? `${Math.round(safeZoom * 100)}% · 双击适应`
                : "滚轮缩放 · 空格拖拽 · 双击适应"}
            </span>
          </div>
        </>
      )}
    </div>
  );
}
