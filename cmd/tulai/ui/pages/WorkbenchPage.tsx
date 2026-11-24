import { Image, Scroll, signal, Text, View } from "nsgo";
import { Button, Checkbox, Icon, IconButton, Input, Progress, Select, Switch, Tabs } from "nsgo/ui";
import {
  addWorkbenchSources,
  applyWorkbenchPrompt,
  clearWashedWorkbenchSources,
  clearWorkbenchSources,
  pickWorkbenchFiles,
  pickWorkbenchFolder,
  removeWorkbenchSource,
  selectWorkbenchSource,
  setGalleryOpen,
  setHistoryOpen,
  setOutputSize,
  setPromptSuffix,
  setWorkbenchField,
  startTasks,
  stopTasks,
} from "tulai/backend";
import { AccountLanes } from "../components/AccountLanes";
import { ActiveScanOverlay, LivePulseDot } from "../components/MotionPrimitives";
import { PageToolbar } from "../components/primitives";
import { ResultGrid } from "../components/ResultGrid";
import { WorkbenchDialogs } from "../components/WorkbenchDialogs";
import { goTab, selectWorkbenchPlatform, TAB_FISSION, TAB_GEN, TAB_WASH, workbench } from "../store";

const MODE_OPTIONS = [
  { value: String(TAB_WASH), label: "批量洗图" },
  { value: String(TAB_GEN), label: "自由生图" },
  { value: String(TAB_FISSION), label: "裂变图片" },
];

const PLATFORM_OPTIONS = [
  { value: "豆包", label: "豆包" },
  { value: "千问", label: "千问" },
  { value: "ChatGPT", label: "ChatGPT" },
  { value: "Antigravity", label: "Antigravity" },
];

const BATCH_MODE_OPTIONS = [
  { value: "0", label: "按文本批量" },
  { value: "1", label: "按参考图批量" },
];

const FULL_SELECT_STYLE = {
  width: "100%" as const,
  alignSelf: "stretch" as const,
};

