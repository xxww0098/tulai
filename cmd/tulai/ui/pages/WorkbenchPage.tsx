import { Image, Scroll, Text, View } from "nsgo";
import { Button, Checkbox, Icon, Input, Progress, Select, Switch, Tabs } from "nsgo/ui";
import {
  addWorkbenchSources,
  applyWorkbenchPrompt,
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
import { PageToolbar, SectionCard } from "../components/primitives";
import { ResultGrid } from "../components/ResultGrid";
import { WorkbenchDialogs } from "../components/WorkbenchDialogs";
import { goTab, selectWorkbenchPlatform, TAB_FISSION, TAB_GEN, TAB_WASH, updateWorkbenchField, workbench } from "../store";

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

export function WorkbenchPage() {
  const wb = workbench;
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
      {/* 固定顶部模式/平台/执行操作条 */}
      <PageToolbar
        left={
          <View class="flex-row flex-wrap items-center gap-2">
            <Tabs
              testID="wb-mode-tabs"
              variant="segmented"
              size="small"
              options={MODE_OPTIONS}
              value={String(wb().page)}
              onChange={({ value }) => goTab(Number(value))}
            />
            <View class="h-4 w-px bg-slate-200 dark:bg-[#222B3C]" />
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
          <View class="flex-row items-center gap-2">
            <Button
              testID="wb-history-btn"
              text="历史记录"
              size="small"
              icon="list"
              variant="outline"
              onPress={() => setHistoryOpen(true)}
            />
            <If
              when={Boolean(wb().running)}
              else={
                <Button
                  testID="wb-start-btn"
                  text={wb().startLabel || "开始生成"}
                  size="small"
                  icon="play"
                  variant="primary"
                  disabled={!wb().canStart}
                  onPress={() => startTasks()}
                />
              }
            >
              <Button
                testID="wb-stop-btn"
                text="停止任务"
                size="small"
                icon="square"
                variant="secondary"
                onPress={() => stopTasks()}
              />
            </If>
          </View>
        }
        bottom={
          <If when={Boolean(wb().running || wb().progText || wb().startBlockedHint)}>
            <View class="gap-1 rounded-lg bg-slate-50 px-2.5 py-1.5 dark:bg-[#1A2232]">
              <View class="flex-row items-center justify-between gap-2">
                <Text class="flex-1 truncate text-xs text-slate-700 dark:text-slate-200">
                  {wb().progText || wb().startBlockedHint || wb().outputNote || ""}
                </Text>
                <If when={Boolean(wb().running || wb().totalCount > 0)}>
                  <Text class="text-xs font-semibold text-blue-600 dark:text-blue-400">
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

      {/* 双栏桌面工作区：左编排参数 / 右实时车道与结果网格 */}
      <View class="min-h-0 flex-1 gap-3 p-3 md:flex-row">
        {/* 左栏：提示词与参考图 */}
        <View testID="wb-composer" class="w-full min-h-0 md:w-[368px] xl:w-[404px]">
          <Scroll class="min-h-0 flex-1" contentClass="gap-2.5 pr-1">
            <SectionCard
              compact
              title={isWash() ? "洗图提示词与风格" : isFission() ? "裂变提示词" : "生图提示词"}
              right={
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
                      text="默认词"
                      size="small"
                      variant="ghost"
                      onPress={() => setWorkbenchField("resetWashPrompt", "")}
                    />
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
              }
            >
              <View class="gap-2.5">
                {/* 模式内选项 */}
                <If when={isFree()}>
                  <View class="flex-row flex-wrap items-center gap-2">
                    <Tabs
                      variant="segmented"
                      size="small"
                      options={BATCH_MODE_OPTIONS}
                      value={String(wb().batchMode)}
                      onChange={({ value }) => setWorkbenchField("batchMode", value)}
                    />
                    <If when={promptOptList().length > 0}>
                      <View class="min-w-[140px] flex-1">
                        <Select
                          options={promptOptList()}
                          value={wb().promptChoice || ""}
                          placeholder="套用模板..."
                          onChange={({ value }) => applyWorkbenchPrompt(value)}
                        />
                      </View>
                    </If>
                  </View>
                </If>

                <If when={isWash()}>
                  <View class="flex-row flex-wrap items-center justify-between gap-2">
                    <If when={washStyleList().length > 0}>
                      <View class="min-w-[160px] flex-1">
                        <Select
                          options={washStyleList()}
                          value={wb().washStyle || ""}
                          placeholder="选择洗图风格"
                          onChange={({ value }) => setWorkbenchField("washStyle", value)}
                        />
                      </View>
                    </If>
                    <View class="flex-row items-center gap-2">
                      <Switch
                        size="small"
                        checked={Boolean(wb().removeWatermark)}
                        onChange={({ checked }) =>
                          setWorkbenchField("removeWatermark", checked ? "true" : "false")
                        }
                      />
                      <Text class="text-xs text-slate-700 dark:text-slate-200">自动去水印</Text>
                    </View>
                  </View>
                </If>

                <If when={isFission()}>
                  <View class="flex-row items-center gap-2">
                    <Switch
                      size="small"
                      checked={Boolean(wb().skipImages)}
                      onChange={({ checked }) =>
                        setWorkbenchField("skipImages", checked ? "true" : "false")
                      }
                    />
                    <Text class="text-xs text-slate-700 dark:text-slate-200">
                      跳过参考图直接按提示词裂变
                    </Text>
                  </View>
                </If>

                {/* 多行提示词 */}
                <Input
                  multiline
                  value={wb().prompts || ""}
                  placeholder={
                    isFree()
                      ? "每行一条提示词（按文本批量），或输入统一描述搭配参考图..."
                      : isWash()
                        ? "输入洗图要求（如去文字水印、纯白底保留主体）..."
                        : "输入多角度/多场景裂变要求，每行一个方向..."
                  }
                  onChange={({ value }) => setWorkbenchField("prompts", value)}
                  class="min-h-[126px] rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-900 dark:border-[#334155] dark:bg-[#0D1118] dark:text-slate-100"
                />

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
                      class="flex-1"
                    />
                  </View>
                </If>

                {/* 输出尺寸规格行 */}
                <View class="flex-row items-center gap-2">
                  <If when={presetList().length > 0}>
                    <View class="flex-1">
                      <Select
                        options={presetList()}
                        value={wb().outputPreset || ""}
                        placeholder="输出规格"
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
                    <View class="w-[126px]">
                      <Select
                        options={ratioList()}
                        value={wb().outputRatio || ""}
                        placeholder="画面比例"
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
              </View>
            </SectionCard>

            {/* 参考图素材卡 */}
            <SectionCard
              compact
              title={`参考素材 (${sourceCount()})`}
              subtitle={sourceCount() === 0 && isFission() ? "单图裂变：支持拖入 1 张主图" : undefined}
              right={
                <View class="flex-row items-center gap-1">
                  <Button
                    text="选图"
                    size="small"
                    icon="plus"
                    variant="outline"
                    onPress={() => void pickWorkbenchFiles()}
                  />
                  <If when={!isFission()}>
                    <Button
                      text="目录"
                      size="small"
                      variant="outline"
                      onPress={() => void pickWorkbenchFolder()}
                    />
                  </If>
                  <If when={sourceCount() > 0}>
                    <Button
                      text="清空"
                      size="small"
                      variant="ghost"
                      onPress={() => clearWorkbenchSources()}
                    />
                  </If>
                </View>
              }
            >
              <If
                when={sourceCount() > 0}
                else={
                  <View class="items-center justify-center gap-1.5 rounded-lg border border-dashed border-slate-200 bg-slate-50/70 py-6 dark:border-[#222B3C] dark:bg-[#0D1118]/50">
                    <Icon name="image" size={20} color="#94A3B8" />
                    <Text class="text-xs text-slate-500 dark:text-slate-400">
                      点击上方“选图”或直接拖拽文件
                    </Text>
                  </View>
                }
              >
                <View class="gap-2">
                  <View class="flex-row flex-wrap gap-2">
                    <Each of={(wb().sources ?? []).slice(0, 6)} key={(s) => s.path || String(s.index)}>
                      {(item) => (
                        <View
                          onPress={() => selectWorkbenchSource(item().index)}
                          class="w-[100px] gap-1 rounded-lg border p-1"
                          classList={{
                            "border-blue-600 bg-blue-50/50 dark:border-blue-400 dark:bg-blue-500/10": Boolean(
                              item().selected,
                            ),
                            "border-slate-200 dark:border-[#222B3C]": !item().selected,
                          }}
                        >
                          <View class="h-[64px] w-full items-center justify-center overflow-hidden rounded bg-slate-100 dark:bg-[#0D1118]">
                            <If when={Boolean(item().fileURL)}>
                              <Image
                                src={item().fileURL}
                                class="h-full w-full object-cover"
                                testID={`wb-source-thumb-${item().index}`}
                              />
                            </If>
                          </View>
                          <View class="flex-row items-center justify-between gap-1">
                            <Text class="flex-1 truncate text-[10px] text-slate-700 dark:text-slate-200">
                              {item().name || `#${item().index + 1}`}
                            </Text>
                            <View
                              onPress={() => removeWorkbenchSource(item().index)}
                              class="rounded px-1 hover:bg-rose-100 dark:hover:bg-rose-500/20"
                            >
                              <Text class="text-[10px] text-rose-500">✕</Text>
                            </View>
                          </View>
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
                    />
                  </If>
                </View>
              </If>
            </SectionCard>
          </Scroll>
        </View>

        {/* 右栏：账号车道与结果舞台 */}
        <View testID="wb-stage" class="min-w-0 min-h-0 flex-1">
          <Scroll class="min-h-0 flex-1" contentClass="gap-3 pr-1">
            <AccountLanes />
            <ResultGrid />
          </Scroll>
        </View>
      </View>

      <WorkbenchDialogs />
    </View>
  );
}
