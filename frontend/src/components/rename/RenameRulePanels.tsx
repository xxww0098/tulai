import { useRef, useState } from "react";
import {
  Button,
  Checkbox,
  DropdownMenu,
  Icon,
  IconButton,
  Input,
  Tooltip,
} from "@/components/ui";
import { cn } from "@/lib/cn";
import { Rename } from "@/lib/ipc";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import { showToast, type RenameRuleDTO, type RenameStateDTO } from "@/store";
import {
  DEFAULT_TEMPLATE_NAMES,
  INSERT_ATTR_OPTIONS,
  INSERT_CONTENT_TABS,
  INSERT_POSITION_OPTIONS,
  NUMBER_FORMAT_OPTIONS,
  formatNumberSample,
  isRegexFind,
  resolveInsertTab,
  resolvePositionValue,
  runIpc,
  toggleRegexFind,
} from "./renameHelpers";

function ClearCircleButton({
  ariaLabel,
  disabled,
  onClick,
}: {
  ariaLabel: string;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      tabIndex={-1}
      disabled={disabled}
      aria-label={ariaLabel}
      onClick={onClick}
      className="flex size-4 shrink-0 cursor-pointer items-center justify-center rounded-full text-subtle transition-colors hover:bg-black/10 hover:text-ink disabled:pointer-events-none disabled:opacity-40 dark:hover:bg-white/10"
    >
      <Icon name="circle-x" size={14} />
    </button>
  );
}

const MATERIAL_BOX_CLASSES = cn(
  "flex min-w-0 items-center gap-2 rounded-t-[4px] border-b border-[#595959] bg-[#EBEBEB] px-3.5",
  "transition-[border-color,background-color] duration-150",
  "hover:bg-[#E5E5E5] focus-within:border-b-2 focus-within:border-accent focus-within:bg-[#E8E8E8]",
  "dark:border-strong dark:bg-sunken dark:hover:bg-sunken-hover dark:focus-within:border-accent dark:focus-within:bg-field",
);

