import { Image, Scroll, Text, View } from "nsgo";
import { Badge, Button, Checkbox, Dialog, Input } from "nsgo/ui";
import {
  openPath,
  removeGallerySourceItem,
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
import { promptParts, workbench } from "../store";

export function WorkbenchDialogs() {
  const wb = workbench;
  const parts = promptParts;

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
                  class="flex-row items-center justify-between gap-2 rounded-lg border p-2.5"
                  classList={{
                    "border-blue-500 bg-blue-50/70 dark:border-blue-400 dark:bg-blue-500/10": Boolean(sess().active),
                    "border-slate-200 bg-white dark:border-[#222B3C] dark:bg-[#1A2232]": !sess().active,
                  }}
                >
                  <View class="min-w-0 flex-1 gap-0.5">
                    <View class="flex-row items-center gap-1.5">
                      <Text class="flex-1 truncate text-xs font-medium text-slate-900 dark:text-slate-100">
                        {sess().title || `会话 #${sess().id}`}
                      </Text>
                      <If when={Boolean(sess().active)}>
                        <Badge text="当前" variant="info" />
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
                <View class="gap-1.5 rounded-lg border border-slate-200 p-2.5 dark:border-[#222B3C] dark:bg-[#1A2232]">
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
                <View class="flex-row items-center gap-3 rounded-lg border border-slate-200 p-2 dark:border-[#222B3C] dark:bg-[#1A2232]">
                  <View class="size-12 shrink-0 items-center justify-center overflow-hidden rounded bg-slate-100 dark:bg-[#0D1118]">
                    <If when={Boolean(item().fileURL)}>
                      <Image
                        src={item().fileURL}
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
        title="图片预览"
        description={wb().previewPath}
        style={{ width: 720, height: 580 }}
        testID="lightbox-dialog"
        onClose={() => setPreviewPath("")}
        closeOnBackdropPress
        closeOnEscape
      >
        <View class="min-h-0 w-full min-w-0 flex-1 gap-3">
          <View class="min-h-0 flex-1 items-center justify-center overflow-hidden rounded-lg bg-slate-100 dark:bg-[#0D1118]">
            <If when={Boolean(wb().previewURL)}>
              <Image src={wb().previewURL} class="h-full w-full object-contain" testID="lightbox-preview-image" />
            </If>
          </View>
          <View class="flex-row items-center justify-end gap-2">
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
      </Dialog>
    </View>
  );
}
