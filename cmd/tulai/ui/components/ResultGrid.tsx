import { Image, signal, Text, View } from "nsgo";
import { Badge, Button, Icon } from "nsgo/ui";
import {
  type ResultTileDTO,
  openOutputFolder,
  openPath,
  replaceResultImage,
  retryAllFailed,
  retryTask,
  revealPath,
  setPreviewPath,
  setResultsDoneOpen,
} from "tulai/backend";
import { workbench } from "../store";
import { EmptyNotice, SectionCard } from "./primitives";

function tileBadgeVariant(status?: string): "up" | "warn" | "danger" | "info" | "neutral" {
  switch (status) {
    case "已完成":
      return "up";
    case "生成中":
    case "排队中":
    case "重试中":
      return "info";
    case "需要人工验证":
    case "需核对":
      return "warn";
    case "失败":
    case "已取消":
      return "danger";
    default:
      return "neutral";
  }
}

function ResultTileCard(props: { tile: ResultTileDTO }) {
  const imgSrc = () => props.tile.fileURL || props.tile.srcURL || "";
  return (
    <View class="w-[168px] gap-1 rounded-lg border border-slate-200 bg-slate-50/70 p-1.5 dark:border-[#222B3C] dark:bg-[#1A2232]">
      <View
        onPress={() => {
          const p = props.tile.path;
          if (p) setPreviewPath(p);
        }}
        class="h-[124px] w-full items-center justify-center overflow-hidden rounded-md bg-slate-200/70 dark:bg-[#0D1118]"
      >
        <If
          when={Boolean(imgSrc())}
          else={<Icon name="image" size={24} color="#94A3B8" />}
        >
          <Image
            src={imgSrc()}
            class="h-full w-full object-contain"
            testID={`result-tile-image-${props.tile.taskIndex}`}
          />
        </If>
      </View>

      <View class="gap-0.5">
        <View class="flex-row items-center justify-between gap-1">
          <Text class="flex-1 truncate text-xs font-medium text-slate-900 dark:text-slate-100">
            {props.tile.title || `任务 #${props.tile.taskIndex + 1}`}
          </Text>
          <Badge text={props.tile.status || "-"} variant={tileBadgeVariant(props.tile.status)} />
        </View>
        <If when={Boolean(props.tile.platform || props.tile.elapsed)}>
          <Text class="truncate text-[11px] text-slate-500 dark:text-slate-400">
            {[props.tile.platform, props.tile.elapsed].filter(Boolean).join(" · ")}
          </Text>
        </If>
        <If when={Boolean(props.tile.reason)}>
          <Text class="truncate text-[11px] text-rose-600 dark:text-rose-400">
            {props.tile.reason}
          </Text>
        </If>
      </View>

      <View class="flex-row flex-wrap items-center gap-1 border-t border-slate-200/60 pt-1 dark:border-[#222B3C]">
        <If when={Boolean(props.tile.path)}>
          <Button
            text="定位"
            size="small"
            variant="ghost"
            onPress={() => {
              if (props.tile.path) void revealPath(props.tile.path);
            }}
          />
          <Button
            text="打开"
            size="small"
            variant="ghost"
            onPress={() => {
              if (props.tile.path) void openPath(props.tile.path);
            }}
          />
        </If>
        <If when={Boolean(props.tile.actionText)}>
          <Button
            text={props.tile.actionText}
            size="small"
            variant="outline"
            onPress={() => retryTask(props.tile.taskIndex)}
          />
        </If>
        <If when={Boolean(props.tile.canReplace)}>
          <Button
            text="替换"
            size="small"
            variant="outline"
            onPress={() => void replaceResultImage(props.tile.taskIndex)}
          />
        </If>
      </View>
    </View>
  );
}