function RenameRuleRow({
  rule,
  templateNames,
  canDelete,
  busy,
}: {
  rule: RenameRuleDTO;
  templateNames: string[];
  canDelete: boolean;
  busy: boolean;
}) {
  const findRef = useRef(rule.find || "");
  const withRef = useRef(rule.with || "");
  const textRef = useRef(rule.text || "");
  const countRef = useRef(rule.count ?? 1);
  const offsetRef = useRef(rule.offset ?? 0);

  const commitRule = (patch: Partial<RenameRuleDTO>) => {
    const nextFind = patch.find ?? findRef.current;
    const nextWith = patch.with ?? withRef.current;
    const nextText = patch.text ?? textRef.current;
    const nextTpl = patch.template ?? rule.template;
    const nextCount = patch.count ?? countRef.current;
    const nextOffset = patch.offset ?? offsetRef.current;
    runIpc(() =>
      Rename.renameSetRule(
        rule.index,
        nextFind,
        nextWith,
        nextText,
        nextTpl,
        nextCount,
        nextOffset,
      ),
    );
  };

  const findDraft = useSyncedDraft(
    rule.find || "",
    `rn-rule-find-${rule.index}`,
    (nextFind) => {
      findRef.current = nextFind;
      commitRule({ find: nextFind });
    },
    { debounceMs: 90 },
  );

  const withDraft = useSyncedDraft(
    rule.with || "",
    `rn-rule-with-${rule.index}`,
    (nextWith) => {
      withRef.current = nextWith;
      commitRule({ with: nextWith });
    },
    { debounceMs: 90 },
  );

  const textDraft = useSyncedDraft(
    rule.text || "",
    `rn-rule-text-${rule.index}`,
    (nextText) => {
      textRef.current = nextText;
      commitRule({ text: nextText });
    },
    { debounceMs: 90 },
  );

  const countDraft = useSyncedDraft(
    String(rule.count ?? 1),
    `rn-rule-count-${rule.index}`,
    (raw) => {
      const parsed = Math.max(0, Number.parseInt(raw, 10) || 0);
      countRef.current = parsed;
      commitRule({ count: parsed });
    },
    { debounceMs: 90 },
  );

  const offsetDraft = useSyncedDraft(
    String(rule.offset ?? 0),
    `rn-rule-offset-${rule.index}`,
    (raw) => {
      const parsed = Math.max(0, Number.parseInt(raw, 10) || 0);
      offsetRef.current = parsed;
      commitRule({ offset: parsed });
    },
    { debounceMs: 90 },
  );

  findRef.current = findDraft.value;
  withRef.current = withDraft.value;
  textRef.current = textDraft.value;
  countRef.current = Math.max(0, Number.parseInt(countDraft.value, 10) || 0);
  offsetRef.current = Math.max(0, Number.parseInt(offsetDraft.value, 10) || 0);

  const tpl = rule.template ?? 0;
  const tplLabels = templateNames.length > 0 ? templateNames : DEFAULT_TEMPLATE_NAMES;
  const activeTplLabel = tplLabels[tpl] ?? "普通查找";
  const regexActive = tpl === 0 && isRegexFind(findDraft.value);
  const needsText = tpl >= 4 && tpl <= 7;
  const needsCount = tpl === 1 || tpl === 2 || tpl === 3 || tpl === 6 || tpl === 7;
  const needsOffset = tpl === 3;

  return (
    <div
      data-testid={`rename-rule-${rule.index}`}
      className="flex flex-row items-center gap-5"
    >
      {/* Left Material filled box: 查找 */}
      <div className={cn(MATERIAL_BOX_CLASSES, "group/find h-[54px] flex-1")}>
        {tpl === 0 ? (
          <>
            <input
              type="text"
              value={findDraft.value}
              disabled={busy}
              placeholder="查找"
              aria-label={`规则 ${rule.index + 1} 查找内容`}
              onChange={(e) => findDraft.onChange(e.target.value)}
              onCompositionStart={findDraft.onCompositionStart}
              onCompositionEnd={(e) => findDraft.onCompositionEnd(e.currentTarget.value)}
              onBlur={findDraft.onBlur}
              className="min-w-0 flex-1 bg-transparent text-[13.5px] text-[#1F2937] placeholder:text-[#6B7280] focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:text-ink dark:placeholder:text-subtle"
            />
            {findDraft.value ? (
              <ClearCircleButton
                ariaLabel="清空查找内容"
                disabled={busy}
                onClick={() => findDraft.setValue("", true)}
              />
            ) : null}
            <Tooltip
              content={regexActive ? "已启用正则表达式（/模式/g）" : "切换为正则表达式（/模式/g）"}
              side="top"
            >
              <button
                type="button"
                disabled={busy}
                aria-label="切换正则表达式"
                aria-pressed={regexActive}
                onClick={() => findDraft.setValue(toggleRegexFind(findDraft.value), true)}
                className={cn(
                  "inline-flex shrink-0 cursor-pointer items-center gap-0.5 rounded-[4px] px-1.5 py-0.5 font-mono-console text-[10.5px] leading-none transition-all",
                  regexActive
                    ? "bg-accent-soft font-semibold text-accent opacity-100"
                    : "touch-visible text-[#6B7280] opacity-0 group-hover/find:opacity-80 group-focus-within/find:opacity-80 hover:bg-black/5 hover:opacity-100 dark:text-subtle dark:hover:bg-white/10",
                )}
              >
                <Icon name="regex" size={11} />
                <span>正则</span>
              </button>
            </Tooltip>
          </>
        ) : (
          <div className="flex min-w-0 flex-1 items-center gap-2 text-xs text-muted">
            <span className="shrink-0 rounded-[4px] bg-black/5 px-1.5 py-0.5 text-[11px] font-medium text-[#374151] dark:bg-white/10 dark:text-ink">
              {activeTplLabel}
            </span>
            {needsText ? (
              <div className="flex min-w-[70px] flex-1 items-center gap-1">
                <input
                  type="text"
                  value={textDraft.value}
                  disabled={busy}
                  placeholder="锚点字符"
                  aria-label={`规则 ${rule.index + 1} 锚点字符`}
                  onChange={(e) => textDraft.onChange(e.target.value)}
                  onCompositionStart={textDraft.onCompositionStart}
                  onCompositionEnd={(e) => textDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={textDraft.onBlur}
                  className="min-w-0 flex-1 bg-transparent text-xs text-ink placeholder:text-[#6B7280] focus:outline-none"
                />
                {textDraft.value ? (
                  <ClearCircleButton
                    ariaLabel="清空锚点字符"
                    disabled={busy}
                    onClick={() => textDraft.setValue("", true)}
                  />
                ) : null}
              </div>
            ) : null}

            {needsOffset ? (
              <label className="inline-flex shrink-0 items-center gap-1 text-[11px] text-muted">
                <span>跳过</span>
                <input
                  type="text"
                  inputMode="numeric"
                  value={offsetDraft.value}
                  disabled={busy}
                  aria-label={`规则 ${rule.index + 1} 起始偏移`}
                  onChange={(e) => offsetDraft.onChange(e.target.value)}
                  onCompositionStart={offsetDraft.onCompositionStart}
                  onCompositionEnd={(e) => offsetDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={offsetDraft.onBlur}
                  className="tnum h-6 w-10 rounded-[4px] border border-strong bg-field px-1 text-center text-xs text-ink focus:border-accent focus:outline-none"
                />
                <span>位</span>
              </label>
            ) : null}

            {needsCount ? (
              <label className="inline-flex shrink-0 items-center gap-1 text-[11px] text-muted">
                <span>{tpl === 1 ? "前" : tpl === 2 ? "后" : tpl === 6 ? "后取" : tpl === 7 ? "前取" : "取"}</span>
                <input
                  type="text"
                  inputMode="numeric"
                  value={countDraft.value}
                  disabled={busy}
                  aria-label={`规则 ${rule.index + 1} 字符位数`}
                  onChange={(e) => countDraft.onChange(e.target.value)}
                  onCompositionStart={countDraft.onCompositionStart}
                  onCompositionEnd={(e) => countDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={countDraft.onBlur}
                  className="tnum h-6 w-10 rounded-[4px] border border-strong bg-field px-1 text-center text-xs text-ink focus:border-accent focus:outline-none"
                />
                <span>位</span>
              </label>
            ) : null}
          </div>
        )}

        {/* Solid dark circular chevron dropdown matching uTools */}
        <DropdownMenu
          align="end"
          trigger={
            <button
              type="button"
              disabled={busy}
              aria-label={`规则 ${rule.index + 1} 匹配方式`}
              className="flex size-[20px] shrink-0 cursor-pointer items-center justify-center rounded-full bg-[#616161] text-white transition-colors hover:bg-[#424242] disabled:cursor-not-allowed disabled:opacity-50 dark:bg-subtle dark:text-card dark:hover:bg-muted"
            >
              <Icon name="chevron-down" size={12} strokeWidth={2.5} />
            </button>
          }
          items={tplLabels.map((label, tplIdx) => ({
            id: `tpl-${tplIdx}`,
            label,
            icon: tpl === tplIdx ? "check" : undefined,
            onSelect: () => commitRule({ template: tplIdx }),
          }))}
        />
      </div>

      <Icon name="arrow-right" size={17} className="shrink-0 text-[#A3A3A3] dark:text-subtle" />

      {/* Right Material filled box: 替换成 */}
      <div className={cn(MATERIAL_BOX_CLASSES, "h-[54px] flex-1")}>
        <input
          type="text"
          value={withDraft.value}
          disabled={busy}
          placeholder="替换成"
          aria-label={`规则 ${rule.index + 1} 替换为`}
          onChange={(e) => withDraft.onChange(e.target.value)}
          onCompositionStart={withDraft.onCompositionStart}
          onCompositionEnd={(e) => withDraft.onCompositionEnd(e.currentTarget.value)}
          onBlur={withDraft.onBlur}
          className="min-w-0 flex-1 bg-transparent text-[13.5px] text-[#1F2937] placeholder:text-[#6B7280] focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:text-ink dark:placeholder:text-subtle"
        />
        {regexActive || tpl !== 0 ? (
          <Tooltip content="正则替换引用：$& 代表匹配原文，$1 代表第 1 个捕获组" side="top">
            <span className="shrink-0 font-mono-console text-[10px] text-subtle">$&amp; / $1</span>
          </Tooltip>
        ) : null}
        {withDraft.value ? (
          <ClearCircleButton
            ariaLabel="清空替换内容"
            disabled={busy}
            onClick={() => withDraft.setValue("", true)}
          />
        ) : null}
      </div>

      {canDelete ? (
        <Tooltip content="删除此规则" side="top">
          <IconButton
            icon="x"
            variant="ghost"
            size={24}
            disabled={busy}
            aria-label="删除规则"
            onClick={() => runIpc(() => Rename.renameRemoveRule(rule.index))}
          />
        </Tooltip>
      ) : null}
    </div>
  );
}

function SegmentedGroup<T extends string>({
  items,
  value,
  disabled,
  onChange,
  testIdPrefix,
  tone = "blue",
}: {
  items: readonly { value: T; label: string }[];
  value: T;
  disabled: boolean;
  onChange: (next: T) => void;
  testIdPrefix?: string;
  tone?: "blue" | "rose";
}) {
  return (
    <div className="inline-flex rounded-[5px] border border-[#D4D4D8] bg-card select-none dark:border-strong">
      {items.map((item, idx) => {
        const active = item.value === value;
        const activeCls =
          tone === "rose"
            ? "z-10 bg-[#FDF0F2] text-[#D94B64] ring-1 ring-[#F2B8C2] font-medium dark:bg-rose-950/50 dark:text-rose-300 dark:ring-rose-700/60"
            : "z-10 bg-[#EDF2FE] text-[#3B6CE2] ring-1 ring-[#9BB5F4] font-medium dark:bg-accent-soft dark:text-accent dark:ring-accent-border";

        return (
          <button
            key={item.value}
            type="button"
            disabled={disabled}
            data-testid={testIdPrefix ? `${testIdPrefix}-${item.value}` : undefined}
            aria-pressed={active}
            onClick={() => onChange(item.value)}
            className={cn(
              "relative h-[30px] cursor-pointer px-3 text-[12.5px] whitespace-nowrap transition-colors duration-150",
              "first:rounded-l-[4px] last:rounded-r-[4px]",
              "disabled:cursor-not-allowed disabled:opacity-50",
              idx > 0 && "border-l border-[#D4D4D8] dark:border-strong",
              active
                ? activeCls
                : "text-[#374151] hover:bg-[#F5F5F5] hover:text-[#111827] dark:text-muted dark:hover:bg-sunken dark:hover:text-ink",
            )}
          >
            {item.label}
          </button>
        );
      })}
    </div>
  );
}

export function RenameRulePanels({
  st,
  onCopyOriginalNames,
}: {
  st: RenameStateDTO;
  onCopyOriginalNames: () => void;
}) {
  const [lastAttrKind, setLastAttrKind] = useState("修改时间");
  const busy = Boolean(st.busy);
  const rules = st.rules ?? [];
  const rows = st.rows ?? [];
  const hasRows = rows.length > 0;
  const templateNames = st.templateNames ?? DEFAULT_TEMPLATE_NAMES;

  // Mode 2 (Insert) drafts
  const insertIndexDraft = useSyncedDraft(
    String(st.index ?? 1),
    "rn-insert-index",
    (val) => runIpc(() => Rename.renameSetField("index", val)),
    { debounceMs: 90 },
  );
  const insertTextDraft = useSyncedDraft(
    st.text || "",
    "rn-insert-text",
    (val) => runIpc(() => Rename.renameSetField("text", val)),
    { debounceMs: 90 },
  );
  const insertPrefixDraft = useSyncedDraft(
    st.prefix || "",
    "rn-insert-prefix",
    (val) => runIpc(() => Rename.renameSetField("prefix", val)),
    { debounceMs: 90 },
  );
  const insertSuffixDraft = useSyncedDraft(
    st.suffix || "",
    "rn-insert-suffix",
    (val) => runIpc(() => Rename.renameSetField("suffix", val)),
    { debounceMs: 90 },
  );

  // Mode 2/3 shared numbering drafts
  const numPrefixDraft = useSyncedDraft(
    st.numPrefix || "",
    "rn-num-prefix",
    (val) => runIpc(() => Rename.renameSetField("numPrefix", val)),
    { debounceMs: 90 },
  );
  const numSuffixDraft = useSyncedDraft(
    st.numSuffix || "",
    "rn-num-suffix",
    (val) => runIpc(() => Rename.renameSetField("numSuffix", val)),
    { debounceMs: 90 },
  );
  const numStartDraft = useSyncedDraft(
    String(st.numStart ?? 1),
    "rn-num-start",
    (val) => runIpc(() => Rename.renameSetField("numStart", val)),
    { debounceMs: 90 },
  );
  const numWidthDraft = useSyncedDraft(
    st.numWidth ? String(st.numWidth) : "",
    "rn-num-width",
    (val) => {
      const parsed = Math.max(0, Number.parseInt(val, 10) || 0);
      runIpc(() => Rename.renameSetField("numWidth", String(parsed)));
    },
    { debounceMs: 90 },
  );

  // Mode 4 (AI naming) drafts
  const aiPromptDraft = useSyncedDraft(
    st.aiPrompt || "",
    "rn-ai-prompt",
    (val) => runIpc(() => Rename.renameSetField("aiPrompt", val)),
    { debounceMs: 90 },
  );
  const aiUrlDraft = useSyncedDraft(
    st.aiUrl || "",
    "rn-ai-url",
    (val) => runIpc(() => Rename.renameSetField("aiUrl", val)),
    { debounceMs: 90 },
  );
  const aiKeyDraft = useSyncedDraft(
    st.aiKey || "",
    "rn-ai-key",
    (val) => runIpc(() => Rename.renameSetField("aiKey", val)),
    { debounceMs: 90 },
  );
  const aiModelDraft = useSyncedDraft(
    st.aiModel || "",
    "rn-ai-model",
    (val) => runIpc(() => Rename.renameSetField("aiModel", val)),
    { debounceMs: 90 },
  );

  const insertTab = resolveInsertTab(st.insertKind || "文本");
  const activeAttrKind = INSERT_ATTR_OPTIONS.some((o) => o.value === st.insertKind)
    ? st.insertKind
    : lastAttrKind;
  const activePosition = resolvePositionValue(st.position || "末尾");
  const activeNumFormat = st.numFormat || "阿拉伯数字";
  const isArabicFormat = activeNumFormat === "阿拉伯数字";
  const numSample = formatNumberSample(
    activeNumFormat,
    Number.parseInt(numStartDraft.value, 10) || 0,
    Number.parseInt(numWidthDraft.value, 10) || 0,
    numPrefixDraft.value,
    numSuffixDraft.value,
  );
  const showNumSample =
    Boolean(numPrefixDraft.value) ||
    Boolean(numSuffixDraft.value) ||
    (Number.parseInt(numWidthDraft.value, 10) || 0) > 0 ||
    activeNumFormat !== "阿拉伯数字";

  const handleSelectNumFormat = (fmt: string) => {
    runIpc(() => Rename.renameSetField("numFormat", fmt));
    if (
      (fmt === "英文大写" || fmt === "英文小写") &&
      (Number.parseInt(numStartDraft.value, 10) || 0) < 1
    ) {
      numStartDraft.setValue("1", true);
    }
  };

  const stepNumStart = (delta: number) => {
    const current = Number.parseInt(numStartDraft.value, 10) || 0;
    const next = Math.max(0, current + delta);
    numStartDraft.setValue(String(next), true);
  };

  const handleSelectInsertContent = (kind: "文本" | "序号" | "文件属性") => {
    if (kind === "文本") {
      if (st.prefix) insertPrefixDraft.setValue("", true);
      if (st.suffix) insertSuffixDraft.setValue("", true);
      runIpc(() => Rename.renameSetField("insertKind", "文本"));
    } else if (kind === "序号") {
      if (st.prefix) insertPrefixDraft.setValue("", true);
      if (st.suffix) insertSuffixDraft.setValue("", true);
      runIpc(() => Rename.renameSetField("insertKind", "序号"));
    } else {
      runIpc(() => Rename.renameSetField("insertKind", activeAttrKind));
    }
  };

  const triggerAISuggest = () => {
    aiPromptDraft.flush();
    if (!st.aiUrl.trim() || !st.aiKey.trim() || !st.aiModel.trim()) {
      runIpc(() => Rename.renameSetField("aiSettings", "true"));
      showToast("请先填写模型接口地址、API Key 和模型名称", "warning");
      return;
    }
    runIpc(() => Rename.renameSuggestAI());
  };

  // Mode 1 (手动编辑) has its own ultra-slim centered tip strip matching screenshot 5
  if (st.mode === 1) {
    return (
      <section
        key={st.mode}
        data-testid="rename-config"
        className="animate-tulai-panel-in relative flex shrink-0 items-center justify-center border-b border-[#E5E7EB] bg-card px-4 py-1.5 dark:border-divider"
      >
        <span className="text-center text-[11.5px] text-[#6B7280] dark:text-muted">
          点击任意新文件名即可修改；粘贴多行文本（如 Excel 列）将按行向下批量填入
        </span>
        {hasRows ? (
          <Button
            text="复制原文件名"
            size="xs"
            icon="copy"
            variant="ghost"
            disabled={busy}
            onClick={onCopyOriginalNames}
            className="absolute right-3"
          />
        ) : null}
      </section>
    );
  }

  return (
    <section
      key={st.mode}
      data-testid="rename-config"
      className={cn(
        "animate-tulai-panel-in shrink-0 bg-card px-4",
        hasRows ? "py-3.5" : "flex min-h-[224px] flex-col py-4",
      )}
    >
      {/* Mode 0: 查找替换 */}
      {st.mode === 0 ? (
        <div className="flex flex-1 flex-col justify-between gap-4">
          <div className="flex max-h-[168px] flex-col gap-2.5 overflow-y-auto pt-1 pr-0.5">
            {rules.map((rule) => (
              <RenameRuleRow
                key={rule.index}
                rule={rule}
                templateNames={templateNames}
                canDelete={rules.length > 1}
                busy={busy}
              />
            ))}
          </div>

          <div className="flex flex-wrap items-center justify-between gap-2 pt-2">
            <Checkbox
              label="替换文件扩展名"
              checked={Boolean(st.includeExt)}
              disabled={busy}
              onCheckedChange={(checked) =>
                runIpc(() => Rename.renameSetField("includeExt", checked ? "true" : "false"))
              }
            />
            <button
              type="button"
              disabled={busy}
              onClick={() => runIpc(() => Rename.renameAddRule())}
              className="inline-flex h-[32px] cursor-pointer items-center gap-1.5 rounded-[5px] border border-[#8BA3EA] bg-card px-3 text-xs font-medium text-[#4169E1] transition-colors hover:bg-[#F5F8FF] disabled:pointer-events-none disabled:opacity-45 dark:border-accent-border dark:text-accent dark:hover:bg-accent-soft"
            >
              <Icon name="plus" size={14} />
              <span>添加替换规则</span>
            </button>
          </div>
        </div>
      ) : null}

      {/* Mode 4: 智能重命名 */}
      {st.mode === 4 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2.5 px-4">
          <div
            className={cn(
              "flex w-full max-w-[840px] flex-col rounded-[14px] border border-[#E2E4E8] bg-card transition-[border-color,box-shadow] duration-150",
              "shadow-[0_4px_18px_rgba(0,0,0,0.06)] dark:border-hairline dark:bg-sunken/50 dark:shadow-none",
              "focus-within:border-accent focus-within:ring-2 focus-within:ring-[var(--focus-ring)]",
            )}
          >
            <textarea
              data-testid="rename-ai-prompt"
              rows={2}
              value={aiPromptDraft.value}
              disabled={busy}
              placeholder="用一句话描述重命名规则，例如：中文转英文 / 按日期排序并加序号 / 设备+拍摄时间 / 视频时长+码率"
              aria-label="AI 重命名规则"
              onChange={(e) => aiPromptDraft.onChange(e.target.value)}
              onCompositionStart={aiPromptDraft.onCompositionStart}
              onCompositionEnd={(e) => aiPromptDraft.onCompositionEnd(e.currentTarget.value)}
              onBlur={aiPromptDraft.onBlur}
              onKeyDown={(e) => {
                if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  triggerAISuggest();
                }
              }}
              className="w-full resize-none bg-transparent px-5 pt-4 pb-2 text-[13.5px] leading-relaxed text-[#1F2937] placeholder:text-[#9CA3AF] focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:text-ink dark:placeholder:text-subtle"
            />

            <div className="flex items-center justify-between gap-3 px-5 pt-1 pb-3.5">
              <button
                type="button"
                data-testid="rename-ai-settings-btn"
                onClick={() =>
                  runIpc(() =>
                    Rename.renameSetField("aiSettings", st.aiSettings ? "false" : "true"),
                  )
                }
                className="group inline-flex min-w-[136px] max-w-[240px] cursor-pointer items-center justify-between gap-3 border-b border-[#8C8C8C] pb-1 text-[13px] text-[#1F2937] transition-colors hover:border-[#374151] dark:border-strong dark:text-ink dark:hover:border-muted"
              >
                <span className="truncate">{st.aiModel || "配置 AI 模型"}</span>
                <svg
                  width="10"
                  height="6"
                  viewBox="0 0 10 6"
                  fill="currentColor"
                  aria-hidden="true"
                  className={cn(
                    "shrink-0 text-[#595959] transition-transform dark:text-subtle",
                    st.aiSettings && "rotate-180",
                  )}
                >
                  <path d="M0 0h10L5 6z" />
                </svg>
              </button>

              <Tooltip content="生成智能命名建议" shortcut="⌘/Ctrl+Enter" side="top">
                <IconButton
                  data-testid="rename-ai-submit-btn"
                  icon="send"
                  size={30}
                  variant={rows.length > 0 && aiPromptDraft.value.trim() ? "primary" : "ghost"}
                  busy={busy}
                  disabled={rows.length === 0 || busy}
                  aria-label="生成重命名建议"
                  onClick={triggerAISuggest}
                  className={
                    rows.length > 0 && aiPromptDraft.value.trim()
                      ? undefined
                      : "text-[#BDBDBD] hover:text-[#6B7280]"
                  }
                />
              </Tooltip>
            </div>
          </div>

          {st.aiSettings ? (
            <div
              data-testid="rename-ai-settings-panel"
              className="grid w-full max-w-[840px] grid-cols-1 gap-2 rounded-[10px] border border-hairline bg-sunken p-2.5 md:grid-cols-[1.3fr_1fr_1fr_auto] md:items-center"
            >
              <Input
                data-testid="rename-ai-url"
                size="small"
                value={aiUrlDraft.value}
                placeholder="OpenAI 兼容接口地址"
                aria-label="OpenAI 兼容接口地址"
                onChange={(e) => aiUrlDraft.onChange(e.target.value)}
                onCompositionStart={aiUrlDraft.onCompositionStart}
                onCompositionEnd={(e) => aiUrlDraft.onCompositionEnd(e.currentTarget.value)}
                onBlur={aiUrlDraft.onBlur}
              />
              <Input
                data-testid="rename-ai-key"
                size="small"
                masked
                value={aiKeyDraft.value}
                placeholder="API Key"
                aria-label="API Key"
                onChange={(e) => aiKeyDraft.onChange(e.target.value)}
                onCompositionStart={aiKeyDraft.onCompositionStart}
                onCompositionEnd={(e) => aiKeyDraft.onCompositionEnd(e.currentTarget.value)}
                onBlur={aiKeyDraft.onBlur}
              />
              <Input
                data-testid="rename-ai-model"
                size="small"
                value={aiModelDraft.value}
                placeholder="模型名称"
                aria-label="模型名称"
                onChange={(e) => aiModelDraft.onChange(e.target.value)}
                onCompositionStart={aiModelDraft.onCompositionStart}
                onCompositionEnd={(e) => aiModelDraft.onCompositionEnd(e.currentTarget.value)}
                onBlur={aiModelDraft.onBlur}
              />
              <Button
                text="保存"
                size="small"
                variant="outline"
                onClick={() => {
                  aiUrlDraft.flush();
                  aiKeyDraft.flush();
                  aiModelDraft.flush();
                  runIpc(() =>
                    Rename.renameSaveAIModel(
                      aiUrlDraft.value,
                      aiKeyDraft.value,
                      aiModelDraft.value,
                    ),
                  );
                }}
              />
            </div>
          ) : null}
        </div>
      ) : null}

      {/* Mode 2: 插入内容 */}
      {st.mode === 2 ? (
        <div className="flex flex-1 flex-col gap-7">
          <div className="flex flex-wrap items-center justify-between gap-2.5">
            <div className="flex items-center gap-1.5">
              <span className="text-[13px] font-medium text-[#1F2937] dark:text-ink">内容：</span>
              <SegmentedGroup
                items={INSERT_CONTENT_TABS}
                value={insertTab}
                disabled={busy}
                tone="blue"
                testIdPrefix="rename-insert-kind"
                onChange={handleSelectInsertContent}
              />
            </div>

            <div className="flex items-center gap-1.5">
              <span className="text-[13px] font-medium text-[#1F2937] dark:text-ink">位置：</span>
              <SegmentedGroup
                items={INSERT_POSITION_OPTIONS}
                value={activePosition}
                disabled={busy}
                tone="rose"
                testIdPrefix="rename-insert-pos"
                onChange={(pos) => runIpc(() => Rename.renameSetField("position", pos))}
              />
            </div>
          </div>

          {insertTab === "文本" ? (
            <div className="flex flex-wrap items-center gap-3">
              {activePosition === "指定位置" ? (
                <div className={cn(MATERIAL_BOX_CLASSES, "h-[54px] shrink-0 px-3")}>
                  <span className="text-xs text-[#6B7280] dark:text-muted">第</span>
                  <input
                    data-testid="rename-insert-index"
                    type="text"
                    inputMode="numeric"
                    value={insertIndexDraft.value}
                    disabled={busy}
                    aria-label="插入字符位置"
                    onChange={(e) => insertIndexDraft.onChange(e.target.value)}
                    onCompositionStart={insertIndexDraft.onCompositionStart}
                    onCompositionEnd={(e) =>
                      insertIndexDraft.onCompositionEnd(e.currentTarget.value)
                    }
                    onBlur={insertIndexDraft.onBlur}
                    className="tnum h-6 w-11 rounded-[4px] border border-strong bg-field px-1 text-center text-xs text-ink focus:border-accent focus:outline-none"
                  />
                  <span className="text-xs text-[#6B7280] dark:text-muted">个字符后</span>
                </div>
              ) : null}

              <div className={cn(MATERIAL_BOX_CLASSES, "h-[54px] min-w-[200px] flex-1")}>
                <input
                  data-testid="rename-insert-text"
                  type="text"
                  value={insertTextDraft.value}
                  disabled={busy}
                  placeholder="固定文本"
                  aria-label="要插入的文本内容"
                  onChange={(e) => insertTextDraft.onChange(e.target.value)}
                  onCompositionStart={insertTextDraft.onCompositionStart}
                  onCompositionEnd={(e) => insertTextDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={insertTextDraft.onBlur}
                  className="min-w-0 flex-1 bg-transparent text-[13.5px] text-[#1F2937] placeholder:text-[#6B7280] focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:text-ink dark:placeholder:text-subtle"
                />
                {insertTextDraft.value ? (
                  <ClearCircleButton
                    ariaLabel="清空插入文本"
                    disabled={busy}
                    onClick={() => insertTextDraft.setValue("", true)}
                  />
                ) : null}
              </div>
            </div>
          ) : null}

          {insertTab === "序号" ? (
            <div className="grid grid-cols-2 gap-2.5 md:grid-cols-6">
              {activePosition === "指定位置" ? (
                <div className={cn(MATERIAL_BOX_CLASSES, "h-[50px] flex-col items-stretch justify-center gap-0 py-1")}>
                  <span className="text-[10.5px] text-[#6B7280] dark:text-subtle">插入字符位</span>
                  <input
                    data-testid="rename-insert-index"
                    type="text"
                    inputMode="numeric"
                    value={insertIndexDraft.value}
                    disabled={busy}
                    aria-label="插入字符位置"
                    onChange={(e) => insertIndexDraft.onChange(e.target.value)}
                    onCompositionStart={insertIndexDraft.onCompositionStart}
                    onCompositionEnd={(e) =>
                      insertIndexDraft.onCompositionEnd(e.currentTarget.value)
                    }
                    onBlur={insertIndexDraft.onBlur}
                    className="tnum bg-transparent text-[13px] text-ink focus:outline-none"
                  />
                </div>
              ) : null}
              <DropdownMenu
                align="start"
                trigger={
                  <button
                    type="button"
                    disabled={busy}
                    aria-label="插入序号格式"
                    className={cn(
                      MATERIAL_BOX_CLASSES,
                      "h-[50px] w-full cursor-pointer justify-between py-1 text-left",
                    )}
                  >
                    <div className="flex min-w-0 flex-col">
                      <span className="text-[10.5px] text-[#6B7280] dark:text-subtle">序号类型</span>
                      <span className="truncate text-[13px] text-[#111827] dark:text-ink">
                        {activeNumFormat}
                      </span>
                    </div>
                    <svg width="9" height="5" viewBox="0 0 10 6" fill="currentColor" aria-hidden="true" className="shrink-0 text-[#595959] dark:text-subtle">
                      <path d="M0 0h10L5 6z" />
                    </svg>
                  </button>
                }
                items={NUMBER_FORMAT_OPTIONS.map((opt) => ({
                  id: `insert-num-fmt-${opt.value}`,
                  label: opt.label,
                  icon: activeNumFormat === opt.value ? "check" : undefined,
                  onSelect: () => handleSelectNumFormat(opt.value),
                }))}
              />
              <div className={cn(MATERIAL_BOX_CLASSES, "h-[50px] flex-col items-stretch justify-center gap-0 py-1")}>
                <span className="text-[10.5px] text-[#6B7280] dark:text-subtle">序号前缀</span>
                <input
                  type="text"
                  value={numPrefixDraft.value}
                  disabled={busy}
                  placeholder="如 _"
                  aria-label="序号前缀"
                  onChange={(e) => numPrefixDraft.onChange(e.target.value)}
                  onCompositionStart={numPrefixDraft.onCompositionStart}
                  onCompositionEnd={(e) => numPrefixDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={numPrefixDraft.onBlur}
                  className="bg-transparent text-[13px] text-ink placeholder:text-[#6B7280] focus:outline-none dark:placeholder:text-subtle"
                />
              </div>
              <div className={cn(MATERIAL_BOX_CLASSES, "h-[50px] flex-col items-stretch justify-center gap-0 py-1")}>
                <span className="text-[10.5px] text-[#6B7280] dark:text-subtle">开始序号</span>
                <input
                  type="text"
                  inputMode="numeric"
                  value={numStartDraft.value}
                  disabled={busy}
                  aria-label="起始序号"
                  onChange={(e) => numStartDraft.onChange(e.target.value)}
                  onCompositionStart={numStartDraft.onCompositionStart}
                  onCompositionEnd={(e) => numStartDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={numStartDraft.onBlur}
                  className="tnum bg-transparent text-[13px] text-ink focus:outline-none"
                />
              </div>
              <div className={cn(MATERIAL_BOX_CLASSES, "h-[50px] flex-col items-stretch justify-center gap-0 py-1")}>
                <span className="text-[10.5px] text-[#6B7280] dark:text-subtle">补零位数</span>
                <input
                  type="text"
                  inputMode="numeric"
                  value={numWidthDraft.value}
                  disabled={busy || !isArabicFormat}
                  placeholder="不补零"
                  aria-label="补零位数"
                  onChange={(e) => numWidthDraft.onChange(e.target.value)}
                  onCompositionStart={numWidthDraft.onCompositionStart}
                  onCompositionEnd={(e) => numWidthDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={numWidthDraft.onBlur}
                  className="tnum bg-transparent text-[13px] text-ink placeholder:text-[#6B7280] focus:outline-none disabled:opacity-45 dark:placeholder:text-subtle"
                />
              </div>
              <div className={cn(MATERIAL_BOX_CLASSES, "h-[50px] flex-col items-stretch justify-center gap-0 py-1")}>
                <span className="text-[10.5px] text-[#6B7280] dark:text-subtle">序号后缀</span>
                <input
                  type="text"
                  value={numSuffixDraft.value}
                  disabled={busy}
                  placeholder="可选"
                  aria-label="序号后缀"
                  onChange={(e) => numSuffixDraft.onChange(e.target.value)}
                  onCompositionStart={numSuffixDraft.onCompositionStart}
                  onCompositionEnd={(e) => numSuffixDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={numSuffixDraft.onBlur}
                  className="bg-transparent text-[13px] text-ink placeholder:text-[#6B7280] focus:outline-none dark:placeholder:text-subtle"
                />
              </div>
            </div>
          ) : null}

          {insertTab === "文件属性" ? (
            <div className="flex flex-wrap items-center gap-3">
              {activePosition === "指定位置" ? (
                <div className={cn(MATERIAL_BOX_CLASSES, "h-[54px] shrink-0 px-3")}>
                  <span className="text-xs text-[#6B7280] dark:text-muted">第</span>
                  <input
                    data-testid="rename-insert-index"
                    type="text"
                    inputMode="numeric"
                    value={insertIndexDraft.value}
                    disabled={busy}
                    aria-label="插入字符位置"
                    onChange={(e) => insertIndexDraft.onChange(e.target.value)}
                    onCompositionStart={insertIndexDraft.onCompositionStart}
                    onCompositionEnd={(e) =>
                      insertIndexDraft.onCompositionEnd(e.currentTarget.value)
                    }
                    onBlur={insertIndexDraft.onBlur}
                    className="tnum h-6 w-11 rounded-[4px] border border-strong bg-field px-1 text-center text-xs text-ink focus:border-accent focus:outline-none"
                  />
                  <span className="text-xs text-[#6B7280] dark:text-muted">个字符后</span>
                </div>
              ) : null}

              <DropdownMenu
                align="start"
                trigger={
                  <button
                    type="button"
                    data-testid="rename-insert-attr-select"
                    disabled={busy}
                    aria-label="选择文件属性"
                    className={cn(
                      MATERIAL_BOX_CLASSES,
                      "h-[54px] w-[200px] shrink-0 cursor-pointer justify-between py-1 text-left",
                    )}
                  >
                    <div className="flex min-w-0 flex-col">
                      <span className="text-[10.5px] text-[#6B7280] dark:text-subtle">文件信息</span>
                      <span className="truncate text-[13px] text-[#111827] dark:text-ink">
                        {INSERT_ATTR_OPTIONS.find((o) => o.value === activeAttrKind)?.label ?? activeAttrKind}
                      </span>
                    </div>
                    <svg width="9" height="5" viewBox="0 0 10 6" fill="currentColor" aria-hidden="true" className="shrink-0 text-[#595959] dark:text-subtle">
                      <path d="M0 0h10L5 6z" />
                    </svg>
                  </button>
                }
                items={INSERT_ATTR_OPTIONS.map((opt) => ({
                  id: `attr-${opt.value}`,
                  label: opt.label,
                  icon: activeAttrKind === opt.value ? "check" : undefined,
                  onSelect: () => {
                    setLastAttrKind(opt.value);
                    runIpc(() => Rename.renameSetField("insertKind", opt.value));
                  },
                }))}
              />

              <div className={cn(MATERIAL_BOX_CLASSES, "h-[54px] min-w-[120px] flex-1")}>
                <input
                  data-testid="rename-insert-prefix"
                  type="text"
                  value={insertPrefixDraft.value}
                  disabled={busy}
                  placeholder="连接前缀（如 _）"
                  aria-label="属性前缀"
                  onChange={(e) => insertPrefixDraft.onChange(e.target.value)}
                  onCompositionStart={insertPrefixDraft.onCompositionStart}
                  onCompositionEnd={(e) => insertPrefixDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={insertPrefixDraft.onBlur}
                  className="min-w-0 flex-1 bg-transparent text-[13px] text-ink placeholder:text-[#6B7280] focus:outline-none dark:placeholder:text-subtle"
                />
              </div>
              <div className={cn(MATERIAL_BOX_CLASSES, "h-[54px] min-w-[120px] flex-1")}>
                <input
                  data-testid="rename-insert-suffix"
                  type="text"
                  value={insertSuffixDraft.value}
                  disabled={busy}
                  placeholder="连接后缀（可选）"
                  aria-label="属性后缀"
                  onChange={(e) => insertSuffixDraft.onChange(e.target.value)}
                  onCompositionStart={insertSuffixDraft.onCompositionStart}
                  onCompositionEnd={(e) => insertSuffixDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={insertSuffixDraft.onBlur}
                  className="min-w-0 flex-1 bg-transparent text-[13px] text-ink placeholder:text-[#6B7280] focus:outline-none dark:placeholder:text-subtle"
                />
              </div>
            </div>
          ) : null}
        </div>
      ) : null}

      {/* Mode 3: 自动编号 — exact 3-column layout with center stacked card */}
      {st.mode === 3 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2">
          <div className="flex w-full items-center gap-5">
            {/* Left: 序号前面字符 */}
            <div className={cn(MATERIAL_BOX_CLASSES, "h-[54px] flex-1")}>
              <input
                data-testid="rename-num-prefix"
                type="text"
                value={numPrefixDraft.value}
                disabled={busy}
                placeholder="前缀（序号前）"
                aria-label="序号前缀"
                onChange={(e) => numPrefixDraft.onChange(e.target.value)}
                onCompositionStart={numPrefixDraft.onCompositionStart}
                onCompositionEnd={(e) => numPrefixDraft.onCompositionEnd(e.currentTarget.value)}
                onBlur={numPrefixDraft.onBlur}
                className="min-w-0 flex-1 bg-transparent text-[13.5px] text-[#1F2937] placeholder:text-[#6B7280] focus:outline-none dark:text-ink dark:placeholder:text-subtle"
              />
              {numPrefixDraft.value ? (
                <ClearCircleButton
                  ariaLabel="清空前缀"
                  disabled={busy}
                  onClick={() => numPrefixDraft.setValue("", true)}
                />
              ) : null}
            </div>

            {/* Center stacked card: 开始序号 / 序号类型 / 固定位数 */}
            <div className="flex w-[205px] shrink-0 flex-col gap-2 rounded-[10px] border border-[#D4D4D8] bg-card p-2.5 dark:border-strong">
              {/* 开始序号 */}
              <div className={cn(MATERIAL_BOX_CLASSES, "h-[46px] justify-between px-3 py-1")}>
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="text-[10.5px] leading-tight text-[#6B7280] dark:text-subtle">
                    开始序号
                  </span>
                  <input
                    data-testid="rename-num-start"
                    type="text"
                    inputMode="numeric"
                    value={numStartDraft.value}
                    disabled={busy}
                    aria-label="起始序号"
                    onChange={(e) => numStartDraft.onChange(e.target.value)}
                    onCompositionStart={numStartDraft.onCompositionStart}
                    onCompositionEnd={(e) => numStartDraft.onCompositionEnd(e.currentTarget.value)}
                    onBlur={numStartDraft.onBlur}
                    className="tnum min-w-0 bg-transparent text-[13.5px] leading-tight text-[#111827] focus:outline-none dark:text-ink"
                  />
                </div>
                <div className="flex shrink-0 flex-col items-center justify-center gap-0.5 text-[#595959] dark:text-subtle">
                  <button
                    type="button"
                    tabIndex={-1}
                    disabled={busy}
                    aria-label="增加起始序号"
                    onClick={() => stepNumStart(1)}
                    className="flex h-3 w-4 cursor-pointer items-center justify-center hover:text-ink disabled:opacity-40"
                  >
                    <svg width="8" height="5" viewBox="0 0 8 5" fill="currentColor" aria-hidden="true">
                      <path d="M4 0L8 5H0z" />
                    </svg>
                  </button>
                  <button
                    type="button"
                    tabIndex={-1}
                    disabled={busy}
                    aria-label="减少起始序号"
                    onClick={() => stepNumStart(-1)}
                    className="flex h-3 w-4 cursor-pointer items-center justify-center hover:text-ink disabled:opacity-40"
                  >
                    <svg width="8" height="5" viewBox="0 0 8 5" fill="currentColor" aria-hidden="true">
                      <path d="M0 0h8L4 5z" />
                    </svg>
                  </button>
                </div>
              </div>

              {/* 序号类型 dropdown */}
              <DropdownMenu
                align="center"
                trigger={
                  <button
                    type="button"
                    data-testid="rename-num-format-trigger"
                    disabled={busy}
                    aria-label="序号类型"
                    className={cn(
                      MATERIAL_BOX_CLASSES,
                      "h-[46px] w-full cursor-pointer justify-between px-3 py-1 text-left",
                    )}
                  >
                    <div className="flex min-w-0 flex-col">
                      <span className="text-[10.5px] leading-tight text-[#6B7280] dark:text-subtle">
                        序号类型
                      </span>
                      <span className="truncate text-[13.5px] leading-tight text-[#111827] dark:text-ink">
                        {activeNumFormat}
                      </span>
                    </div>
                    <svg
                      width="10"
                      height="6"
                      viewBox="0 0 10 6"
                      fill="currentColor"
                      aria-hidden="true"
                      className="shrink-0 text-[#595959] dark:text-subtle"
                    >
                      <path d="M0 0h10L5 6z" />
                    </svg>
                  </button>
                }
                items={NUMBER_FORMAT_OPTIONS.map((opt) => ({
                  id: `num-fmt-${opt.value}`,
                  label: opt.label,
                  "data-testid": `rename-num-format-${opt.value}`,
                  icon: activeNumFormat === opt.value ? "check" : undefined,
                  onSelect: () => handleSelectNumFormat(opt.value),
                }))}
              />

              {/* 固定位数 */}
              <div className={cn(MATERIAL_BOX_CLASSES, "h-[44px] flex-col items-stretch justify-center gap-0 px-3 py-1")}>
                {numWidthDraft.value ? (
                  <span className="text-[10.5px] leading-tight text-[#6B7280] dark:text-subtle">
                    {isArabicFormat ? "补零位数" : "补零位数（仅限阿拉伯数字）"}
                  </span>
                ) : null}
                <input
                  data-testid="rename-num-width"
                  type="text"
                  inputMode="numeric"
                  value={numWidthDraft.value}
                  disabled={busy || !isArabicFormat}
                  placeholder={isArabicFormat ? "补零位数" : "补零位数（仅限阿拉伯数字）"}
                  aria-label="补零位数"
                  onChange={(e) => numWidthDraft.onChange(e.target.value)}
                  onCompositionStart={numWidthDraft.onCompositionStart}
                  onCompositionEnd={(e) => numWidthDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={numWidthDraft.onBlur}
                  className="tnum min-w-0 bg-transparent text-[13.5px] leading-tight text-[#111827] placeholder:text-[#6B7280] focus:outline-none disabled:opacity-45 dark:text-ink dark:placeholder:text-subtle"
                />
              </div>
            </div>

            {/* Right: 序号后面字符 */}
            <div className={cn(MATERIAL_BOX_CLASSES, "h-[54px] flex-1")}>
              <input
                data-testid="rename-num-suffix"
                type="text"
                value={numSuffixDraft.value}
                disabled={busy}
                placeholder="后缀（序号后）"
                aria-label="序号后缀"
                onChange={(e) => numSuffixDraft.onChange(e.target.value)}
                onCompositionStart={numSuffixDraft.onCompositionStart}
                onCompositionEnd={(e) => numSuffixDraft.onCompositionEnd(e.currentTarget.value)}
                onBlur={numSuffixDraft.onBlur}
                className="min-w-0 flex-1 bg-transparent text-[13.5px] text-[#1F2937] placeholder:text-[#6B7280] focus:outline-none dark:text-ink dark:placeholder:text-subtle"
              />
              {numSuffixDraft.value ? (
                <ClearCircleButton
                  ariaLabel="清空后缀"
                  disabled={busy}
                  onClick={() => numSuffixDraft.setValue("", true)}
                />
              ) : null}
            </div>
          </div>

          {showNumSample ? (
            <span className="tnum text-[11px] text-subtle">
              示例：{numSample}
            </span>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
