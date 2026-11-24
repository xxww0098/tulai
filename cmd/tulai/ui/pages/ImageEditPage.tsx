import { Image, List, Scroll, signal, Text, View } from "nsgo";
import { Badge, Button, Checkbox, Dialog, IconButton, Input, Progress, Select, Slider, Switch } from "nsgo/ui";
import {
  editAddFromSources,
  editAddPaths,
  editApplyDims,
  editApplyRecipe,
  editCancelBatch,
  editClearAll,
  editClearFiles,
  editDeleteRecipe,
  editOpenOutDir,
  editPickFiles,
  editPickFolder,
  editPickOutDir,
  editPickRatio,
  editRemoveIndex,
  editResetFeature,
  editResetView,
  editRetryFailed,
  editRunBatch,
  editSaveMySize,
  editSaveRecipe,
  editSelectIndex,
  editSetFeature,
  editSetField,
  editToggleExpanded,
  editZoomStep,
} from "tulai/backend";
import { ImageEditStage } from "tulai/native";
import { EmptyNotice, PageToolbar, SectionCard } from "../components/primitives";
import { imageEdit } from "../store";

const FORMAT_OPTIONS = [
  { value: "original", label: "保持原格式" },
  { value: "jpg", label: "JPEG (.jpg)" },
  { value: "png", label: "PNG (.png)" },
  { value: "webp", label: "WebP (.webp)" },
];

