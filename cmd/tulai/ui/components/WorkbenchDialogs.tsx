import { computed, Image, Scroll, Text, View } from "nsgo";
import { Badge, Button, Checkbox, Dialog, IconButton, Input } from "nsgo/ui";
import {
  type ResultTileDTO,
  clearWashedWorkbenchSources,
  openPath,
  removeGallerySourceItem,
  replaceResultImage,
  retryTask,
  revealPath,
  sessionAction,
  setAllGallerySelect,
  setGalleryOpen,
  setHistoryOpen,
  setPreviewPath,
  setPromptPart,
  setWorkbenchField,
  toggleGallerySelect,
} from "tulai/backend";
import { promptParts, requeueWorkbenchDone, workbench } from "../store";

export function WorkbenchDialogs() {
  const wb = workbench;
  const parts = promptParts;

  const previewTiles = computed(() => {
    const out: ResultTileDTO[] = [];
    const seen = new Set<string>();
    const push = (t: ResultTileDTO) => {
      if (!t.path || seen.has(t.path)) return;
      seen.add(t.path);
      out.push(t);
    };
    for (const r of wb().activeRows ?? []) {
      for (const t of r.tiles ?? []) push(t);
    }
    for (const r of wb().issueRows ?? []) {
      for (const t of r.tiles ?? []) push(t);
    }
    for (const t of wb().doneTiles ?? []) push(t);
    return out;
  });

  const currentIndex = computed(() => {
    const p = wb().previewPath;
    if (!p) return -1;
    return previewTiles().findIndex((t) => t.path === p);
  });

  const currentTile = computed(() => {
    const idx = currentIndex();
    return idx >= 0 ? previewTiles()[idx] : undefined;
  });

  const stepPreview = (delta: number) => {
    const list = previewTiles();
    if (list.length <= 1) return;
    const idx = currentIndex();
    const base = idx >= 0 ? idx : 0;
    const nextIdx = (base + delta + list.length) % list.length;
    const nextPath = list[nextIdx]?.path;
    if (nextPath) setPreviewPath(nextPath);
  };

  return (
    <View>
      {/* 历史会话 */}
      <Dialog
        open={Boolean(wb().historyOpen)}
        title="历史记录"
        description="点击任意历史记录可恢复当时的提示词与结果队列"
        style={{ width: 560, height: 500 }}
        testID="history-dialog"
        onClose={() => setHistoryOpen(false)}
        closeOnBackdropPress
        closeOnEscape
      >
        <View class="min-h-0 w-full min-w-0 flex-1 gap-3" testID="history-dialog-body">
          <View class="flex-row items-center justify-between">
            <Text class="text-xs text-slate-500 dark:text-slate-400">
              {`共 ${(wb().sessions ?? []).length} 条记录`}
            </Text>
            <Button
              text="新建空白记录"
              size="small"
              icon="plus"
              variant="outline"
              onPress={() => sessionAction("new", 0)}
            />
          </View>
          <Scroll class="min-h-0 flex-1" contentClass="gap-2 pr-1">
            <Each of={wb().sessions ?? []} key={(s) => String(s.id)}>
              {(sess) => (
                <View
                  class="flex-row items-center justify-between gap-2 rounded-lg border p-2.5 transition duration-150"
                  classList={{
                    "border-blue-500 bg-blue-50/70 dark:border-blue-400 dark:bg-blue-500/10": Boolean(sess().active),
                    "border-slate-200/80 bg-white dark:border-[#1E2738] dark:bg-[#161E2E]": !sess().active,
                  }}
                >
                  <View class="min-w-0 flex-1 gap-0.5">
                    <View class="flex-row items-center gap-1.5">
                      <Text class="flex-1 truncate text-xs font-medium text-slate-900 dark:text-slate-100">
                        {sess().title || `会话 #${sess().id}`}
                      </Text>
                      <If when={Boolean(sess().active)}>
                        <Badge text="当前" variant="accent" />
                      </If>
                    </View>
                    <Text class="text-[11px] text-slate-500 dark:text-slate-400">
                      {[sess().countText, sess().when].filter(Boolean).join(" · ")}
                    </Text>
                  </View>
                  <View class="flex-row items-center gap-1">
                    <Button
                      text="打开"
                      size="small"
                      variant="outline"
                      onPress={() => sessionAction("reopen", sess().id)}
                    />
                    <Button
                      text="删除"
                      size="small"
                      variant="ghost"
                      onPress={() => sessionAction("delete", sess().id)}
                    />
                  </View>
                </View>
              )}
            </Each>
          </Scroll>
        </View>
      </Dialog>

      {/* 结构化提示词编辑器 */}
      <Dialog
        open={Boolean(wb().structuredOpen)}
        title="结构化提示词拼装"
        description="按模块拆分填写主体、背景、风格、光线、构图与文字要求，实时同步到输入框"
        style={{ width: 580, height: 540 }}
        testID="structured-dialog"
        onClose={() => setWorkbenchField("structuredOpen", "false")}
        closeOnBackdropPress
        closeOnEscape
      >
        <View class="min-h-0 w-full min-w-0 flex-1 gap-2.5">
          <Scroll class="min-h-0 flex-1" contentClass="gap-2.5 pr-1">
            <Each of={parts().items ?? []} key={(p) => p.key || p.name || ""}>
              {(item) => (
                <View class="gap-1.5 rounded-lg border border-slate-200/80 p-2.5 dark:border-[#1E2738] dark:bg-[#161E2E]">
                  <Checkbox
                    text={item().name || item().key || ""}
                    checked={Boolean(item().enabled)}
                    onChange={({ checked }) => setPromptPart(item().key || "", checked, item().value || "")}
                  />
                  <Input
                    value={item().value || ""}
                    placeholder={`填写${item().name || ""}描述...`}
                    onChange={({ value }) =>
                      setPromptPart(item().key || "", Boolean(item().enabled || value.trim() !== ""), value)
                    }
                  />
                </View>
              )}
            </Each>
          </Scroll>
        </View>
      </Dialog>

      {/* 素材画廊管理 */}
      <Dialog
        open={Boolean(wb().galleryOn)}
        title="全部素材图片"
        description={`已选 ${wb().gallerySelected} / ${(wb().galleryItems ?? []).length} 张参与本次任务`}
        style={{ width: 640, height: 540 }}
        testID="gallery-dialog"
        onClose={() => setGalleryOpen(false)}
        closeOnBackdropPress
        closeOnEscape
      >
        <View class="min-h-0 w-full min-w-0 flex-1 gap-3" testID="gallery-dialog-body">
          <View class="flex-row items-center justify-between gap-2">
            <View class="flex-row items-center gap-2">
              <Button
                text={wb().galleryAll ? "全不选" : "全选"}
                size="small"
                variant="outline"
                onPress={() => setAllGallerySelect(!wb().galleryAll)}
              />
              <If when={(wb().mode === "wash" || !wb().mode) && wb().washedCount > 0 && !wb().running}>
                <Button
                  testID="gallery-clear-washed-btn"
                  text={`清空已洗 (${wb().washedCount})`}
                  size="small"
                  variant="outline"
                  onPress={() => clearWashedWorkbenchSources()}
                />
              </If>
            </View>
            <Button
              text="完成"
              size="small"
              variant="primary"
              onPress={() => setGalleryOpen(false)}
            />
          </View>
          <Scroll class="min-h-0 flex-1" contentClass="gap-2 pr-1">
            <Each of={wb().galleryItems ?? []} key={(it) => it.path || String(it.index)}>
              {(item) => (
                <View class="flex-row items-center gap-3 rounded-lg border border-slate-200/80 p-2 transition duration-150 dark:border-[#1E2738] dark:bg-[#161E2E]">
                  <View class="size-12 shrink-0 items-center justify-center overflow-hidden rounded bg-slate-100 dark:bg-[#0D1118]">
                    <If when={Boolean(item().fileURL)}>
                      <Image
                        src={item().fileURL}
                        fadeDuration={150}
                        class="size-12 object-cover"
                        testID={`gallery-thumb-${item().index}`}
                      />
                    </If>
                  </View>
                  <View class="min-w-0 flex-1 gap-0.5">
                    <Text class="truncate text-xs font-medium text-slate-900 dark:text-slate-100">
                      {item().name || item().path}
                    </Text>
                    <Text class="truncate text-[11px] text-slate-500 dark:text-slate-400">
                      {item().washing ? "洗图中..." : item().washed ? "已洗图" : item().path}
                    </Text>
                  </View>
                  <View class="shrink-0 flex-row items-center gap-2">
                    <Checkbox
                      text="参与"
                      checked={Boolean(item().selected)}
                      onChange={({ checked }) => toggleGallerySelect(item().path || "", checked)}
                    />
                    <Button
                      text="移除"
                      size="small"
                      variant="ghost"
                      onPress={() => removeGallerySourceItem(item().path || "")}
                    />
                  </View>
                </View>
              )}
            </Each>
          </Scroll>
        </View>
      </Dialog>

      {/* 大图预览 Lightbox */}
      <Dialog
        open={Boolean(wb().previewOn)}
        title={currentTile()?.title || "图片预览"}
        description={wb().previewPath}
        style={{ width: 780, height: 600 }}
        testID="lightbox-dialog"
        onClose={() => setPreviewPath("")}
        closeOnBackdropPress
        closeOnEscape
      >
        <View class="min-h-0 w-full min-w-0 flex-1 gap-3">
          <View class="relative min-h-0 flex-1 items-center justify-center overflow-hidden rounded-xl bg-slate-100/80 dark:bg-[#0B0F17]">
            <If when={Boolean(wb().previewURL)}>
              <Image
                src={wb().previewURL}
                fadeDuration={180}
                class="h-full w-full object-contain"
                testID="lightbox-preview-image"
              />
            </If>
          </View>
          <View class="flex-row flex-wrap items-center justify-between gap-2 pt-0.5">
            <View class="flex-row items-center gap-2">
              <If when={previewTiles().length > 1}>
                <View class="flex-row items-center gap-1.5">
                  <IconButton
                    icon="chevron-left"
                    size={26}
                    variant="raised"
                    accessibilityLabel="上一张"
                    onPress={() => stepPreview(-1)}
                  />
                  <Text
                    class="px-1 text-xs font-medium text-slate-600 dark:text-slate-300"
                    style={{ fontVariant: ["tabular-nums"] }}
                  >
                    {`${currentIndex() >= 0 ? currentIndex() + 1 : 1} / ${previewTiles().length}`}
                  </Text>
                  <IconButton
                    icon="chevron-right"
                    size={26}
                    variant="raised"
                    accessibilityLabel="下一张"
                    onPress={() => stepPreview(1)}
                  />
                </View>
              </If>
              <If when={Boolean(currentTile()?.platform || currentTile()?.elapsed)}>
                <Text
                  class="text-xs text-slate-400 dark:text-slate-500"
                  style={{ fontVariant: ["tabular-nums"] }}
                >
                  {[currentTile()?.platform, currentTile()?.elapsed].filter(Boolean).join(" · ")}
                </Text>
              </If>
            </View>

            <View class="flex-row flex-wrap items-center gap-2">
              <If when={Boolean(currentTile()?.canRewash)}>
                <Button
                  testID="lightbox-rewash-btn"
                  text="放回待洗（二次清洗）"
                  size="small"
                  icon="rotate-ccw"
                  variant="outline"
                  onPress={() => {
                    const t = currentTile();
                    if (!t) return;
                    requeueWorkbenchDone([t.taskIndex]);
                    setPreviewPath("");
                  }}
                />
              </If>
              <If when={Boolean(currentTile()?.canReplace)}>
                <Button
                  testID="lightbox-replace-btn"
                  text="替换原图"
                  size="small"
                  icon="replace"
                  variant="outline"
                  onPress={() => {
                    const t = currentTile();
                    if (t) void replaceResultImage(t.taskIndex);
                  }}
                />
              </If>
              <If when={Boolean(currentTile()?.actionText)}>
                <Button
                  text={currentTile()?.actionText}
                  size="small"
                  icon="refresh-cw"
                  variant="outline"
                  onPress={() => {
                    const t = currentTile();
                    if (t) retryTask(t.taskIndex);
                  }}
                />
              </If>
              <Button
                text="在文件夹中显示"
                size="small"
                icon="folder-open"
                variant="outline"
                onPress={() => {
                  if (wb().previewPath) void revealPath(wb().previewPath!);
                }}
              />
              <Button
                text="系统默认打开"
                size="small"
                icon="external-link"
                variant="outline"
                onPress={() => {
                  if (wb().previewPath) void openPath(wb().previewPath!);
                }}
              />
              <Button text="关闭" size="small" variant="primary" onPress={() => setPreviewPath("")} />
            </View>
          </View>
        </View>
      </Dialog>
    </View>
  );
}