export function ResultGrid() {
  const wb = workbench;
  const doneLimit = signal(24);
  const activeRows = () => wb().activeRows ?? [];
  const issueRows = () => wb().issueRows ?? [];
  const doneTiles = () => wb().doneTiles ?? [];
  const total = () => wb().totalCount;

  return (
    <SectionCard
      compact
      title="生成队列与结果"
      subtitle={`共 ${total()} 个任务 · 进行中 ${wb().activeCount} · 已完成 ${wb().doneCount}${wb().failedCount > 0 ? ` · 失败 ${wb().failedCount}` : ""}`}
      right={
        <View class="flex-row items-center gap-2">
          <If when={wb().failedCount + wb().needsCount > 0}>
            <Button
              text="重试失败"
              size="small"
              icon="refresh-cw"
              variant="outline"
              onPress={() => retryAllFailed()}
            />
          </If>
          <Button
            text="打开输出目录"
            size="small"
            icon="folder-open"
            variant="ghost"
            onPress={() => void openOutputFolder()}
          />
        </View>
      }
    >
      <If
        when={total() > 0 || activeRows().length > 0 || issueRows().length > 0 || doneTiles().length > 0}
        else={
          <EmptyNotice
            icon="sparkles"
            title="暂无生成结果"
            subtitle="在左侧配置提示词与参考图后，点击上方开始按钮生成图片"
          />
        }
      >
        <View class="gap-3">
          <If when={activeRows().length > 0}>
            <View class="gap-2">
              <Text class="text-xs font-semibold text-blue-600 dark:text-blue-400">
                进行中
              </Text>
              <Each of={activeRows()} key={(r) => r.key || r.title || ""}>
                {(row) => (
                  <View class="gap-2">
                    <If when={Boolean(row().header)}>
                      <Text class="text-xs font-medium text-slate-600 dark:text-slate-300">
                        {row().title} {row().summary ? `· ${row().summary}` : ""}
                      </Text>
                    </If>
                    <View class="flex-row flex-wrap gap-2.5">
                      <Each of={row().tiles ?? []} key={(t) => t.key || String(t.taskIndex)}>
                        {(tile) => <ResultTileCard tile={tile()} />}
                      </Each>
                    </View>
                  </View>
                )}
              </Each>
            </View>
          </If>

          <If when={issueRows().length > 0}>
            <View class="gap-2">
              <Text class="text-xs font-semibold text-amber-600 dark:text-amber-400">
                异常 / 需处理
              </Text>
              <Each of={issueRows()} key={(r) => r.key || r.title || ""}>
                {(row) => (
                  <View class="gap-2">
                    <If when={Boolean(row().header)}>
                      <Text class="text-xs font-medium text-slate-600 dark:text-slate-300">
                        {row().title} {row().summary ? `· ${row().summary}` : ""}
                      </Text>
                    </If>
                    <View class="flex-row flex-wrap gap-2.5">
                      <Each of={row().tiles ?? []} key={(t) => t.key || String(t.taskIndex)}>
                        {(tile) => <ResultTileCard tile={tile()} />}
                      </Each>
                    </View>
                  </View>
                )}
              </Each>
            </View>
          </If>

          <If when={doneTiles().length > 0}>
            <View class="gap-2">
              <View class="flex-row items-center justify-between">
                <Text class="text-xs font-semibold text-emerald-700 dark:text-emerald-400">
                  {`已完成 (${doneTiles().length} 张 / ${wb().doneTasks} 任务)`}
                </Text>
                <Button
                  text={wb().resultsDoneOpen ? "收起已完成" : "展开已完成"}
                  size="small"
                  variant="ghost"
                  onPress={() => setResultsDoneOpen(!wb().resultsDoneOpen)}
                />
              </View>
              <If when={Boolean(wb().resultsDoneOpen) || (activeRows().length === 0 && issueRows().length === 0)}>
                <View class="gap-2">
                  <View class="flex-row flex-wrap gap-2.5">
                    <Each of={doneTiles().slice(0, doneLimit())} key={(t) => t.key || `${t.taskIndex}-${t.path || ""}`}>
                      {(tile) => <ResultTileCard tile={tile()} />}
                    </Each>
                  </View>
                  <If when={doneTiles().length > doneLimit()}>
                    <View class="flex-row items-center gap-2 pt-1">
                      <Button
                        text="显示更多 (+24)"
                        size="small"
                        variant="outline"
                        onPress={() => doneLimit.set(doneLimit() + 24)}
                      />
                      <Button
                        text="展开全部"
                        size="small"
                        variant="ghost"
                        onPress={() => doneLimit.set(doneTiles().length)}
                      />
                    </View>
                  </If>
                </View>
              </If>
            </View>
          </If>
        </View>
      </If>
    </SectionCard>
  );
}
