import { effect, List, signal, Text, View } from "nsgo";
import { Badge, Button, Checkbox, Dialog, Input, Select } from "nsgo/ui";
import {
  createNewPrompt,
  deleteUserPrompt,
  restoreBuiltinPrompt,
  savePromptEdits,
  selectPrompt,
  setPromptFilter,
  setPromptSuffix,
} from "tulai/backend";
import { EmptyNotice, PageToolbar, SectionCard } from "../components/primitives";
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

      {/* 左右分栏 */}
      <View class="min-h-0 flex-1 gap-3 p-3 md:flex-row">
        {/* 左侧列表（顶部固定搜索） */}
        <View testID="prompts-list" class="w-full min-h-0 md:w-[272px]">
          <SectionCard class="flex-1 min-h-0" compact title={`模板列表 (${(st().items ?? []).length})`}>
            <View class="min-h-0 flex-1 gap-2">
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
                    class="gap-1 rounded-lg border p-2"
                    classList={{
                      "border-blue-600 bg-blue-50/70 dark:border-blue-400 dark:bg-blue-500/10":
                        item().id === st().selectedID,
                      "border-slate-200 bg-white hover:bg-slate-50 dark:border-[#222B3C] dark:bg-[#1A2232] dark:hover:bg-[#232D42]":
                        item().id !== st().selectedID,
                    }}
                  >
                    <View class="flex-row items-center justify-between gap-2">
                      <Text class="flex-1 truncate text-xs font-medium text-slate-900 dark:text-slate-100">
                        {item().name || item().label}
                      </Text>
                      <Badge text={item().kindLbl || item().kind || "通用"} variant="outline" />
                    </View>
                    <Text class="truncate text-[11px] text-slate-500 dark:text-slate-400">
                      {item().text || ""}
                    </Text>
                  </View>
                )}
              </List>
            </View>
          </SectionCard>
        </View>

        {/* 右侧编辑器 */}
        <View testID="prompts-editor" class="min-w-0 min-h-0 flex-1">
          <SectionCard
            class="flex-1 min-h-0"
            compact
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
          >
            <If
              when={Boolean(st().selectedID)}
              else={<EmptyNotice icon="book-open" title="请从左侧选择一个模板" />}
            >
              <View class="min-h-0 flex-1 gap-3">
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
                      onChange={({ value }) => draftKind.set(value)}
                    />
                  </View>
                </View>
                <Input
                  multiline
                  value={draftText()}
                  placeholder="填写提示词正文内容..."
                  onChange={({ value }) => draftText.set(value)}
                  class="min-h-0 flex-1 rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs text-slate-900 dark:border-[#334155] dark:bg-[#0D1118] dark:text-slate-100"
                />
              </View>
            </If>
          </SectionCard>
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
              <Select options={KIND_OPTIONS} value={newKind()} onChange={({ value }) => newKind.set(value)} />
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
