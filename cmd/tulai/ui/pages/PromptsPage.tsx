import { effect, List, signal, Text, View } from "nsgo";
import { Button, Checkbox, Dialog, Input, Select } from "nsgo/ui";
import {
  createNewPrompt,
  deleteUserPrompt,
  restoreBuiltinPrompt,
  savePromptEdits,
  selectPrompt,
  setPromptFilter,
  setPromptSuffix,
} from "tulai/backend";
import { OutlineBadge } from "tulai/native";
import { EmptyNotice, PageToolbar, PaneHeader } from "../components/primitives";
import { goTab, promptsState, showToast, TAB_GEN, updateWorkbenchField } from "../store";

const KIND_OPTIONS = [
  { value: "生图", label: "生图" },
  { value: "洗图", label: "洗图" },
  { value: "裂变", label: "裂变" },
];

export function PromptsPage() {
  const st = promptsState;
  const draftName = signal("");
  const draftKind = signal("生图");
  const draftText = signal("");
  const newOpen = signal(false);
  const newName = signal("");
  const newKind = signal("生图");
  const newText = signal("");

  effect(() => {
    const s = st();
    draftName.set(s.selectedName || "");
    draftKind.set(s.selectedKind || "生图");
    draftText.set(s.selectedText || "");
  });

  return (
    <View class="min-h-0 flex-1">
      <PageToolbar
        left={
          <View class="flex-row flex-wrap items-center gap-2">
            <Checkbox
              text="启用全局后缀"
              checked={Boolean(st().useSuffix)}
              onChange={({ checked }) => setPromptSuffix(st().suffix || "", checked)}
            />
            <Input
              value={st().suffix || ""}
              placeholder="如：4K 超清摄影质感，无水印"
              onChange={({ value }) => setPromptSuffix(value, Boolean(st().useSuffix))}
              class="w-[260px]"
            />
          </View>
        }
        right={
          <Button
            text="新建模板"
            size="small"
            icon="plus"
            variant="primary"
            onPress={() => {
              newName.set("");
              newKind.set("生图");
              newText.set("");
              newOpen.set(true);
            }}
          />
        }
      />

      {/* 通栏无嵌套左右分栏 */}
      <View class="min-h-0 flex-1 flex-row bg-[#F8FAFC] dark:bg-[#0D1118]">
        {/* 左侧列表（顶部固定搜索） */}
        <View
          testID="prompts-list"
          class="h-full min-h-0 shrink-0 border-r border-[#E5E9F0] bg-white dark:border-[#222B3C] dark:bg-[#131924]"
          style={{ width: "24%", minWidth: 228, maxWidth: 312 }}
        >
          <PaneHeader title={`模板列表 (${(st().items ?? []).length})`} />
          <View class="min-h-0 flex-1 gap-2 p-2.5">
            <Input
              value={st().filter || ""}
              placeholder="搜索模板名称或内容..."
              onChange={({ value }) => setPromptFilter(value)}
            />
            <List
              class="min-h-0 flex-1"
              contentClass="gap-1.5 pr-1"
              each={st().items ?? []}
              keyExtractor={(it) => it.id || it.name || ""}
              estimatedItemSize={52}
              empty={<EmptyNotice icon="book-open" title="没有匹配的模板" />}
            >
              {(item) => (
                <View
                  onPress={() => selectPrompt(item().id || "")}
                  class="gap-1 rounded-md border p-2 transition-colors duration-150"
                  classList={{
                    "border-blue-600 bg-blue-50/70 dark:border-blue-400 dark:bg-blue-500/10":
                      item().id === st().selectedID,
                    "border-[#E5E9F0] bg-white hover:bg-[#F1F5F9]/70 dark:border-[#222B3C] dark:bg-[#1A2232] dark:hover:bg-[#232D42]":
                      item().id !== st().selectedID,
                  }}
                >
                  <View class="flex-row items-center justify-between gap-2">
                    <Text class="flex-1 truncate text-xs font-medium text-slate-900 dark:text-slate-100">
                      {item().name || item().label}
                    </Text>
                    <OutlineBadge text={item().kindLbl || item().kind || "通用"} />
                  </View>
                  <Text class="truncate text-[11px] text-slate-500 dark:text-slate-400">
                    {item().text || ""}
                  </Text>
                </View>
              )}
            </List>
          </View>
        </View>

        {/* 右侧全高编辑器 */}
        <View testID="prompts-editor" class="min-w-0 min-h-0 flex-1 bg-white dark:bg-[#131924]">
          <PaneHeader
            title={st().selectedName || "选择或新建模板"}
            subtitle={st().note || undefined}
            right={
              <If when={Boolean(st().selectedID)}>
                <View class="flex-row flex-wrap items-center gap-1.5">
                  <Button
                    text="填入工作台"
                    size="small"
                    icon="sparkles"
                    variant="outline"
                    onPress={() => {
                      goTab(TAB_GEN);
                      updateWorkbenchField("prompts", draftText());
                      showToast("已将提示词填入自由生图", "success");
                    }}
                  />
                  <If when={Boolean(st().selectedIsBln)}>
                    <Button
                      text="恢复默认"
                      size="small"
                      variant="ghost"
                      onPress={() => restoreBuiltinPrompt()}
                    />
                  </If>
                  <If when={!st().selectedIsBln}>
                    <Button
                      text="删除"
                      size="small"
                      variant="ghost"
                      onPress={() => deleteUserPrompt()}
                    />
                  </If>
                  <Button
                    text="保存"
                    size="small"
                    icon="save"
                    variant="primary"
                    onPress={() => savePromptEdits(draftName(), draftKind(), draftText())}
                  />
                </View>
              </If>
            }
          />
          <View class="min-h-0 flex-1">
            <If
              when={Boolean(st().selectedID)}
              else={
                <View class="flex-1 items-center justify-center p-4">
                  <EmptyNotice icon="book-open" title="请从左侧选择一个模板" />
                </View>
              }
            >
              <View class="min-h-0 flex-1 gap-2.5 p-3">
                <View class="flex-row items-center gap-2">
                  <Input
                    value={draftName()}
                    placeholder="模板名称"
                    onChange={({ value }) => draftName.set(value)}
                    class="flex-1"
                  />
                  <View class="w-[120px]">
                    <Select
                      options={KIND_OPTIONS}
                      value={draftKind()}
                      style={{ width: "100%", alignSelf: "stretch" }}
                      onChange={({ value }) => draftKind.set(value)}
                    />
                  </View>
                </View>
                <Input
                  multiline
                  value={draftText()}
                  placeholder="填写提示词正文内容..."
                  onChange={({ value }) => draftText.set(value)}
                  class="min-h-0 flex-1 rounded-md border border-[#E5E9F0] bg-white p-3 text-xs text-slate-900 dark:border-[#334155] dark:bg-[#0D1118] dark:text-slate-100"
                />
              </View>
              <View class="shrink-0 flex-row items-center justify-between gap-2 border-t border-[#E5E9F0] px-3 py-1.5 dark:border-[#222B3C]">
                <Text
                  class="text-[11px] text-slate-500 dark:text-slate-400"
                  style={{ fontVariant: ["tabular-nums"] }}
                >
                  {`${draftText().length} 字符 · ${draftText().trim() ? draftText().split("\n").length : 0} 行`}
                </Text>
                <Text class="truncate text-[11px] text-slate-500 dark:text-slate-400">
                  {st().useSuffix && st().suffix ? `全局后缀已启用：${st().suffix}` : "全局后缀未启用"}
                </Text>
              </View>
            </If>
          </View>
        </View>
      </View>

      {/* 新建弹窗 */}
      <Dialog
        open={newOpen()}
        title="新建提示词模板"
        style={{ width: 500 }}
        onClose={() => newOpen.set(false)}
        closeOnBackdropPress
        closeOnEscape
      >
        <View class="w-full min-w-0 gap-3">
          <View class="flex-row items-center gap-2">
            <Input
              value={newName()}
              placeholder="模板名称"
              onChange={({ value }) => newName.set(value)}
              class="flex-1"
            />
            <View class="w-[120px]">
              <Select
                options={KIND_OPTIONS}
                value={newKind()}
                style={{ width: "100%", alignSelf: "stretch" }}
                onChange={({ value }) => newKind.set(value)}
              />
            </View>
          </View>
          <Input
            multiline
            value={newText()}
            placeholder="输入模板内容..."
            onChange={({ value }) => newText.set(value)}
            class="h-[140px] rounded-lg border border-slate-200 bg-slate-50 p-2.5 text-xs dark:border-[#334155] dark:bg-[#0D1118]"
          />
          <View class="flex-row justify-end gap-2">
            <Button text="取消" size="small" variant="ghost" onPress={() => newOpen.set(false)} />
            <Button
              text="创建"
              size="small"
              variant="primary"
              disabled={newName().trim() === "" || newText().trim() === ""}
              onPress={() => {
                createNewPrompt(newName(), newKind(), newText());
                newOpen.set(false);
              }}
            />
          </View>
        </View>
      </Dialog>
    </View>
  );
}