export function WorkbenchPage() {
  const wb = workbench;
  const promptExpanded = signal(false);
  const isFree = () => wb().mode === "free";
  const isWash = () => wb().mode === "wash" || !wb().mode;
  const isFission = () => wb().mode === "fission";
  const sourceCount = () => wb().sourcesTotal || (wb().sources ?? []).length;
  const selectedPlatform = () =>
    wb().qianwen ? "千问" : wb().chatGPT ? "ChatGPT" : wb().agy ? "Antigravity" : "豆包";

  const promptOptList = () =>
    (wb().promptOptions ?? []).map((p) => ({ value: p, label: p }));
  const washStyleList = () =>
    (wb().washStyleOptions ?? []).map((s) => ({ value: s, label: s }));
  const presetList = () =>
    (wb().outputPresets ?? []).map((p) => ({ value: p, label: p }));
  const ratioList = () =>
    (wb().outputRatios ?? []).map((r) => ({ value: r, label: r }));

  return (
    <View
      class="min-h-0 flex-1"
      onFileDrop={(e) => {
        if (e.uris.length > 0) addWorkbenchSources(e.uris);
      }}
    >
      {/* 精简工作室顶栏：模式 + 平台 + 历史记录 */}
      <PageToolbar
        left={
          <View class="flex-row flex-wrap items-center gap-2.5">
            <Tabs
              testID="wb-mode-tabs"
              variant="segmented"
              size="small"
              options={MODE_OPTIONS}
              value={String(wb().page)}
              onChange={({ value }) => goTab(Number(value))}
            />
            <View class="h-4 w-px bg-slate-200/80 dark:bg-[#1E2738]" />
            <Tabs
              testID="wb-platform-tabs"
              variant="segmented"
              size="small"
              options={PLATFORM_OPTIONS}
              value={selectedPlatform()}
              onChange={({ value }) => selectWorkbenchPlatform(value)}
            />
          </View>
        }
        right={
          <Button
            testID="wb-history-btn"
            text="历史记录"
            size="small"
            icon="clock"
            variant="ghost"
            onPress={() => setHistoryOpen(true)}
          />
        }
        bottom={
          <If when={Boolean(wb().running || wb().progText)}>
            <View class="gap-1.5 pt-1">
              <View class="flex-row items-center justify-between gap-3">
                <View class="min-w-0 flex-1 flex-row items-center gap-2">
                  <If
                    when={Boolean(wb().running)}
                    else={<View class="size-1.5 shrink-0 rounded-full bg-emerald-500" />}
                  >
                    <LivePulseDot color="blue" size={7} />
                  </If>
                  <Text class="flex-1 truncate text-xs text-slate-600 dark:text-slate-300">
                    {wb().progText || wb().outputNote || ""}
                  </Text>
                </View>
                <If when={Boolean(wb().running || wb().totalCount > 0)}>
                  <Text
                    class="shrink-0 text-xs font-semibold text-blue-600 dark:text-blue-400"
                    style={{ fontVariant: ["tabular-nums"] }}
                  >
                    {`${Math.round(wb().progress * 100)}%`}
                  </Text>
                </If>
              </View>
              <If when={Boolean(wb().running)}>
                <Progress value={wb().progress} />
              </If>
            </View>
          </If>
        }
      />

      {/* 固定双栏 SaaS 创作工作台：左栏与右栏按窗口比例同步拉伸 */}
      <View class="min-h-0 flex-1 flex-row bg-[#F8FAFC] dark:bg-[#0B0F17]">
        {/* 左栏：固定三段式（顶部参数 / 中段独立滚动素材 / 底部固定主按钮） */}
        <View
          testID="wb-composer"
          class="h-full min-h-0 shrink-0 border-r border-slate-200/80 bg-white dark:border-[#1E2738] dark:bg-[#111722]"
          style={{ width: "27%", minWidth: 276, maxWidth: 356 }}
        >
          {/* 模块一：模式设置与提示词（固定顶部） */}
          <View class="shrink-0 gap-2 border-b border-slate-100 px-3 py-2.5 dark:border-[#1E2738]">
              <View class="flex-row items-center justify-between gap-2">
                <Text class="text-xs font-semibold text-slate-900 dark:text-slate-100">
                  {isWash() ? "洗图设置" : isFission() ? "裂变提示词" : "生图提示词"}
                </Text>
                <View class="flex-row items-center gap-1">
                  <If when={isFree()}>
                    <Button
                      text={wb().structuredCount > 0 ? `结构化 (${wb().structuredCount})` : "结构化"}
                      size="small"
                      icon="sliders-horizontal"
                      variant="outline"
                      onPress={() => setWorkbenchField("structuredOpen", "true")}
                    />
                  </If>
                  <If when={isWash()}>
                    <Button
                      testID="wb-prompt-toggle"
                      text={promptExpanded() ? "收起提示词" : "编辑提示词"}
                      size="small"
                      variant="ghost"
                      onPress={() => promptExpanded.set(!promptExpanded())}
                    />
                    <If when={promptExpanded()}>
                      <Button
                        text="默认词"
                        size="small"
                        variant="ghost"
                        onPress={() => setWorkbenchField("resetWashPrompt", "")}
                      />
                    </If>
                  </If>
                  <If when={isFission()}>
                    <Button
                      text="默认词"
                      size="small"
                      variant="ghost"
                      onPress={() => setWorkbenchField("resetFissionPrompt", "")}
                    />
                  </If>
                </View>
              </View>

              {/* 模式内选项 */}
              <If when={isFree()}>
                <View class="gap-2">
                  <Tabs
                    variant="segmented"
                    size="small"
                    options={BATCH_MODE_OPTIONS}
                    value={String(wb().batchMode)}
                    onChange={({ value }) => setWorkbenchField("batchMode", value)}
                  />
                  <If when={promptOptList().length > 0}>
                    <Select
                      options={promptOptList()}
                      value={wb().promptChoice || ""}
                      placeholder="套用模板..."
                      style={FULL_SELECT_STYLE}
                      onChange={({ value }) => applyWorkbenchPrompt(value)}
                    />
                  </If>
                </View>
              </If>

              <If when={isWash()}>
                <View class="gap-2.5">
                  <If when={washStyleList().length > 0}>
                    <Select
                      testID="wb-wash-style-select"
                      options={washStyleList()}
                      value={wb().washStyle || ""}
                      placeholder="选择洗图风格"
                      style={FULL_SELECT_STYLE}
                      onChange={({ value }) => setWorkbenchField("washStyle", value)}
                    />
                  </If>
                  <View class="flex-row items-center justify-between gap-2">
                    <Text class="text-xs text-slate-600 dark:text-slate-300">自动去水印</Text>
                    <Switch
                      size="small"
                      checked={Boolean(wb().removeWatermark)}
                      onChange={({ checked }) =>
                        setWorkbenchField("removeWatermark", checked ? "true" : "false")
                      }
                    />
                  </View>
                </View>
              </If>

              <If when={isFission()}>
                <View class="flex-row items-center justify-between gap-2">
                  <Text class="text-xs text-slate-600 dark:text-slate-300">
                    跳过参考图直接按提示词裂变
                  </Text>
                  <Switch
                    size="small"
                    checked={Boolean(wb().skipImages)}
                    onChange={({ checked }) =>
                      setWorkbenchField("skipImages", checked ? "true" : "false")
                    }
                  />
                </View>
              </If>

              {/* 提示词摘要 / 编辑器 */}
              <If
                when={!isWash() || promptExpanded()}
                else={
                  <View
                    testID="wb-prompt-summary"
                    onPress={() => promptExpanded.set(true)}
                    style={{ cursor: "pointer" }}
                    class="rounded-lg bg-slate-50 px-3 py-2 transition duration-150 hover:bg-slate-100/80 dark:bg-[#161E2E] dark:hover:bg-[#1C2639]"
                  >
                    <Text class="truncate text-xs text-slate-600 dark:text-slate-300">
                      {((wb().prompts || "").trim().split("\n")[0] || "使用默认洗图要求").slice(0, 80)}
                    </Text>
                  </View>
                }
              >
                <Input
                  testID="wb-prompt-editor"
                  multiline
                  disabled={Boolean(wb().running)}
                  value={wb().prompts || ""}
                  placeholder={
                    isFree()
                      ? "每行一条提示词（按文本批量），或输入统一描述搭配参考图..."
                      : isWash()
                        ? "提取主画，去掉载体与边框，输出纯 2D 画面；可补充其他要求..."
                        : "输入多角度/多场景裂变要求，每行一个方向..."
                  }
                  onChange={({ value }) => setWorkbenchField("prompts", value)}
                  class={isWash()
                    ? "h-[164px] rounded-lg border border-slate-200/90 bg-slate-50/70 px-3 py-2 text-xs text-slate-900 dark:border-[#263248] dark:bg-[#0B0F17] dark:text-slate-100"
                    : "min-h-[120px] rounded-lg border border-slate-200/90 bg-slate-50/70 px-3 py-2 text-xs text-slate-900 dark:border-[#263248] dark:bg-[#0B0F17] dark:text-slate-100"}
                />
              </If>

              {/* 自由生图全局后缀 */}
              <If when={isFree()}>
                <View class="flex-row items-center gap-2">
                  <Checkbox
                    text="追加后缀"
                    checked={Boolean(wb().useSuffix)}
                    onChange={({ checked }) => setPromptSuffix(wb().promptSuffix || "", checked)}
                  />
                  <Input
                    value={wb().promptSuffix || ""}
                    placeholder="全局附加修饰词..."
                    onChange={({ value }) => setPromptSuffix(value, Boolean(wb().useSuffix))}
                    class="min-w-0 flex-1"
                  />
                </View>
              </If>

              {/* 输出尺寸与画面比例（CSS Grid 自适应等分，不裁切） */}
              <If when={presetList().length > 0 || ratioList().length > 0}>
                <View
                  style={
                    presetList().length > 0 && ratioList().length > 0
                      ? { display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }
                      : undefined
                  }
                >
                  <If when={presetList().length > 0}>
                    <View testID="wb-output-preset-wrap" class="min-w-0 gap-1">
                      <Text class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
                        输出规格
                      </Text>
                      <Select
                        testID="wb-output-preset-select"
                        options={presetList()}
                        value={wb().outputPreset || ""}
                        placeholder="输出规格"
                        style={FULL_SELECT_STYLE}
                        onChange={({ value }) =>
                          setOutputSize(
                            value,
                            wb().outputMode || "",
                            wb().outputRatio || "",
                            wb().outputWidth || "",
                            wb().outputHeight || "",
                            Boolean(wb().outputCrop),
                          )
                        }
                      />
                    </View>
                  </If>
                  <If when={ratioList().length > 0}>
                    <View testID="wb-output-ratio-wrap" class="min-w-0 gap-1">
                      <Text class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
                        画面比例
                      </Text>
                      <Select
                        testID="wb-output-ratio-select"
                        options={ratioList()}
                        value={wb().outputRatio || ""}
                        placeholder="自动 / 原比例"
                        style={FULL_SELECT_STYLE}
                        onChange={({ value }) =>
                          setOutputSize(
                            wb().outputPreset || "",
                            "ratio",
                            value,
                            wb().outputWidth || "",
                            wb().outputHeight || "",
                            Boolean(wb().outputCrop),
                          )
                        }
                      />
                    </View>
                  </If>
                </View>
              </If>
            </View>

            {/* 模块二：参考素材（头部固定，素材网格独立滚动） */}
            <View class="min-h-0 flex-1 gap-2 px-3 py-2.5">
              <View class="shrink-0 flex-row items-center justify-between gap-2">
                <View class="min-w-0 flex-1 gap-0.5">
                  <Text
                    class="text-xs font-semibold text-slate-900 dark:text-slate-100"
                    style={{ fontVariant: ["tabular-nums"] }}
                  >
                    {`参考素材 (${sourceCount()})`}
                  </Text>
                  <If when={sourceCount() === 0 && isFission()}>
                    <Text class="text-[11px] text-slate-500 dark:text-slate-400">
                      单图裂变：支持拖入 1 张主图
                    </Text>
                  </If>
                </View>
                <View class="shrink-0 flex-row items-center gap-1">
                  <Button
                    text="选图"
                    size="small"
                    icon="plus"
                    variant="outline"
                    onPress={() => void pickWorkbenchFiles()}
                  />
                  <If when={!isFission()}>
                    <IconButton
                      icon="folder"
                      size={26}
                      variant="ghost"
                      tooltip="导入文件夹"
                      accessibilityLabel="导入文件夹"
                      onPress={() => void pickWorkbenchFolder()}
                    />
                  </If>
                  <If when={sourceCount() > 0}>
                    <IconButton
                      icon="trash"
                      size={26}
                      variant="ghost"
                      tooltip="清空全部素材"
                      accessibilityLabel="清空素材"
                      onPress={() => clearWorkbenchSources()}
                    />
                  </If>
                </View>
              </View>

              <If when={isWash() && wb().washedCount > 0 && !wb().running}>
                <View
                  testID="wb-washed-bar"
                  class="shrink-0 flex-row items-center justify-between gap-2 rounded-lg border border-emerald-200/80 bg-emerald-50/70 px-2 py-1 dark:border-emerald-500/25 dark:bg-emerald-500/10"
                >
                  <View class="min-w-0 flex-row items-center gap-1.5">
                    <Icon name="check" size={11} color="#059669" />
                    <Text
                      class="truncate text-[11px] font-medium text-emerald-700 dark:text-emerald-300"
                      style={{ fontVariant: ["tabular-nums"] }}
                    >
                      {`已洗 ${wb().washedCount} / ${sourceCount()}`}
                    </Text>
                  </View>
                  <Button
                    testID="wb-clear-washed-btn"
                    text="清空已洗"
                    size="small"
                    variant="ghost"
                    onPress={() => clearWashedWorkbenchSources()}
                  />
                </View>
              </If>

              <Scroll class="min-h-0 flex-1" contentClass="gap-2 pr-0.5">
                <If
                  when={sourceCount() > 0}
                  else={
                    <View
                      onPress={() => void pickWorkbenchFiles()}
                      style={{ cursor: "pointer" }}
                      class="min-h-[120px] items-center justify-center gap-1.5 rounded-xl border border-dashed border-slate-200/90 bg-slate-50/60 py-6 transition duration-150 hover:border-blue-400 dark:border-[#263248] dark:bg-[#0B0F17]/60"
                    >
                      <Icon name="image" size={20} color="#64748B" />
                      <Text class="text-xs text-slate-500 dark:text-slate-400">
                        点击选图或直接拖拽文件到此处
                      </Text>
                    </View>
                  }
                >
                  <View class="gap-2">
                    <View style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 6 }}>
                      <Each of={(wb().sources ?? []).slice(0, 12)} key={(s) => s.path || String(s.index)}>
                        {(item) => (
                          <View
                            onPress={() => selectWorkbenchSource(item().index)}
                            style={{ cursor: "pointer" }}
                            hoverStyle={{ transform: [{ translateY: -1 }] }}
                            class="relative overflow-hidden rounded-lg border transition duration-200"
                            classList={{
                              "border-blue-500 bg-blue-50/50 dark:border-blue-400 dark:bg-blue-500/15": Boolean(
                                item().washing,
                              ),
                              "border-blue-600 bg-blue-50/40 dark:border-blue-400 dark:bg-blue-500/10": Boolean(
                                !item().washing && item().selected,
                              ),
                              "border-emerald-400/75 bg-white dark:border-emerald-500/45 dark:bg-[#161E2E]": Boolean(
                                !item().washing && !item().selected && item().washed,
                              ),
                              "border-slate-200/80 bg-white hover:border-slate-300 dark:border-[#1E2738] dark:bg-[#161E2E] dark:hover:border-[#334155]":
                                !item().washing && !item().selected && !item().washed,
                            }}
                          >
                            <View
                              class="relative w-full items-center justify-center overflow-hidden bg-slate-100/70 dark:bg-[#0B0F17]"
                              style={{ aspectRatio: 1.25 }}
                            >
                              <If when={Boolean(item().fileURL)}>
                                <Image
                                  src={item().fileURL}
                                  fadeDuration={180}
                                  class="h-full w-full object-contain"
                                  classList={{
                                    "opacity-80": Boolean(
                                      !item().washing && !item().washed && (wb().running || wb().pendingCount > 0),
                                    ),
                                  }}
                                  testID={`wb-source-thumb-${item().index}`}
                                />
                              </If>
                              <If when={Boolean(item().washing)}>
                                <ActiveScanOverlay compact tone="blue" />
                              </If>
                            </View>
                            {/* 左上角实时加工状态标签 */}
                            <If
                              when={Boolean(item().washing)}
                              else={
                                <If
                                  when={Boolean(item().washed)}
                                  else={
                                    <If when={Boolean(wb().running || wb().pendingCount > 0)}>
                                      <View
                                        style={{ position: "absolute", top: 4, left: 4 }}
                                        class="rounded bg-slate-900/75 px-1.5 py-0.5"
                                      >
                                        <Text class="text-[9px] font-medium text-slate-100">等待</Text>
                                      </View>
                                    </If>
                                  }
                                >
                                  <View
                                    style={{ position: "absolute", top: 4, left: 4 }}
                                    class="flex-row items-center gap-0.5 rounded bg-emerald-600/90 px-1 py-0.5"
                                  >
                                    <Icon name="check" size={9} color="#FFFFFF" />
                                    <Text class="text-[9px] font-semibold text-white">已洗</Text>
                                  </View>
                                </If>
                              }
                            >
                              <View
                                style={{ position: "absolute", top: 4, left: 4 }}
                                class="flex-row items-center gap-1 rounded bg-blue-600/95 px-1.5 py-0.5"
                              >
                                <View class="size-1.5 rounded-full bg-white animate-pulse" />
                                <Text class="text-[9px] font-semibold text-white">洗图中</Text>
                              </View>
                            </If>
                            <If when={!wb().running}>
                              <View
                                role="button"
                                accessibilityLabel={`移除素材 ${item().name || item().index + 1}`}
                                onPress={() => removeWorkbenchSource(item().index)}
                                style={{ position: "absolute", top: 4, right: 4, cursor: "pointer" }}
                                class="size-5 items-center justify-center rounded-full bg-slate-900/65 hover:bg-rose-600"
                              >
                                <Icon name="x" size={11} color="#FFFFFF" />
                              </View>
                            </If>
                            <Text class="truncate px-1.5 py-1 text-[10px] text-slate-600 dark:text-slate-300">
                              {item().name || `#${item().index + 1}`}
                            </Text>
                          </View>
                        )}
                      </Each>
                    </View>
                    <If when={sourceCount() > 6}>
                      <Button
                        text={`管理全部 ${sourceCount()} 张素材`}
                        size="small"
                        variant="outline"
                        onPress={() => setGalleryOpen(true)}
                        class="w-full"
                      />
                    </If>
                  </View>
                </If>
              </Scroll>
            </View>

          {/* 底部固定主行动区 */}
          <View class="shrink-0 gap-1.5 border-t border-slate-200/80 bg-white px-3 py-2 dark:border-[#1E2738] dark:bg-[#111722]">
            <If when={!wb().running && Boolean(wb().startBlockedHint)}>
              <Text class="text-[11px] font-medium text-amber-600 dark:text-amber-400">
                {wb().startBlockedHint}
              </Text>
            </If>
            <If
              when={Boolean(wb().running)}
              else={
                <Button
                  testID="wb-start-btn"
                  text={wb().startLabel || "开始生成"}
                  size="default"
                  icon="play"
                  variant="primary"
                  disabled={!wb().canStart}
                  onPress={() => startTasks()}
                  class="w-full"
                />
              }
            >
              <Button
                testID="wb-stop-btn"
                text="停止任务"
                size="default"
                icon="square"
                variant="secondary"
                onPress={() => stopTasks()}
                class="w-full"
              />
            </If>
          </View>
        </View>

        {/* 右栏：顶部固定账号条 + 下方独立滚动出图画廊 */}
        <View testID="wb-stage" class="min-w-0 min-h-0 flex-1 bg-[#F8FAFC] dark:bg-[#0B0F17]">
          <AccountLanes />
          <ResultGrid />
        </View>
      </View>

      <WorkbenchDialogs />
    </View>
  );
}