export function ImageEditPage() {
  const st = imageEdit;
  const recipeName = signal("");

  return (
    <View
      class="min-h-0 flex-1"
      onFileDrop={(e) => {
        if (e.uris.length > 0) editAddPaths(e.uris);
      }}
    >
      {/* 固定顶部操作栏 */}
      <PageToolbar
        left={
          <View class="flex-row flex-wrap items-center gap-1.5">
            <Button text="选图" size="small" icon="plus" variant="outline" onPress={() => editPickFiles()} />
            <Button text="目录" size="small" icon="folder" variant="outline" onPress={() => editPickFolder()} />
            <Button text="从生图导入" size="small" variant="outline" onPress={() => editAddFromSources()} />
            <Button text="配方" size="small" icon="save" variant="outline" onPress={() => editSetField("recipeOpen", "true")} />
            <If when={(st().files ?? []).length > 0}>
              <Button text="清空" size="small" variant="ghost" onPress={() => editClearFiles()} />
            </If>
          </View>
        }
        right={
          <View class="flex-row flex-wrap items-center gap-2">
            <If when={st().failCount > 0}>
              <Button text={`重试失败 (${st().failCount})`} size="small" variant="outline" onPress={() => editRetryFailed()} />
            </If>
            <If
              when={Boolean(st().running)}
              else={
                <Button
                  text={`批量导出 (${(st().files ?? []).length})`}
                  size="small"
                  icon="play"
                  variant="primary"
                  disabled={(st().files ?? []).length === 0}
                  onPress={() => editRunBatch()}
                />
              }
            >
              <Button text="停止导出" size="small" icon="square" variant="secondary" onPress={() => editCancelBatch()} />
            </If>
          </View>
        }
        bottom={
          <If when={Boolean(st().running || st().notice || (st().chips ?? []).length > 0)}>
            <View class="gap-1.5">
              <View class="flex-row flex-wrap items-center justify-between gap-2">
                <View class="flex-row flex-wrap items-center gap-1.5">
                  <Each of={st().chips ?? []} key={(c) => c.id || c.label || ""}>
                    {(chip) => <Badge text={chip().label || ""} variant="accent" />}
                  </Each>
                  <If when={(st().chips ?? []).length > 0}>
                    <Button text="重置参数" size="small" variant="ghost" onPress={() => editClearAll()} />
                  </If>
                  <If when={Boolean(st().notice)}>
                    <Text class="text-xs text-slate-600 dark:text-slate-300">{st().notice}</Text>
                  </If>
                </View>
                <If when={Boolean(st().running)}>
                  <View class="w-[240px] gap-1">
                    <Text class="text-[11px] text-slate-500">
                      {`处理中 ${st().progressDone}/${st().progressTotal} ${st().progressCurrent || ""}`}
                    </Text>
                    <Progress value={st().progressTotal > 0 ? st().progressDone / st().progressTotal : 0} />
                  </View>
                </If>
              </View>
            </View>
          </If>
        }
      />

      {/* 三栏精修工作室：左素材条 / 中央大画布 / 右折叠检视栏 */}
      <View class="min-h-0 flex-1 flex-row gap-2 p-2">
        {/* 左栏：素材胶片列表 */}
        <View
          testID="edit-files"
          class="h-full min-h-0 shrink-0"
          style={{ width: "16%", minWidth: 164, maxWidth: 220 }}
        >
          <SectionCard class="flex-1 min-h-0" compact title={`图片 (${(st().files ?? []).length})`}>
            <List
              class="min-h-0 flex-1"
              contentClass="gap-1.5 pr-1"
              each={st().files ?? []}
              keyExtractor={(f) => f.path || String(f.index)}
              estimatedItemSize={50}
              empty={<EmptyNotice icon="image" title="无图片" subtitle="添加或拖入图片" />}
            >
              {(file) => (
                <View
                  onPress={() => editSelectIndex(file().index)}
                  class="flex-row items-center gap-2 rounded-lg border p-1.5"
                  classList={{
                    "border-blue-600 bg-blue-50/70 dark:border-blue-400 dark:bg-blue-500/10":
                      file().index === st().selected,
                    "border-slate-200 dark:border-[#222B3C] dark:bg-[#1A2232]":
                      file().index !== st().selected,
                  }}
                >
                  <View class="size-9 shrink-0 overflow-hidden rounded bg-slate-100 dark:bg-[#0D1118]">
                    <If when={Boolean(file().fileURL)}>
                      <Image src={file().fileURL} class="size-9 object-cover" />
                    </If>
                  </View>
                  <View class="min-w-0 flex-1">
                    <Text class="truncate text-[11px] font-medium text-slate-900 dark:text-slate-100">
                      {file().name}
                    </Text>
                    <If when={Boolean(file().status)}>
                      <Text class="truncate text-[10px] text-slate-500">{file().status}</Text>
                    </If>
                  </View>
                  <IconButton
                    icon="x"
                    variant="ghost"
                    size={22}
                    accessibilityLabel="移除"
                    onPress={() => editRemoveIndex(file().index)}
                  />
                </View>
              )}
            </List>
          </SectionCard>
        </View>

        {/* 中央画布区 */}
        <View
          testID="edit-canvas"
          class="min-w-0 min-h-0 flex-1 gap-2 rounded-xl border border-slate-200 bg-white p-2.5 dark:border-[#222B3C] dark:bg-[#131924]"
        >
          <View class="flex-row flex-wrap items-center justify-between gap-2 px-1">
            <View class="flex-row items-center gap-1.5">
              <IconButton icon="zoom-out" size={26} variant="raised" accessibilityLabel="缩小" onPress={() => editZoomStep(0.8)} />
              <Text class="w-[42px] text-center text-xs text-slate-600 dark:text-slate-300">
                {`${Math.round(st().zoom * 100)}%`}
              </Text>
              <IconButton icon="zoom-in" size={26} variant="raised" accessibilityLabel="放大" onPress={() => editZoomStep(1.25)} />
              <Button text="适应" size="small" variant="ghost" onPress={() => editResetView()} />
              <If when={Boolean(st().hasSelection)}>
                <Text class="text-[11px] text-slate-500 dark:text-slate-400">
                  {`${st().srcW}×${st().srcH} → ${st().outW}×${st().outH}`}
                </Text>
              </If>
            </View>

            <View class="flex-row items-center gap-2">
              <View class="flex-row items-center gap-1.5">
                <Switch
                  size="small"
                  checked={Boolean(st().compare)}
                  onChange={({ checked }) => editSetField("compare", checked ? "true" : "false")}
                />
                <Text class="text-xs text-slate-700 dark:text-slate-200">原图对比</Text>
              </View>
              <If when={Boolean(st().compare)}>
                <View class="w-[96px]">
                  <Slider
                    min={0.05}
                    max={0.95}
                    step={0.01}
                    value={st().split}
                    onChange={({ value }) => editSetField("split", String(value))}
                  />
                </View>
              </If>
            </View>
          </View>

          <View class="min-h-0 flex-1 overflow-hidden rounded-lg bg-slate-950">
            <ImageEditStage class="h-full w-full" />
          </View>
        </View>

        {/* 右栏：可折叠参数检视面板 */}
        <View
          testID="edit-inspector"
          class="h-full min-h-0 shrink-0"
          style={{ width: "24%", minWidth: 256, maxWidth: 340 }}
        >
          <Scroll class="min-h-0 flex-1" contentClass="gap-2.5 pr-1">
            {/* 裁剪尺寸与旋转 */}
            <SectionCard
              compact
              title="裁剪、尺寸与旋转"
              subtitle={st().summaryCrop}
              right={
                <View class="flex-row items-center gap-1">
                  <IconButton
                    icon={st().cropOpen ? "chevron-up" : "chevron-down"}
                    size={24}
                    variant="ghost"
                    accessibilityLabel="展开或收起裁剪面板"
                    onPress={() => editToggleExpanded("crop")}
                  />
                  <Switch
                    size="small"
                    checked={Boolean(st().cropEnabled)}
                    onChange={({ checked }) => editSetFeature("crop", checked)}
                  />
                </View>
              }
            >
              <If when={Boolean(st().cropOpen)}>
                <View class="gap-2.5">
                  <View class="flex-row flex-wrap items-center gap-1.5">
                    <IconButton icon="rotate-ccw" size={26} variant="raised" accessibilityLabel="逆时针旋转" onPress={() => editSetField("rotateLeft", "1")} />
                    <IconButton icon="rotate-cw" size={26} variant="raised" accessibilityLabel="顺时针旋转" onPress={() => editSetField("rotateRight", "1")} />
                    <IconButton icon="flip-horizontal" size={26} variant="raised" accessibilityLabel="水平翻转" onPress={() => editSetField("flipH", "1")} />
                    <IconButton icon="flip-vertical" size={26} variant="raised" accessibilityLabel="垂直翻转" onPress={() => editSetField("flipV", "1")} />
                    <Button text="重置" size="small" variant="ghost" onPress={() => editResetFeature("crop")} />
                  </View>

                  <View class="flex-row flex-wrap gap-1">
                    <Each of={st().ratioPresets ?? []} key={(r) => r.ratio || ""}>
                      {(preset) => (
                        <Button
                          text={preset().label || preset().ratio || ""}
                          size="small"
                          variant={st().cropRatio === preset().ratio ? "primary" : "outline"}
                          onPress={() => editPickRatio(preset().ratio || "")}
                        />
                      )}
                    </Each>
                  </View>

                  <View class="flex-row items-center gap-1.5">
                    <Input
                      value={st().dimW || ""}
                      placeholder="宽"
                      onChange={({ value }) => editSetField("dimW", value)}
                      class="flex-1"
                    />
                    <Text class="text-xs text-slate-400">×</Text>
                    <Input
                      value={st().dimH || ""}
                      placeholder="高"
                      onChange={({ value }) => editSetField("dimH", value)}
                      class="flex-1"
                    />
                    <Button
                      text="应用"
                      size="small"
                      variant="outline"
                      onPress={() =>
                        editApplyDims(
                          st().mode || "resize",
                          Number(st().dimW) || 0,
                          Number(st().dimH) || 0,
                          st().cropRatio || "",
                        )
                      }
                    />
                    <IconButton
                      icon="save"
                      size={26}
                      variant="ghost"
                      accessibilityLabel="保存为我的常用尺寸"
                      onPress={() => editSaveMySize()}
                    />
                  </View>
                </View>
              </If>
            </SectionCard>

            {/* 色彩调节 */}
            <SectionCard
              compact
              title="色彩与清晰度"
              subtitle={st().summaryAdjust}
              right={
                <View class="flex-row items-center gap-1">
                  <IconButton
                    icon={st().adjustOpen ? "chevron-up" : "chevron-down"}
                    size={24}
                    variant="ghost"
                    accessibilityLabel="展开或收起色彩面板"
                    onPress={() => editToggleExpanded("adjust")}
                  />
                  <Switch
                    size="small"
                    checked={Boolean(st().adjustEnabled)}
                    onChange={({ checked }) => editSetFeature("adjust", checked)}
                  />
                </View>
              }
            >
              <If when={Boolean(st().adjustOpen)}>
                <View class="gap-2">
                  <View class="gap-1">
                    <Text class="text-[11px] text-slate-600 dark:text-slate-300">{`亮度 (${st().brightness})`}</Text>
                    <Slider
                      min={-100}
                      max={100}
                      step={1}
                      value={st().brightness}
                      onChange={({ value }) => editSetField("brightness", String(Math.round(value)))}
                    />
                  </View>
                  <View class="gap-1">
                    <Text class="text-[11px] text-slate-600 dark:text-slate-300">{`对比度 (${st().contrast})`}</Text>
                    <Slider
                      min={-100}
                      max={100}
                      step={1}
                      value={st().contrast}
                      onChange={({ value }) => editSetField("contrast", String(Math.round(value)))}
                    />
                  </View>
                  <View class="gap-1">
                    <Text class="text-[11px] text-slate-600 dark:text-slate-300">{`饱和度 (${st().saturation})`}</Text>
                    <Slider
                      min={-100}
                      max={100}
                      step={1}
                      value={st().saturation}
                      onChange={({ value }) => editSetField("saturation", String(Math.round(value)))}
                    />
                  </View>
                  <View class="flex-row items-center justify-between pt-1">
                    <Checkbox
                      text="轻度锐化"
                      checked={Boolean(st().sharpen)}
                      onChange={({ checked }) => editSetField("sharpen", checked ? "true" : "false")}
                    />
                    <Checkbox
                      text="黑白灰度"
                      checked={Boolean(st().grayscale)}
                      onChange={({ checked }) => editSetField("grayscale", checked ? "true" : "false")}
                    />
                    <Button text="重置" size="small" variant="ghost" onPress={() => editResetFeature("adjust")} />
                  </View>
                </View>
              </If>
            </SectionCard>

            {/* 格式与导出目录 */}
            <SectionCard
              compact
              title="输出格式与目录"
              subtitle={st().summaryFormat}
              right={
                <View class="flex-row items-center gap-1">
                  <IconButton
                    icon={st().formatOpen ? "chevron-up" : "chevron-down"}
                    size={24}
                    variant="ghost"
                    accessibilityLabel="展开或收起格式面板"
                    onPress={() => editToggleExpanded("format")}
                  />
                  <Switch
                    size="small"
                    checked={Boolean(st().formatEnabled)}
                    onChange={({ checked }) => editSetFeature("format", checked)}
                  />
                </View>
              }
            >
              <If when={Boolean(st().formatOpen)}>
                <View class="gap-2">
                  <View class="flex-row items-center gap-2">
                    <View class="flex-1">
                      <Select
                        options={FORMAT_OPTIONS}
                        value={st().format || "original"}
                        style={{ width: "100%", alignSelf: "stretch" }}
                        onChange={({ value }) => editSetField("format", value)}
                      />
                    </View>
                    <Input
                      value={st().quality || "90"}
                      placeholder="品质"
                      onChange={({ value }) => editSetField("quality", value)}
                      class="w-[62px]"
                    />
                  </View>
                  <Input
                    value={st().suffix || ""}
                    placeholder="输出文件名后缀 (如 _edited)"
                    onChange={({ value }) => editSetField("suffix", value)}
                  />
                  <View class="flex-row items-center gap-1.5">
                    <Text class="flex-1 truncate text-[11px] text-slate-500">
                      {st().outDir || "原图同目录 / 默认输出"}
                    </Text>
                    <Button text="选目录" size="small" variant="outline" onPress={() => editPickOutDir()} />
                    <Button text="打开" size="small" variant="ghost" onPress={() => editOpenOutDir()} />
                  </View>
                </View>
              </If>
            </SectionCard>
          </Scroll>
        </View>
      </View>

      {/* 配方管理器弹窗 */}
      <Dialog
        open={Boolean(st().recipeOpen)}
        title="保存与套用精修配方"
        style={{ width: 500 }}
        onClose={() => editSetField("recipeOpen", "false")}
        closeOnBackdropPress
        closeOnEscape
      >
        <View class="w-full min-w-0 gap-3">
          <View class="flex-row items-center gap-2">
            <Input
              value={recipeName()}
              placeholder="输入新配方名称..."
              onChange={({ value }) => recipeName.set(value)}
              class="flex-1"
            />
            <Button
              text="保存当前"
              size="small"
              variant="primary"
              disabled={recipeName().trim() === ""}
              onPress={() => {
                editSaveRecipe(recipeName());
                recipeName.set("");
              }}
            />
          </View>
          <If when={Boolean(st().recipeErr)}>
            <Text class="text-xs text-rose-600">{st().recipeErr}</Text>
          </If>
          <View class="gap-1.5">
            <Each of={st().recipes ?? []} key={(r) => String(r.index)}>
              {(recipe) => (
                <View class="flex-row items-center justify-between rounded-lg border border-slate-200 px-3 py-2 dark:border-[#222B3C] dark:bg-[#1A2232]">
                  <Text class="text-xs font-medium text-slate-900 dark:text-slate-100">{recipe().name}</Text>
                  <View class="flex-row items-center gap-1">
                    <Button text="套用" size="small" variant="outline" onPress={() => editApplyRecipe(recipe().index)} />
                    <Button text="删除" size="small" variant="ghost" onPress={() => editDeleteRecipe(recipe().index)} />
                  </View>
                </View>
              )}
            </Each>
          </View>
        </View>
      </Dialog>

      {/* 批量导出结果摘要弹窗 */}
      <Dialog
        open={Boolean(st().resultOpen)}
        title="批量导出完成"
        description={`成功 ${st().okCount} 张 · 失败 ${st().failCount} 张`}
        style={{ width: 460 }}
        onClose={() => editSetField("resultOpen", "false")}
        closeOnBackdropPress
        closeOnEscape
      >
        <View class="w-full min-w-0 flex-row justify-end gap-2">
          <If when={st().failCount > 0}>
            <Button text="重试失败" size="small" variant="outline" onPress={() => editRetryFailed()} />
          </If>
          <Button text="打开输出目录" size="small" variant="outline" onPress={() => editOpenOutDir()} />
          <Button text="完成" size="small" variant="primary" onPress={() => editSetField("resultOpen", "false")} />
        </View>
      </Dialog>
    </View>
  );
}
