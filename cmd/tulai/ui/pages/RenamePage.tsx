import { List, Scroll, Text, View } from "nsgo";
import { Badge, Button, Checkbox, IconButton, Input, Select, Tabs } from "nsgo/ui";
import {
  renameAddRule,
  renameApply,
  renameClear,
  renameIngestPaths,
  renameMoveRow,
  renamePickFiles,
  renamePickFolder,
  renameRemoveAt,
  renameRemoveRule,
  renameSaveAIModel,
  renameSetDraft,
  renameSetField,
  renameSetRule,
  renameSuggestAI,
  renameUndo,
} from "tulai/backend";
import { EmptyNotice, PageToolbar, SectionCard } from "../components/primitives";
import { renameState } from "../store";

const RENAME_MODES = [
  { value: "0", label: "查找替换" },
  { value: "2", label: "插入文本" },
  { value: "3", label: "序号重排" },
  { value: "1", label: "逐行编辑" },
  { value: "4", label: "AI 命名" },
];

const POSITION_OPTIONS = [
  { value: "开头", label: "文件名开头" },
  { value: "结尾", label: "文件名结尾" },
  { value: "指定位置", label: "指定字符后" },
];

const SORT_OPTIONS = [
  { value: "name", label: "按名称排序" },
  { value: "mtime", label: "按修改时间" },
  { value: "size", label: "按文件大小" },
];

