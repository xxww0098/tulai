import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  EmptyNotice,
  FlushPane,
  InsetPillRow,
  PageToolbar,
} from "@/components/primitives";
import {
  Badge,
  Button,
  ContextMenu,
  Dialog,
  DropdownMenu,
  Icon,
  IconButton,
  Input,
  Select,
  Switch,
  Textarea,
  Tooltip,
  TooltipGroup,
  VirtualList,
  type BadgeVariant,
  type MenuActionItem,
} from "@/components/ui";
import { cn } from "@/lib/cn";
import { Core, Prompts, isMyGoRuntime } from "@/lib/ipc";
import { modKeyLabel, useEscapeHandler } from "@/lib/shortcuts";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import {
  TAB_FISSION,
  TAB_GEN,
  TAB_WASH,
  goTab,
  refreshPrompts,
  showToast,
  updateWorkbenchField,
  usePageLoading,
  usePrompts,
  type PromptTemplateDTO,
  type PromptsStateDTO,
} from "@/store";

const KIND_OPTIONS = [
  { value: "生图", label: "生图" },
  { value: "洗图", label: "洗图" },
  { value: "裂变", label: "裂变" },
  { value: "通用", label: "通用" },
];

const FILTER_KINDS = ["全部", "生图", "洗图", "裂变", "通用"] as const;

type EditorViewMode = "edit" | "reader";

interface PromptBlock {
  id: string;
  heading?: string;
  body: string;
}

const DEFAULT_PROMPTS_STATE: PromptsStateDTO = {
  items: [],
  selectedId: "",
  selectedName: "",
  selectedKind: "生图",
  selectedText: "",
  selectedIsBln: false,
  filter: "",
  note: "",
  suffix: "",
  useSuffix: false,
};

function invokeSafe(call: () => Promise<unknown>, after?: () => Promise<unknown>): void {
  try {
    void Promise.resolve(call())
      .then(() => (after ? after() : undefined))
      .catch(() => {
        // Ignore runtime-unavailable rejections outside MyGo WebView (e.g. Vite preview)
      });
  } catch {
    // Ignore synchronous runtime-unavailable errors
  }
}

async function copyToClipboard(text: string): Promise<boolean> {
  if (!text) return false;
  if (isMyGoRuntime()) {
    try {
      await Core.copyText(text);
      return true;
    } catch {
      return false;
    }
  }
  try {
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Ignore clipboard fallback error
  }
  return false;
}

function normalizeKindLabel(kindLbl?: string, kind?: string): string {
  const raw = (kindLbl || kind || "").trim();
  if (raw === "gen" || raw === "生图") return "生图";
  if (raw === "wash" || raw === "洗图") return "洗图";
  if (raw === "fission" || raw === "裂变") return "裂变";
  return "通用";
}

function kindBadgeVariant(kindLabel: string): BadgeVariant {
  switch (kindLabel) {
    case "生图":
      return "success";
    case "洗图":
      return "accent";
    case "裂变":
      return "warn";
    default:
      return "neutral";
  }
}

function kindIcon(kindLabel: string): string {
  switch (kindLabel) {
    case "洗图":
      return "wand-sparkles";
    case "裂变":
      return "layers";
    case "生图":
      return "sparkles";
    default:
      return "book-open";
  }
}

function getTargetWorkbench(kindLabel: string): { tab: number; label: string; buttonText: string } {
  switch (kindLabel) {
    case "洗图":
      return { tab: TAB_WASH, label: "批量洗图", buttonText: "填入批量洗图" };
    case "裂变":
      return { tab: TAB_FISSION, label: "裂变图片", buttonText: "填入裂变图片" };
    default:
      return { tab: TAB_GEN, label: "自由生图", buttonText: "填入自由生图" };
  }
}

function parsePromptBlocks(raw: string): PromptBlock[] {
  const normalized = raw.replace(/\r\n/g, "\n").trim();
  if (!normalized) return [];

  const hasBlankParagraphs = /\n\s*\n/.test(normalized);
  const chunks = hasBlankParagraphs
    ? normalized
        .split(/\n\s*\n/)
        .map((chunk) => chunk.trim())
        .filter(Boolean)
    : normalized
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean);

  return chunks.map((chunk, idx) => {
    const colonIdx = chunk.search(/[：:]/);
    if (colonIdx > 0 && colonIdx <= 12) {
      const candidate = chunk.slice(0, colonIdx).trim();
      const rest = chunk.slice(colonIdx + 1).trim();
      if (candidate && rest && !/[，。,.\s]/.test(candidate)) {
        return {
          id: `blk-${idx}`,
          heading: candidate,
          body: rest,
        };
      }
    }
    return {
      id: `blk-${idx}`,
      body: chunk,
    };
  });
}

function PromptDirectorySkeleton() {
  return (
    <div className="flex min-h-0 flex-1 gap-3 px-3 pb-3">
      <div className="flex h-full w-[244px] shrink-0 flex-col overflow-hidden rounded-[16px] border border-hairline bg-card lg:w-[276px] xl:w-[296px]">
        <div className="h-[44px] border-b border-divider bg-card-header px-3.5 py-2.5">
          <div className="h-4 w-24 animate-pulse rounded bg-sunken" />
        </div>
        <div className="flex flex-col gap-2 border-b border-divider p-3">
          <div className="h-7 w-full animate-pulse rounded-[10px] bg-sunken" />
          <div className="h-7 w-full animate-pulse rounded-[10px] bg-sunken" />
        </div>
        <div className="flex flex-col gap-2 p-3">
          {Array.from({ length: 4 }).map((_, idx) => (
            <div key={idx} className="h-[68px] w-full animate-pulse rounded-[12px] bg-sunken" />
          ))}
        </div>
      </div>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-[16px] border border-hairline bg-card">
        <div className="h-[48px] border-b border-divider bg-card-header px-4 py-3">
          <div className="h-4 w-40 animate-pulse rounded bg-sunken" />
        </div>
        <div className="flex flex-1 flex-col gap-3 p-4">
          <div className="h-8 w-full animate-pulse rounded-[10px] bg-sunken" />
          <div className="min-h-0 flex-1 animate-pulse rounded-[12px] bg-sunken" />
        </div>
      </div>
    </div>
  );
}

