import { Image, Scroll, Text, View } from "nsgo";
import { Badge, Button, Progress, Switch, Tabs } from "nsgo/ui";
import {
  dedupAddFiles,
  dedupAddFolder,
  dedupClear,
  dedupIngestPaths,
  dedupOpenOutDir,
  dedupPickOutDir,
  dedupSetFlatColor,
  dedupSetLevel,
  dedupSetMore,
  dedupStart,
  revealPath,
} from "tulai/backend";
import { EmptyNotice, PageToolbar, SectionCard } from "../components/primitives";
import { dedup } from "../store";

const LEVEL_TABS = [
  { value: "0", label: "严格" },
  { value: "1", label: "均衡 (推荐)" },
  { value: "2", label: "宽松" },
];

export function DedupPage() {
  const st = dedup;

  return (
    <View
      class="min-h-0 flex-1"
      onFileDrop={(e) => {
        if (e.uris.length > 0) dedupIngestPaths(e.uris);
      }}
    >
      <PageToolbar
        left={
          <View class="flex-row flex-wrap items-center gap-3">
            <Tabs
              variant="segmented"
              size="small"
              options={LEVEL_TABS}
              value={String(st().level)}
              onChange={({ value }) => dedupSetLevel(Number(value) || 0)}
            />
            <View class="flex-row items-center gap-2">
              <Switch
                size="small"
                checked={Boolean(st().flatColorOK)}
                onChange={({ checked }) => dedupSetFlatColor(checked)}
              />
              <Text class="text-xs text-slate-700 dark:text-slate-200">允许纯色图</Text>
            </View>
          </View>
        }
        right={
          <View class="flex-row flex-wrap items-center gap-1.5">
            <Button text="添加图片" size="small" icon="plus" variant="outline" onPress={() => void dedupAddFiles()} />
            <Button text="添加目录" size="small" icon="folder" variant="outline" onPress={() => void dedupAddFolder()} />
            <If when={st().itemCount > 0}>
              <Button text="清空" size="small" variant="ghost" onPress={() => dedupClear()} />
            </If>
            <Button
              text={st().busy ? "扫描中..." : "开始去重"}
              size="small"
              icon="layers"
              variant="primary"
              busy={Boolean(st().busy)}
              disabled={st().itemCount < 2 || Boolean(st().busy)}
              onPress={() => dedupStart()}
            />
          </View>
        }
        bottom={
          <View class="gap-1.5">
            <View class="flex-row flex-wrap items-center justify-between gap-2">
              <Text class="flex-1 truncate text-[11px] text-slate-500 dark:text-slate-400">
                {st().levelHint || ""}
              </Text>
              <View class="flex-row items-center gap-1.5">
                <Text class="max-w-[260px] truncate text-[11px] text-slate-500 dark:text-slate-400">
                  {`输出：${st().resolvedOut || st().outDir || "自动"}`}
                </Text>
                <Button text="更改" size="small" variant="outline" onPress={() => void dedupPickOutDir()} />
                <Button text="打开" size="small" variant="ghost" onPress={() => void dedupOpenOutDir()} />
              </View>
            </View>
            <If when={Boolean(st().busy || st().note || st().err)}>
              <View class="gap-1 rounded-lg bg-slate-50 px-2.5 py-1.5 dark:bg-[#1A2232]">
                <View class="flex-row items-center justify-between gap-2">
                  <Text class="text-xs text-slate-700 dark:text-slate-200">
                    {st().err || st().note || ""}
                  </Text>
                  <If when={st().total > 0}>
                    <Text class="text-xs font-medium text-blue-600 dark:text-blue-400">
                      {`${st().scanned} / ${st().total}`}
                    </Text>
                  </If>
                </View>
                <If when={Boolean(st().busy && st().total > 0)}>
                  <Progress value={Math.min(1, st().scanned / st().total)} />
                </If>
              </View>
            </If>
          </View>
        }
      />

      {/* 左：待对比图片池 / 右：相似组报告 */}
      <View class="min-h-0 flex-1 gap-3 p-3 md:flex-row">
        <View testID="dedup-queue" class="w-full min-h-0 md:w-[288px]">
          <SectionCard
            class="flex-1 min-h-0"
            compact
            title={`待扫描图片 (${st().itemCount})`}
            subtitle={st().itemCount < 2 ? "至少需要 2 张图片" : "支持直接拖入"}
            right={
              <If when={st().itemCount > 18}>
                <Button
                  text={st().more ? "收起" : "展开"}
                  size="small"
                  variant="ghost"
                  onPress={() => dedupSetMore(!st().more)}
                />
              </If>
            }
          >
            <Scroll class="min-h-0 flex-1" contentClass="pr-1">
              <If
                when={(st().items ?? []).length > 0}
                else={
                  <EmptyNotice
                    icon="layers"
                    title="尚未添加图片"
                    subtitle="添加图片或文件夹，也可以直接拖拽到这里"
                  />
                }
              >
                <View class="flex-row flex-wrap gap-2">
                  <Each
                    of={st().more ? (st().items ?? []) : (st().items ?? []).slice(0, 18)}
                    key={(it) => it.path || it.name || ""}
                  >
                    {(item) => (
                      <View class="w-[84px] gap-1 rounded-lg border border-slate-200 bg-slate-50/70 p-1 dark:border-[#222B3C] dark:bg-[#1A2232]">
                        <View class="h-[62px] w-full items-center justify-center overflow-hidden rounded bg-slate-100 dark:bg-[#0D1118]">
                          <If when={Boolean(item().fileURL)}>
                            <Image src={item().fileURL} class="h-full w-full object-cover" />
                          </If>
                        </View>
                        <Text class="truncate text-[10px] text-slate-700 dark:text-slate-200">
                          {item().name}
                        </Text>
                      </View>
                    )}
                  </Each>
                </View>
              </If>
            </Scroll>
          </SectionCard>
        </View>

        <View testID="dedup-report" class="min-w-0 min-h-0 flex-1">
          <SectionCard
            class="flex-1 min-h-0"
            compact
            title={
              st().hasReport
                ? `去重报告 · 保留 ${st().unique} 张 / 移出重复 ${st().duplicates} 张`
                : "相似图对照结果"
            }
            subtitle={st().savedDir ? `重复副本已整理到：${st().savedDir}` : "点击“开始去重”后在此展示分组对照"}
          >
            <Scroll class="min-h-0 flex-1" contentClass="gap-3 pr-1">
              <If
                when={Boolean(st().hasReport)}
                else={
                  <EmptyNotice
                    icon="search"
                    title="准备就绪"
                    subtitle="在左侧导入至少 2 张图片后点击右上角“开始去重”"
                  />
                }
              >
                <If
                  when={(st().groups ?? []).length > 0}
                  else={
                    <EmptyNotice
                      icon="circle-check"
                      title="未发现重复图片"
                      subtitle="当前相似度阈值下所有图片均为唯一样本"
                    />
                  }
                >
                  <Each of={st().groups ?? []} key={(g) => String(g.index)}>
                    {(grp) => (
                      <View class="gap-2 rounded-lg border border-slate-200 p-3 dark:border-[#222B3C] dark:bg-[#1A2232]">
                        <View class="flex-row items-center justify-between">
                          <Text class="text-xs font-semibold text-slate-800 dark:text-slate-100">
                            {`相似组 #${grp().index + 1}`}
                          </Text>
                          <Badge text={`汉明距离 ${grp().distance}`} variant="outline" />
                        </View>
                        <View class="flex-row flex-wrap gap-2.5">
                          <View class="w-[152px] gap-1 rounded-lg border-2 border-emerald-500/80 bg-emerald-50/50 p-1.5 dark:bg-emerald-500/10">
                            <View class="h-[96px] w-full overflow-hidden rounded bg-slate-100 dark:bg-[#0D1118]">
                              <If when={Boolean(grp().keep.fileURL)}>
                                <Image src={grp().keep.fileURL} class="h-full w-full object-contain" />
                              </If>
                            </View>
                            <View class="flex-row items-center justify-between gap-1">
                              <Badge text="保留" variant="up" />
                              <Text class="text-[10px] text-slate-500">{`${grp().keep.width}x${grp().keep.height}`}</Text>
                            </View>
                            <Text class="truncate text-[11px] font-medium text-slate-800 dark:text-slate-100">
                              {grp().keep.name}
                            </Text>
                          </View>

                          <Each of={grp().dupes ?? []} key={(d) => d.path || d.name || ""}>
                            {(dup) => (
                              <View
                                onPress={() => {
                                  if (dup().path) void revealPath(dup().path!);
                                }}
                                class="w-[152px] gap-1 rounded-lg border border-slate-200 bg-slate-50 p-1.5 dark:border-[#334155] dark:bg-[#131924]"
                              >
                                <View class="h-[96px] w-full overflow-hidden rounded bg-slate-100 dark:bg-[#0D1118]">
                                  <If when={Boolean(dup().fileURL)}>
                                    <Image src={dup().fileURL} class="h-full w-full object-contain" />
                                  </If>
                                </View>
                                <View class="flex-row items-center justify-between gap-1">
                                  <Badge text="重复" variant="warn" />
                                  <Text class="text-[10px] text-slate-500">{`${dup().width}x${dup().height}`}</Text>
                                </View>
                                <Text class="truncate text-[11px] text-slate-700 dark:text-slate-200">
                                  {dup().name}
                                </Text>
                              </View>
                            )}
                          </Each>
                        </View>
                      </View>
                    )}
                  </Each>
                </If>
              </If>
            </Scroll>
          </SectionCard>
        </View>
      </View>
    </View>
  );
}