export function RenamePage() {
  const st = renameState;

  return (
    <View
      class="min-h-0 flex-1"
      onFileDrop={(e) => {
        if (e.uris.length > 0) renameIngestPaths(e.uris);
      }}
    >
      {/* 固定顶部栏 */}
      <PageToolbar
        left={
          <View class="flex-row flex-wrap items-center gap-1.5">
            <Button text="添加文件" size="small" icon="plus" variant="outline" onPress={() => void renamePickFiles()} />
            <Button text="添加目录" size="small" icon="folder" variant="outline" onPress={() => void renamePickFolder(false)} />
            <Button text="递归目录" size="small" variant="outline" onPress={() => void renamePickFolder(true)} />
            <If when={(st().rows ?? []).length > 0}>
              <Button text="清空" size="small" variant="ghost" onPress={() => renameClear()} />
            </If>
          </View>
        }
        right={
          <View class="flex-row items-center gap-2">
            <If when={Boolean(st().canUndo)}>
              <Button text="撤销上次" size="small" icon="undo-2" variant="outline" onPress={() => renameUndo()} />
            </If>
            <Button
              text={st().busy ? "执行中..." : `执行重命名 (${st().changedCount})`}
              size="small"
              icon="check"
              variant="primary"
              busy={Boolean(st().busy)}
              disabled={st().changedCount === 0 || Boolean(st().busy)}
              onPress={() => renameApply()}
            />
          </View>
        }
        bottom={
          <If when={Boolean(st().err || st().planErr || st().note)}>
            <Text
              class="text-xs"
              classList={{
                "text-rose-600 dark:text-rose-400": Boolean(st().err || st().planErr),
                "text-slate-600 dark:text-slate-300": !st().err && !st().planErr,
              }}
            >
              {st().err || st().planErr || st().note || ""}
            </Text>
          </If>
        }
      />

      {/* 左：规则配置 / 右：实时对比预览表 */}
      <View class="min-h-0 flex-1 flex-row gap-2 p-2">
        <View
          testID="rename-config"
          class="h-full min-h-0 shrink-0"
          style={{ width: "28%", minWidth: 280, maxWidth: 380 }}
        >
          <Scroll class="min-h-0 flex-1" contentClass="gap-2.5 pr-1">
            <SectionCard compact title="命名模式与规则">
              <View class="gap-3">
                <Tabs
                  variant="segmented"
                  size="small"
                  options={RENAME_MODES}
                  value={String(st().mode)}
                  onChange={({ value }) => renameSetField("mode", value)}
                />

                <View class="flex-row items-center justify-between gap-2">
                  <Checkbox
                    text="包含扩展名"
                    checked={Boolean(st().includeExt)}
                    onChange={({ checked }) =>
                      renameSetField("includeExt", checked ? "true" : "false")
                    }
                  />
                  <View class="w-[140px]">
                    <Select
                      options={SORT_OPTIONS}
                      value={st().sortBy || "name"}
                      style={{ width: "100%", alignSelf: "stretch" }}
                      onChange={({ value }) => renameSetField("sortBy", value)}
                    />
                  </View>
                </View>

                {/* 模式 0: 查找替换规则 */}
                <If when={st().mode === 0}>
                  <View class="gap-2 rounded-lg bg-slate-50 p-2.5 dark:bg-[#1A2232]">
                    <View class="flex-row items-center justify-between">
                      <Text class="text-xs font-medium text-slate-700 dark:text-slate-200">
                        替换规则列表
                      </Text>
                      <Button
                        text="新增规则"
                        size="small"
                        icon="plus"
                        variant="outline"
                        onPress={() => renameAddRule()}
                      />
                    </View>
                    <Each of={st().rules ?? []} key={(r) => String(r.index)}>
                      {(rule) => (
                        <View class="gap-1.5 rounded border border-slate-200 bg-white p-2 dark:border-[#222B3C] dark:bg-[#131924]">
                          <View class="flex-row items-center gap-1.5">
                            <Input
                              value={rule().find || ""}
                              placeholder="查找内容..."
                              onChange={({ value }) =>
                                renameSetRule(
                                  rule().index,
                                  value,
                                  rule().with || "",
                                  rule().text || "",
                                  rule().template,
                                  rule().count,
                                  rule().offset,
                                )
                              }
                              class="flex-1"
                            />
                            <IconButton
                              icon="trash"
                              variant="ghost"
                              size={26}
                              accessibilityLabel="删除规则"
                              onPress={() => renameRemoveRule(rule().index)}
                            />
                          </View>
                          <Input
                            value={rule().with || ""}
                            placeholder="替换为..."
                            onChange={({ value }) =>
                              renameSetRule(
                                rule().index,
                                rule().find || "",
                                value,
                                rule().text || "",
                                rule().template,
                                rule().count,
                                rule().offset,
                              )
                            }
                          />
                        </View>
                      )}
                    </Each>
                  </View>
                </If>

                {/* 模式 2: 插入文本 */}
                <If when={st().mode === 2}>
                  <View class="gap-2 rounded-lg bg-slate-50 p-2.5 dark:bg-[#1A2232]">
                    <View class="flex-row items-center gap-2">
                      <View class="flex-1">
                        <Select
                          options={POSITION_OPTIONS}
                          value={st().position || "开头"}
                          style={{ width: "100%", alignSelf: "stretch" }}
                          onChange={({ value }) => renameSetField("position", value)}
                        />
                      </View>
                      <If when={st().position === "指定位置"}>
                        <Input
                          value={String(st().index)}
                          placeholder="位置"
                          onChange={({ value }) => renameSetField("index", value)}
                          class="w-[76px]"
                        />
                      </If>
                    </View>
                    <Input
                      value={st().text || ""}
                      placeholder="要插入的文本内容..."
                      onChange={({ value }) => renameSetField("text", value)}
                    />
                  </View>
                </If>

                {/* 模式 3: 序列编号 */}
                <If when={st().mode === 3}>
                  <View class="gap-2 rounded-lg bg-slate-50 p-2.5 dark:bg-[#1A2232]">
                    <View class="flex-row gap-2">
                      <Input
                        value={st().numPrefix || ""}
                        placeholder="前缀 (如 item_)"
                        onChange={({ value }) => renameSetField("numPrefix", value)}
                        class="flex-1"
                      />
                      <Input
                        value={st().numSuffix || ""}
                        placeholder="后缀"
                        onChange={({ value }) => renameSetField("numSuffix", value)}
                        class="flex-1"
                      />
                    </View>
                    <View class="flex-row items-center gap-2">
                      <Text class="text-xs text-slate-500">起始序号</Text>
                      <Input
                        value={String(st().numStart)}
                        onChange={({ value }) => renameSetField("numStart", value)}
                        class="flex-1"
                      />
                      <Text class="text-xs text-slate-500">补零位数</Text>
                      <Input
                        value={String(st().numWidth)}
                        onChange={({ value }) => renameSetField("numWidth", value)}
                        class="flex-1"
                      />
                    </View>
                  </View>
                </If>

                {/* 模式 1: 逐行编辑提示 */}
                <If when={st().mode === 1}>
                  <View class="rounded-lg bg-slate-50 p-2.5 dark:bg-[#1A2232]">
                    <Text class="text-xs text-slate-600 dark:text-slate-300">
                      在右侧列表中直接逐行编辑每个文件的新名称
                    </Text>
                  </View>
                </If>

                {/* 模式 4: AI 智能命名 */}
                <If when={st().mode === 4}>
                  <View class="gap-2 rounded-lg bg-slate-50 p-2.5 dark:bg-[#1A2232]">
                    <Input
                      multiline
                      value={st().aiPrompt || ""}
                      placeholder="描述重命名规则（如：按电商主图场景生成简洁中文文件名）..."
                      onChange={({ value }) => renameSetField("aiPrompt", value)}
                      class="h-[78px] rounded-lg border border-slate-200 bg-white p-2 text-xs dark:border-[#334155] dark:bg-[#131924]"
                    />
                    <View class="flex-row items-center justify-between gap-2">
                      <Button
                        text="模型配置"
                        size="small"
                        variant="outline"
                        onPress={() =>
                          renameSetField("aiSettings", st().aiSettings ? "false" : "true")
                        }
                      />
                      <Button
                        text="AI 生成建议"
                        size="small"
                        icon="sparkles"
                        variant="primary"
                        busy={Boolean(st().busy)}
                        onPress={() => renameSuggestAI()}
                      />
                    </View>
                    <If when={Boolean(st().aiSettings)}>
                      <View class="gap-1.5 pt-1">
                        <Input
                          value={st().aiurl || ""}
                          placeholder="OpenAI 兼容接口地址"
                          onChange={({ value }) => renameSetField("aiURL", value)}
                        />
                        <Input
                          value={st().aiKey || ""}
                          placeholder="API Key"
                          masked
                          onChange={({ value }) => renameSetField("aiKey", value)}
                        />
                        <View class="flex-row items-center gap-1.5">
                          <Input
                            value={st().aiModel || ""}
                            placeholder="模型名称"
                            onChange={({ value }) => renameSetField("aiModel", value)}
                            class="flex-1"
                          />
                          <Button
                            text="保存"
                            size="small"
                            variant="outline"
                            onPress={() =>
                              renameSaveAIModel(st().aiurl || "", st().aiKey || "", st().aiModel || "")
                            }
                          />
                        </View>
                      </View>
                    </If>
                  </View>
                </If>
              </View>
            </SectionCard>
          </Scroll>
        </View>

        <View testID="rename-preview" class="min-w-0 min-h-0 flex-1">
          <SectionCard
            class="flex-1 min-h-0"
            compact
            title={`实时预览 (${(st().rows ?? []).length} 个文件)`}
            subtitle={`预计更改 ${st().changedCount} 个文件名`}
          >
            <If
              when={(st().rows ?? []).length > 0}
              else={
                <EmptyNotice
                  icon="pencil"
                  title="尚未添加文件"
                  subtitle="点击顶部添加文件/文件夹，或直接拖拽到界面"
                />
              }
            >
              <View class="min-h-0 flex-1 gap-1.5">
                {/* 固定表头 */}
                <View class="flex-row items-center gap-2 rounded-md bg-slate-100/80 px-2.5 py-1.5 dark:bg-[#1A2232]">
                  <Text class="w-[28px] text-center text-[11px] font-semibold text-slate-500">#</Text>
                  <Text class="flex-1 text-[11px] font-semibold text-slate-600 dark:text-slate-300">原文件名</Text>
                  <Text class="w-[14px]" />
                  <Text class="flex-1 text-[11px] font-semibold text-slate-600 dark:text-slate-300">新文件名</Text>
                  <Text class="w-[90px] text-right text-[11px] font-semibold text-slate-500">排序/移除</Text>
                </View>

                <List
                  class="min-h-0 flex-1"
                  contentClass="gap-1.5 pr-1"
                  each={st().rows ?? []}
                  keyExtractor={(r) => r.path || String(r.index)}
                  estimatedItemSize={38}
                >
                  {(row) => (
                    <View class="flex-row items-center gap-2 rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 dark:border-[#222B3C] dark:bg-[#1A2232]">
                      <Text class="w-[28px] text-center text-[11px] text-slate-400">
                        {String(row().index + 1)}
                      </Text>
                      <Text class="flex-1 truncate text-xs text-slate-600 dark:text-slate-300">
                        {row().oldName}
                      </Text>
                      <Text class="text-xs text-slate-400">→</Text>
                      <If
                        when={st().mode === 1 || st().mode === 4}
                        else={
                          <Text
                            class="flex-1 truncate text-xs font-medium"
                            classList={{
                              "text-blue-600 dark:text-blue-400": Boolean(row().changed),
                              "text-slate-400 dark:text-slate-500": !row().changed,
                            }}
                          >
                            {row().newName || row().oldName}
                          </Text>
                        }
                      >
                        <Input
                          value={row().draft || ""}
                          onChange={({ value }) => renameSetDraft(row().index, value)}
                          class="flex-1"
                        />
                      </If>
                      <View class="w-[90px] flex-row items-center justify-end gap-0.5">
                        <If when={Boolean(row().changed)}>
                          <Badge text="改" variant="accent" />
                        </If>
                        <IconButton
                          icon="arrow-up"
                          variant="ghost"
                          size={24}
                          accessibilityLabel="上移"
                          onPress={() => {
                            if (row().index > 0) renameMoveRow(row().index, row().index - 1);
                          }}
                        />
                        <IconButton
                          icon="arrow-down"
                          variant="ghost"
                          size={24}
                          accessibilityLabel="下移"
                          onPress={() => renameMoveRow(row().index, row().index + 1)}
                        />
                        <IconButton
                          icon="x"
                          variant="ghost"
                          size={24}
                          accessibilityLabel="移除"
                          onPress={() => renameRemoveAt(row().index)}
                        />
                      </View>
                    </View>
                  )}
                </List>
              </View>
            </If>
          </SectionCard>
        </View>
      </View>
    </View>
  );
}