export function PromptsPage() {
  const promptsSlice = usePrompts();
  const loading = usePageLoading("prompts");
  const st = promptsSlice ?? DEFAULT_PROMPTS_STATE;

  const [filterKind, setFilterKind] = useState<string>("全部");
  const [draftKind, setDraftKind] = useState<string>(
    normalizeKindLabel(st.selectedKind || "生图"),
  );
  const [resetNonce, setResetNonce] = useState(0);
  const [viewMode, setViewMode] = useState<EditorViewMode>("edit");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [previewExpanded, setPreviewExpanded] = useState(false);

  const [newOpen, setNewOpen] = useState(false);
  const [newName, setNewName] = useState("");
  const [newKind, setNewKind] = useState("生图");
  const [newText, setNewText] = useState("");

  const suffixInputRef = useRef<HTMLInputElement | null>(null);
  const editorBodyRef = useRef<HTMLTextAreaElement | null>(null);
  const newNameInputRef = useRef<HTMLInputElement | null>(null);

  const serverKind = normalizeKindLabel(st.selectedKind || "生图");

  useEffect(() => {
    setDraftKind(serverKind);
    setConfirmDelete(false);
    setPreviewExpanded(false);
  }, [st.selectedId, serverKind, resetNonce]);

  const suffixDraft = useSyncedDraft(
    st.suffix || "",
    "prompts-global-suffix",
    (next) => {
      invokeSafe(() => Prompts.setPromptSuffix(next, Boolean(st.useSuffix)), refreshPrompts);
    },
    { debounceMs: 120 },
  );

  const searchDraft = useSyncedDraft(
    st.filter || "",
    "prompts-search-filter",
    (next) => {
      invokeSafe(() => Prompts.setPromptFilter(next), refreshPrompts);
    },
    { debounceMs: 90 },
  );

  const editorSyncKey = `${st.selectedId}:${resetNonce}`;
  const nameDraft = useSyncedDraft(st.selectedName || "", editorSyncKey, () => {}, {
    debounceMs: 0,
  });
  const textDraft = useSyncedDraft(st.selectedText || "", editorSyncKey, () => {}, {
    debounceMs: 0,
  });

  const isDirty = Boolean(
    st.selectedId &&
      (nameDraft.value !== (st.selectedName || "") ||
        draftKind !== serverKind ||
        textDraft.value !== (st.selectedText || "")),
  );

  const closeNewDialog = useCallback(() => {
    setNewOpen(false);
  }, []);

  useEscapeHandler("prompts-new-dialog", newOpen, closeNewDialog, 40);
  useEscapeHandler(
    "prompts-confirm-delete",
    confirmDelete,
    useCallback(() => setConfirmDelete(false), []),
    10,
  );
  useEscapeHandler(
    "prompts-preview-expanded",
    previewExpanded,
    useCallback(() => setPreviewExpanded(false), []),
    10,
  );

  const openNewDialog = useCallback(
    (presetKind?: string) => {
      const resolvedKind =
        presetKind && presetKind !== "全部"
          ? presetKind
          : filterKind !== "全部"
            ? filterKind
            : "生图";
      setNewName("");
      setNewKind(resolvedKind);
      setNewText("");
      setNewOpen(true);
    },
    [filterKind],
  );

  const allItems = st.items ?? [];
  const filteredItems = useMemo(() => {
    if (filterKind === "全部") return allItems;
    return allItems.filter(
      (it) => normalizeKindLabel(it.kindLbl, it.kind) === filterKind,
    );
  }, [allItems, filterKind]);

  const blocks = useMemo(() => parsePromptBlocks(textDraft.value), [textDraft.value]);
  const charCount = textDraft.value.length;
  const lineCount = textDraft.value.trim() ? textDraft.value.split("\n").length : 0;

  const editorKindOptions = useMemo(() => {
    if (!draftKind || KIND_OPTIONS.some((o) => o.value === draftKind)) {
      return KIND_OPTIONS;
    }
    return [...KIND_OPTIONS, { value: draftKind, label: draftKind }];
  }, [draftKind]);

  const supportsGlobalSuffix = draftKind === "生图" || draftKind === "通用";
  const trimmedSuffix = suffixDraft.value.trim();
  const suffixActive = Boolean(st.useSuffix && trimmedSuffix);

  const combinedPreview = useMemo(() => {
    const body = textDraft.value.trim();
    if (!body) return "";
    if (supportsGlobalSuffix && suffixActive) {
      return `${body}\n${trimmedSuffix}`;
    }
    return body;
  }, [textDraft.value, supportsGlobalSuffix, suffixActive, trimmedSuffix]);

  const targetMeta = useMemo(() => getTargetWorkbench(draftKind), [draftKind]);

  const handleToggleSuffix = useCallback(
    (checked: boolean) => {
      suffixDraft.flush();
      invokeSafe(() => Prompts.setPromptSuffix(suffixDraft.value, checked), refreshPrompts);
    },
    [suffixDraft],
  );

  const handleSelectTemplate = useCallback((id: string) => {
    setConfirmDelete(false);
    invokeSafe(() => Prompts.selectPrompt(id), refreshPrompts);
  }, []);

  const applyTextToWorkbench = useCallback(
    async (text: string, target: { tab: number; label: string; batchMode?: "0" | "1" }) => {
      try {
        await goTab(target.tab);
        if (target.batchMode) {
          await updateWorkbenchField("batchMode", target.batchMode);
        }
        await updateWorkbenchField("prompts", text);
        showToast(`已将提示词填入${target.label}`, "success");
      } catch {
        // Ignore runtime-unavailable rejections outside MyGo WebView
      }
    },
    [],
  );

  const handleApplyCurrent = useCallback(
    async (overrideTarget?: { tab: number; label: string; batchMode?: "0" | "1" }) => {
      textDraft.flush();
      const text = textDraft.value;
      await applyTextToWorkbench(text, overrideTarget ?? targetMeta);
    },
    [applyTextToWorkbench, targetMeta, textDraft],
  );

  const handleRestoreBuiltin = useCallback(() => {
    setResetNonce((n) => n + 1);
    invokeSafe(() => Prompts.restoreBuiltinPrompt(), refreshPrompts);
  }, []);

  const handleDiscardEdits = useCallback(() => {
    setResetNonce((n) => n + 1);
    setConfirmDelete(false);
  }, []);

  const handleDeleteUserPrompt = useCallback(() => {
    setConfirmDelete(false);
    invokeSafe(() => Prompts.deleteUserPrompt(), refreshPrompts);
  }, []);

  const handleSaveEdits = useCallback(() => {
    if (!st.selectedId || !textDraft.value.trim()) return;
    nameDraft.flush();
    textDraft.flush();
    const saveName = nameDraft.value.trim() || st.selectedName || "自定义提示词";
    invokeSafe(
      () => Prompts.savePromptEdits(saveName, draftKind, textDraft.value),
      refreshPrompts,
    );
  }, [st.selectedId, st.selectedName, nameDraft, draftKind, textDraft]);

  const handleDuplicateCurrent = useCallback(() => {
    textDraft.flush();
    nameDraft.flush();
    const body = textDraft.value.trim();
    if (!body) return;
    const baseName = (nameDraft.value.trim() || st.selectedName || "模板") + " 副本";
    invokeSafe(() => Prompts.createNewPrompt(baseName, draftKind, body), refreshPrompts);
    showToast(`已另存副本「${baseName}」`, "success");
  }, [draftKind, nameDraft, st.selectedName, textDraft]);

  const handleCopyText = useCallback(async (text: string, message: string) => {
    const ok = await copyToClipboard(text);
    if (ok) {
      showToast(message, "success");
    }
  }, []);

  const handleCreateTemplate = useCallback(() => {
    if (!newName.trim() || !newText.trim()) return;
    const name = newName.trim();
    const kind = newKind;
    const body = newText;
    setNewOpen(false);
    invokeSafe(() => Prompts.createNewPrompt(name, kind, body), refreshPrompts);
  }, [newName, newKind, newText]);

  // Cmd/Ctrl + S saves dirty edits in the prompt studio
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.isComposing || e.altKey || e.shiftKey) return;
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        if (!newOpen && st.selectedId && isDirty && textDraft.value.trim()) {
          e.preventDefault();
          handleSaveEdits();
        }
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [newOpen, st.selectedId, isDirty, textDraft.value, handleSaveEdits]);

  const applyMenuItems = useMemo<MenuActionItem[]>(
    () => [
      {
        id: "apply-gen-text",
        label: "填入文生图",
        icon: "file-text",
        onSelect: () =>
          void handleApplyCurrent({ tab: TAB_GEN, label: "文生图", batchMode: "0" }),
      },
      {
        id: "apply-gen-image",
        label: "填入图生图",
        icon: "images",
        onSelect: () =>
          void handleApplyCurrent({ tab: TAB_GEN, label: "图生图", batchMode: "1" }),
      },
      {
        id: "apply-wash",
        label: "填入批量洗图",
        icon: "wand-sparkles",
        onSelect: () => void handleApplyCurrent({ tab: TAB_WASH, label: "批量洗图" }),
      },
      {
        id: "apply-fission",
        label: "填入裂变图片",
        icon: "layers",
        onSelect: () => void handleApplyCurrent({ tab: TAB_FISSION, label: "裂变图片" }),
      },
    ],
    [handleApplyCurrent],
  );

  const overflowMenuItems = useMemo<MenuActionItem[]>(() => {
    const items: MenuActionItem[] = [
      {
        id: "menu-copy-body",
        label: "复制正文",
        icon: "copy",
        disabled: !textDraft.value.trim(),
        onSelect: () => void handleCopyText(textDraft.value, "已复制提示词正文"),
      },
      {
        id: "menu-duplicate",
        label: "另存为副本",
        icon: "plus",
        disabled: !textDraft.value.trim(),
        onSelect: handleDuplicateCurrent,
      },
      { type: "separator" },
    ];
    if (st.selectedIsBln) {
      items.push({
        id: "menu-restore",
        label: "恢复默认",
        icon: "rotate-ccw",
        onSelect: handleRestoreBuiltin,
      });
    } else {
      items.push({
        id: "menu-delete",
        label: "删除模板",
        icon: "trash-2",
        danger: true,
        onSelect: () => setConfirmDelete(true),
      });
    }
    return items;
  }, [
    handleCopyText,
    handleDuplicateCurrent,
    handleRestoreBuiltin,
    st.selectedIsBln,
    textDraft.value,
  ]);

  const buildRowContextMenu = useCallback(
    (item: PromptTemplateDTO): MenuActionItem[] => {
      const kLbl = normalizeKindLabel(item.kindLbl, item.kind);
      const target = getTargetWorkbench(kLbl);
      const body = item.text || "";
      const items: MenuActionItem[] = [
        {
          id: `ctx-apply-${item.id}`,
          label: target.buttonText,
          icon: kindIcon(kLbl),
          onSelect: () => void applyTextToWorkbench(body, target),
        },
        {
          id: `ctx-copy-${item.id}`,
          label: "复制正文",
          icon: "copy",
          disabled: !body.trim(),
          onSelect: () => void handleCopyText(body, "已复制提示词正文"),
        },
        {
          id: `ctx-dup-${item.id}`,
          label: "另存为副本",
          icon: "plus",
          disabled: !body.trim(),
          onSelect: () => {
            const copyName = `${item.name || item.label || "模板"} 副本`;
            invokeSafe(() => Prompts.createNewPrompt(copyName, kLbl, body), refreshPrompts);
            showToast(`已新建副本「${copyName}」`, "success");
          },
        },
      ];
      return items;
    },
    [applyTextToWorkbench, handleCopyText],
  );

  const hasActiveFilter = Boolean(searchDraft.value.trim() || filterKind !== "全部");

  const resetFilters = useCallback(() => {
    setFilterKind("全部");
    if (searchDraft.value) {
      searchDraft.setValue("", true);
      invokeSafe(() => Prompts.setPromptFilter(""), refreshPrompts);
    }
  }, [searchDraft]);

  const noteIsError = Boolean(
    st.note && (st.note.includes("不能为空") || st.note.includes("请先")),
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-paper">
      {/* 顶部悬浮工具栏：自由生图全局后缀 + 新建模板 */}
      <PageToolbar
        left={
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex items-center gap-1.5">
              <Switch
                data-testid="prompts-suffix-checkbox"
                size="small"
                checked={Boolean(st.useSuffix)}
                aria-label="启用生图全局后缀"
                onCheckedChange={handleToggleSuffix}
              />
              <span className="text-xs font-medium text-ink select-none">全局后缀</span>
              <Tooltip
                content="仅在「自由生图」发送时换行追加到提示词末尾；批量洗图与裂变不附加"
                side="bottom"
              >
                <Badge
                  tabIndex={0}
                  text="仅自由生图"
                  variant={st.useSuffix ? "accent" : "neutral"}
                  className="focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                />
              </Tooltip>
            </div>
            <Input
              ref={suffixInputRef}
              data-testid="prompts-suffix-input"
              size="small"
              value={suffixDraft.value}
              placeholder="如：4K 超清摄影质感，无水印"
              aria-label="全局提示词后缀"
              onChange={(e) => suffixDraft.onChange(e.target.value)}
              onCompositionStart={suffixDraft.onCompositionStart}
              onCompositionEnd={(e) => suffixDraft.onCompositionEnd(e.currentTarget.value)}
              onBlur={suffixDraft.onBlur}
              rightSlot={
                suffixDraft.value ? (
                  <button
                    type="button"
                    aria-label="清空全局后缀"
                    onClick={() => {
                      suffixDraft.setValue("", true);
                      invokeSafe(
                        () => Prompts.setPromptSuffix("", Boolean(st.useSuffix)),
                        refreshPrompts,
                      );
                    }}
                    className="flex size-5 cursor-pointer items-center justify-center rounded-[6px] text-subtle transition-colors hover:bg-sunken hover:text-ink"
                  >
                    <Icon name="x" size={11} />
                  </button>
                ) : null
              }
              wrapperClassName="w-[180px] sm:w-[220px] lg:w-[272px]"
              className="w-[180px] sm:w-[220px] lg:w-[272px]"
            />
          </div>
        }
        right={
          <Button
            data-testid="prompts-new-btn"
            text="新建模板"
            size="small"
            icon="plus"
            variant="primary"
            onClick={() => openNewDialog()}
          />
        }
      />

      {/* 双栏 Bento 提示词工作区：左侧统一目录卡 + 右侧全高提示词工作室 */}
      {loading && !promptsSlice ? (
        <PromptDirectorySkeleton />
      ) : (
        <div className="flex min-h-0 flex-1 gap-3 px-3 pb-3">
          {/* 左栏：统一提示词目录卡（搜索、分类筛选与模板列表合一） */}
          <FlushPane
            data-testid="prompts-list"
            className="w-[244px] flex-none lg:w-[276px] xl:w-[296px]"
          >
            <div className="flex shrink-0 items-center justify-between gap-2 border-b border-divider bg-card-header px-3.5 py-2.5">
              <div className="flex min-w-0 items-center gap-2">
                <Icon name="book-open" size={14} className="text-subtle" />
                <span className="truncate text-[13px] font-semibold text-ink">提示词库</span>
              </div>
              <Badge
                data-testid="prompts-filter-count"
                text={`${filteredItems.length} 个`}
                variant="neutral"
              />
            </div>

            <div className="flex shrink-0 flex-col gap-2 border-b border-divider px-3 py-2.5">
              <Input
                data-testid="prompts-search-input"
                size="small"
                leftIcon="search"
                value={searchDraft.value}
                placeholder="搜索模板名称或正文..."
                aria-label="搜索模板"
                onChange={(e) => searchDraft.onChange(e.target.value)}
                onCompositionStart={searchDraft.onCompositionStart}
                onCompositionEnd={(e) => searchDraft.onCompositionEnd(e.currentTarget.value)}
                onBlur={searchDraft.onBlur}
                rightSlot={
                  searchDraft.value ? (
                    <button
                      type="button"
                      aria-label="清空搜索"
                      onClick={() => {
                        searchDraft.setValue("", true);
                        invokeSafe(() => Prompts.setPromptFilter(""), refreshPrompts);
                      }}
                      className="flex size-5 cursor-pointer items-center justify-center rounded-[6px] text-subtle transition-colors hover:bg-sunken hover:text-ink"
                    >
                      <Icon name="x" size={11} />
                    </button>
                  ) : null
                }
              />

              <div
                role="tablist"
                aria-label="模板分类筛选"
                className="flex items-center gap-0.5 rounded-[10px] border border-hairline/70 bg-sunken p-0.5"
              >
                {FILTER_KINDS.map((k) => {
                  const active = filterKind === k;
                  return (
                    <button
                      key={k}
                      type="button"
                      role="tab"
                      aria-selected={active}
                      data-testid={`prompts-kind-filter-${k}`}
                      onClick={() => setFilterKind(k)}
                      className={cn(
                        "tnum min-w-0 flex-1 cursor-pointer rounded-[8px] px-1 py-1 text-center text-[11px] whitespace-nowrap transition-colors duration-150 select-none",
                        active
                          ? "bg-accent font-semibold text-on-accent"
                          : "font-medium text-muted hover:bg-sunken-hover hover:text-ink",
                      )}
                    >
                      {k}
                    </button>
                  );
                })}
              </div>
            </div>

            <VirtualList
              data-testid="prompts-template-list"
              className="scroll-gutter min-h-0 flex-1 p-2.5"
              items={filteredItems}
              getItemKey={(it, idx) => it.id || it.name || idx}
              estimateSize={74}
              gap={6}
              empty={
                <EmptyNotice
                  icon="book-open"
                  title={hasActiveFilter ? "没有匹配的模板" : "暂无提示词模板"}
                  subtitle={
                    hasActiveFilter
                      ? "试试更换关键词，或直接新建一条当前分类模板"
                      : "创建常用生图、洗图或裂变提示词，一键填入工作台"
                  }
                  action={
                    <div className="flex flex-wrap items-center justify-center gap-1.5">
                      {hasActiveFilter ? (
                        <Button
                          text="重置筛选"
                          size="small"
                          variant="outline"
                          onClick={resetFilters}
                        />
                      ) : null}
                      <Button
                        text={filterKind !== "全部" ? `新建「${filterKind}」模板` : "新建模板"}
                        size="small"
                        icon="plus"
                        variant="primary"
                        onClick={() => openNewDialog(filterKind)}
                      />
                    </div>
                  }
                  className="my-2 px-3 py-6"
                />
              }
              renderItem={(item) => {
                const selected = item.id === st.selectedId;
                const itemKind = normalizeKindLabel(item.kindLbl, item.kind);
                const itemChars = (item.text || "").length;
                return (
                  <ContextMenu items={buildRowContextMenu(item)}>
                    <div className="w-full">
                      <InsetPillRow
                        data-testid={`prompt-item-${item.id}`}
                        active={selected}
                        onClick={() => handleSelectTemplate(item.id || "")}
                        className="gap-1.5 px-3 py-2.5"
                      >
                        <div className="flex items-center justify-between gap-1.5">
                          <div className="flex min-w-0 flex-1 items-center gap-1.5">
                            <span
                              className={cn(
                                "min-w-0 truncate text-xs",
                                selected
                                  ? "font-semibold text-accent"
                                  : "font-medium text-ink",
                              )}
                            >
                              {item.name || item.label}
                            </span>
                            {selected && isDirty ? (
                              <Tooltip content="有未保存改动" side="top">
                                <span
                                  tabIndex={0}
                                  aria-label="有未保存改动"
                                  className="size-1.5 shrink-0 rounded-full bg-status-warn focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--focus-ring)]"
                                />
                              </Tooltip>
                            ) : null}
                          </div>
                          <div className="flex shrink-0 items-center gap-1">
                            {item.builtin ? (
                              <span
                                className={cn(
                                  "rounded-[5px] border px-1 py-px text-[10px] leading-3.5 font-medium select-none",
                                  selected
                                    ? "border-accent-border bg-card/80 text-accent"
                                    : "border-hairline bg-card text-muted",
                                )}
                              >
                                内置
                              </span>
                            ) : null}
                            <Badge text={itemKind} variant={kindBadgeVariant(itemKind)} />
                          </div>
                        </div>
                        <p
                          className={cn(
                            "line-clamp-2 text-[11px] leading-[1.45]",
                            selected ? "text-ink/80" : "text-muted",
                          )}
                        >
                          {item.text || "（空内容）"}
                        </p>
                        <div className="flex items-center justify-between text-[10px] leading-none">
                          <span
                            className={cn(
                              "tnum",
                              selected ? "text-accent/80" : "text-subtle",
                            )}
                          >
                            {itemChars} 字
                          </span>
                        </div>
                      </InsetPillRow>
                    </div>
                  </ContextMenu>
                );
              }}
            />
          </FlushPane>

          {/* 右栏：全高提示词编辑器工作室 */}
          <FlushPane data-testid="prompts-editor" className="min-w-0 flex-1">
            {/* 工作室顶部操作栏 */}
            <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-divider bg-card-header px-4 py-2.5">
              <div className="flex min-w-0 flex-1 items-center gap-2">
                <Icon
                  name={st.selectedId ? kindIcon(draftKind) : "book-open"}
                  size={15}
                  className={st.selectedId ? "text-accent" : "text-subtle"}
                />
                <span className="truncate text-[13px] font-semibold text-ink">
                  {st.selectedId
                    ? nameDraft.value.trim() || st.selectedName || "未命名模板"
                    : "选择或新建模板"}
                </span>
                {st.selectedId && st.selectedIsBln ? (
                  <Badge text="内置模板" variant="neutral" />
                ) : null}
                {isDirty ? (
                  <Badge
                    data-testid="prompts-dirty-badge"
                    dot
                    text="未保存"
                    variant="warn"
                  />
                ) : st.note ? (
                  <Badge
                    text={st.note}
                    variant={noteIsError ? "danger" : "success"}
                  />
                ) : null}
              </div>

              {st.selectedId ? (
                <div className="flex shrink-0 items-center gap-1.5">
                  {/* 目标感知的拆分填入按钮 */}
                  <div className="inline-flex items-center">
                    <Button
                      data-testid="prompts-apply-btn"
                      text={targetMeta.buttonText}
                      size="small"
                      icon={kindIcon(draftKind)}
                      variant="outline"
                      onClick={() => void handleApplyCurrent()}
                      className="rounded-r-none"
                    />
                    <DropdownMenu
                      align="end"
                      side="bottom"
                      items={applyMenuItems}
                      trigger={
                        <Button
                          data-testid="prompts-apply-menu-btn"
                          size="small"
                          variant="outline"
                          icon="chevron-down"
                          iconSize={12}
                          aria-label="选择填入目标工作台"
                          className="-ml-px rounded-l-none px-1.5"
                        />
                      }
                    />
                  </div>

                  <Tooltip
                    content={
                      st.selectedIsBln
                        ? "内置模板受保护，保存将自动另存为自定义副本"
                        : `保存修改 (${modKeyLabel()}S)`
                    }
                    side="bottom"
                  >
                    <Button
                      data-testid="prompts-save-btn"
                      text={st.selectedIsBln ? "另存副本" : "保存"}
                      size="small"
                      icon="save"
                      variant="primary"
                      disabled={!isDirty || !textDraft.value.trim()}
                      onClick={handleSaveEdits}
                    />
                  </Tooltip>

                  <DropdownMenu
                    align="end"
                    side="bottom"
                    items={overflowMenuItems}
                    trigger={
                      <IconButton
                        data-testid="prompts-more-btn"
                        icon="ellipsis"
                        size="small"
                        variant="ghost"
                        aria-label="更多模板操作"
                      />
                    }
                  />
                </div>
              ) : null}
            </div>

            {st.selectedId ? (
              <div className="flex min-h-0 flex-1 flex-col">
                {/* 元数据与视图模式条 */}
                <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-divider/70 bg-card px-4 py-2.5">
                  <Input
                    data-testid="prompts-edit-name"
                    size="small"
                    value={nameDraft.value}
                    placeholder="模板名称"
                    aria-label="模板名称"
                    onChange={(e) => nameDraft.onChange(e.target.value)}
                    onCompositionStart={nameDraft.onCompositionStart}
                    onCompositionEnd={(e) =>
                      nameDraft.onCompositionEnd(e.currentTarget.value)
                    }
                    onBlur={nameDraft.onBlur}
                    className="min-w-[140px] flex-1"
                  />

                  <div className="w-[96px] shrink-0">
                    <Select
                      data-testid="prompts-edit-kind"
                      size="small"
                      aria-label="模板分类"
                      options={editorKindOptions}
                      value={draftKind}
                      onValueChange={setDraftKind}
                    />
                  </div>

                  <div
                    role="tablist"
                    aria-label="正文视图模式"
                    className="inline-flex shrink-0 items-center gap-0.5 rounded-[9px] border border-hairline/70 bg-sunken p-0.5"
                  >
                    <button
                      type="button"
                      role="tab"
                      aria-selected={viewMode === "edit"}
                      data-testid="prompts-view-edit"
                      onClick={() => setViewMode("edit")}
                      className={cn(
                        "cursor-pointer rounded-[7px] px-2 py-0.5 text-[11px] transition-colors select-none",
                        viewMode === "edit"
                          ? "bg-card font-semibold text-ink shadow-[0_1px_2px_rgba(15,23,42,0.06)] dark:shadow-none"
                          : "font-medium text-muted hover:text-ink",
                      )}
                    >
                      编辑
                    </button>
                    <button
                      type="button"
                      role="tab"
                      aria-selected={viewMode === "reader"}
                      data-testid="prompts-view-reader"
                      onClick={() => setViewMode("reader")}
                      className={cn(
                        "cursor-pointer rounded-[7px] px-2 py-0.5 text-[11px] transition-colors select-none",
                        viewMode === "reader"
                          ? "bg-card font-semibold text-ink shadow-[0_1px_2px_rgba(15,23,42,0.06)] dark:shadow-none"
                          : "font-medium text-muted hover:text-ink",
                      )}
                    >
                      分段阅读
                    </button>
                  </div>

                  <TooltipGroup className="flex shrink-0 items-center gap-1">
                    <Tooltip content="复制正文到剪贴板" side="bottom">
                      <Button
                        data-testid="prompts-copy-btn"
                        text="复制"
                        size="xs"
                        icon="copy"
                        variant="ghost"
                        disabled={!textDraft.value.trim()}
                        onClick={() =>
                          void handleCopyText(textDraft.value, "已复制提示词正文")
                        }
                      />
                    </Tooltip>

                    {st.selectedIsBln ? (
                      <Button
                        data-testid="prompts-restore-btn"
                        text="恢复默认"
                        size="xs"
                        icon="rotate-ccw"
                        variant="ghost"
                        disabled={!isDirty}
                        onClick={handleRestoreBuiltin}
                      />
                    ) : confirmDelete ? (
                      <div className="animate-tulai-banner-in flex items-center gap-1 rounded-[8px] border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] px-1.5 py-0.5">
                        <Button
                          data-testid="prompts-delete-confirm-btn"
                          text="确认删除"
                          size="xs"
                          variant="danger"
                          onClick={handleDeleteUserPrompt}
                        />
                        <Button
                          text="取消"
                          size="xs"
                          variant="ghost"
                          onClick={() => setConfirmDelete(false)}
                        />
                      </div>
                    ) : (
                      <>
                        {isDirty ? (
                          <Button
                            data-testid="prompts-discard-btn"
                            text="撤销修改"
                            size="xs"
                            icon="undo-2"
                            variant="ghost"
                            onClick={handleDiscardEdits}
                          />
                        ) : null}
                        <Button
                          data-testid="prompts-delete-btn"
                          text="删除"
                          size="xs"
                          icon="trash-2"
                          variant="ghost"
                          className="hover:text-status-danger"
                          onClick={() => setConfirmDelete(true)}
                        />
                      </>
                    )}
                  </TooltipGroup>
                </div>

                {/* 内置模板编辑提示条（仅在编辑内置模板有改动时出现） */}
                {st.selectedIsBln && isDirty ? (
                  <div className="animate-tulai-banner-in flex shrink-0 items-center justify-between gap-2 border-b border-accent-border/70 bg-accent-soft px-4 py-1.5 text-[11px] text-accent">
                    <span>
                      正在编辑内置模板：点击「另存副本」会新建自定义模板，内置原版保持不变。
                    </span>
                    <button
                      type="button"
                      onClick={handleRestoreBuiltin}
                      className="shrink-0 cursor-pointer font-semibold underline underline-offset-2 hover:opacity-80"
                    >
                      恢复默认
                    </button>
                  </div>
                ) : null}

                {/* 正文主工作区：文本编辑 / 分段阅读 */}
                <div className="flex min-h-0 flex-1 flex-col p-3">
                  {viewMode === "edit" ? (
                    <Textarea
                      key="edit"
                      ref={editorBodyRef}
                      data-testid="prompts-edit-body"
                      value={textDraft.value}
                      placeholder="填写提示词正文内容，支持空行分段或每行一条描述..."
                      aria-label="提示词正文"
                      onChange={(e) => textDraft.onChange(e.target.value)}
                      onCompositionStart={textDraft.onCompositionStart}
                      onCompositionEnd={(e) =>
                        textDraft.onCompositionEnd(e.currentTarget.value)
                      }
                      onBlur={textDraft.onBlur}
                      className="animate-tulai-panel-in scroll-gutter min-h-0 flex-1 rounded-[12px] border-hairline bg-sunken/70 px-4 py-3 text-[13px] leading-[1.7] text-ink focus:bg-field"
                    />
                  ) : (
                    <div
                      key="reader"
                      data-testid="prompts-reader-view"
                      className="animate-tulai-panel-in scroll-gutter min-h-0 flex-1 overflow-y-auto rounded-[12px] border border-hairline bg-sunken/50 p-4"
                    >
                      {blocks.length === 0 ? (
                        <div className="flex h-full items-center justify-center text-xs text-muted">
                          暂无正文内容，切回「编辑」即可编写提示词
                        </div>
                      ) : (
                        <div className="mx-auto flex max-w-[74ch] flex-col gap-3">
                          {blocks.map((blk, blkIdx) => (
                            <div
                              key={blk.id}
                              style={{ animationDelay: `${Math.min(blkIdx * 25, 150)}ms` }}
                              onDoubleClick={() => {
                                setViewMode("edit");
                                queueMicrotask(() => editorBodyRef.current?.focus());
                              }}
                              className="animate-tulai-stagger-item group rounded-[12px] border border-hairline bg-card px-4 py-3 transition-colors duration-150 hover:border-strong"
                            >
                              <div className="flex items-start justify-between gap-3">
                                <div className="min-w-0 flex-1">
                                  {blk.heading ? (
                                    <div className="mb-1.5 flex items-center gap-2">
                                      <span className="inline-flex items-center rounded-[6px] border border-accent-border bg-accent-soft px-2 py-0.5 text-[11px] font-semibold text-accent">
                                        {blk.heading}
                                      </span>
                                    </div>
                                  ) : null}
                                  <p className="select-text whitespace-pre-wrap text-[13px] leading-[1.75] text-ink">
                                    {blk.body}
                                  </p>
                                </div>
                                <Tooltip content="复制此段" side="left">
                                  <IconButton
                                    icon="copy"
                                    size="xs"
                                    variant="ghost"
                                    aria-label="复制此段"
                                    onClick={() =>
                                      void handleCopyText(
                                        blk.heading ? `${blk.heading}：${blk.body}` : blk.body,
                                        "已复制该段内容",
                                      )
                                    }
                                    className="touch-visible opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                                  />
                                </Tooltip>
                              </div>
                            </div>
                          ))}
                          <div className="pt-1 text-center text-[11px] text-subtle">
                            双击任意段落或切换回「编辑」即可修改内容
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </div>

                {/* 底部紧凑状态与生图后缀联调条（替代旧版占地方的独立预览大卡片） */}
                <div
                  className={cn(
                    "flex shrink-0 flex-col border-t border-divider bg-card-header px-4 py-2",
                    import.meta.env.DEV && "pr-[104px]",
                  )}
                >
                  {previewExpanded && supportsGlobalSuffix && suffixActive && combinedPreview ? (
                    <div className="animate-tulai-banner-in mb-2 rounded-[10px] border border-accent-border/80 bg-accent-soft/60 px-3 py-2">
                      <div className="mb-1 flex items-center justify-between gap-2">
                        <span className="text-[11px] font-semibold text-accent">
                          自由生图实际发送内容（正文换行附加全局后缀）
                        </span>
                        <div className="flex items-center gap-1">
                          <Button
                            text="复制完整"
                            size="xs"
                            icon="copy"
                            variant="ghost"
                            onClick={() =>
                              void handleCopyText(combinedPreview, "已复制含后缀完整提示词")
                            }
                          />
                          <Button
                            text="收起"
                            size="xs"
                            variant="ghost"
                            onClick={() => setPreviewExpanded(false)}
                          />
                        </div>
                      </div>
                      <pre className="max-h-[120px] overflow-y-auto whitespace-pre-wrap font-sans text-xs leading-relaxed text-ink select-text">
                        {combinedPreview}
                      </pre>
                    </div>
                  ) : null}

                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2.5 text-[11px] text-muted">
                      <span data-testid="prompts-preview-stats" className="tnum">
                        {charCount} 字符 · {lineCount} 行 · {blocks.length} 段
                      </span>
                      {isDirty ? (
                        <span className="text-subtle">· {modKeyLabel()}S 保存</span>
                      ) : null}
                    </div>

                    <div className="flex min-w-0 max-w-full items-center gap-2">
                      {!supportsGlobalSuffix ? (
                        <span
                          data-testid="prompts-combined-preview"
                          className="truncate text-[11px] text-muted"
                        >
                          「{draftKind}」模式发送时不附加全局后缀
                        </span>
                      ) : suffixActive ? (
                        <div className="flex min-w-0 items-center gap-1.5">
                          <span className="shrink-0 text-[11px] font-medium text-accent">
                            发送预览
                          </span>
                          <Tooltip content={combinedPreview} side="top">
                            <span
                              tabIndex={0}
                              data-testid="prompts-combined-preview"
                              className="max-w-[320px] truncate rounded text-[11px] text-ink/85 select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                            >
                              {combinedPreview}
                            </span>
                          </Tooltip>
                          <Button
                            text={previewExpanded ? "收起" : "展开"}
                            size="xs"
                            variant="ghost"
                            onClick={() => setPreviewExpanded((v) => !v)}
                          />
                        </div>
                      ) : (
                        <button
                          type="button"
                          data-testid="prompts-combined-preview"
                          onClick={() => suffixInputRef.current?.focus()}
                          className="cursor-pointer truncate text-[11px] text-muted transition-colors hover:text-ink"
                        >
                          {trimmedSuffix
                            ? `全局后缀未启用（已填：${trimmedSuffix}）`
                            : "未设置全局后缀（仅自由生图生效）"}
                        </button>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            ) : (
              <div className="flex flex-1 items-center justify-center p-6">
                <EmptyNotice
                  icon="book-open"
                  title="请从左侧选择一个模板"
                  subtitle="选择后可编辑内容、分段阅读，或按分类一键填入工作台"
                  action={
                    <Button
                      text="新建模板"
                      size="small"
                      icon="plus"
                      variant="primary"
                      onClick={() => openNewDialog()}
                    />
                  }
                />
              </div>
            )}
          </FlushPane>
        </div>
      )}

      {/* 新建模板弹窗 */}
      <Dialog
        open={newOpen}
        onOpenChange={setNewOpen}
        title="新建提示词模板"
        width={520}
        closeOnBackdrop
        closeOnEscape
        data-testid="prompts-new-dialog"
        footer={
          <>
            <Button text="取消" size="small" variant="ghost" onClick={closeNewDialog} />
            <Button
              data-testid="prompts-create-confirm"
              text="创建模板"
              size="small"
              variant="primary"
              disabled={newName.trim() === "" || newText.trim() === ""}
              onClick={handleCreateTemplate}
            />
          </>
        }
      >
        <div
          className="flex w-full min-w-0 flex-col gap-3"
          onKeyDown={(e) => {
            if (!e.nativeEvent.isComposing && (e.metaKey || e.ctrlKey) && e.key === "Enter") {
              e.preventDefault();
              handleCreateTemplate();
            }
          }}
        >
          <div className="flex items-center gap-2">
            <Input
              ref={newNameInputRef}
              data-testid="prompts-new-name"
              value={newName}
              placeholder="模板名称，如：电商白底主图"
              aria-label="新建模板名称"
              onChange={(e) => setNewName(e.target.value)}
              className="flex-1"
            />
            <div className="w-[110px] shrink-0">
              <Select
                data-testid="prompts-new-kind"
                aria-label="新建模板分类"
                options={KIND_OPTIONS}
                value={newKind}
                onValueChange={setNewKind}
              />
            </div>
          </div>
          <Textarea
            data-testid="prompts-new-body"
            value={newText}
            placeholder="输入模板正文内容..."
            aria-label="新建模板内容"
            onChange={(e) => setNewText(e.target.value)}
            className="h-[180px] rounded-[12px] border-hairline bg-sunken/70 p-3 text-[13px] leading-[1.65] focus:bg-field"
          />
          <div className="flex items-center justify-between text-[11px] text-muted">
            <span>{getTargetWorkbench(newKind).label}模式下可直接从模板列表填入</span>
            <span className="tnum">{newText.length} 字符</span>
          </div>
        </div>
      </Dialog>
    </div>
  );
}
