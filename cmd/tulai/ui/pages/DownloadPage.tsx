import { List, Scroll, signal, Text, View } from "nsgo";
import { Button, Checkbox, Dialog, Input, Progress, Switch, Tabs } from "nsgo/ui";
import {
  downloadClearTrash,
  downloadImportAction,
  downloadOpenDir,
  downloadPauseResumeAll,
  downloadPickDir,
  downloadRowAction,
  openDownloadBrowser,
  openDownloadGuide,
  pickDownloadSheet,
  setDownloadField,
  setDownloadFilter,
  submitDownloadURLs,
  toggleDownloadCheck,
} from "tulai/backend";
import { OutlineBadge } from "tulai/native";
import { EmptyNotice, PageToolbar, PaneHeader, StatusBadge } from "../components/primitives";
import { download } from "../store";

export function DownloadPage() {
  const st = download;
  const filtersOpen = signal(false);

  const filterTabs = () => [
    { value: "0", label: `全部 (${st().countAll})` },
    { value: "1", label: `进行中 (${st().countOpen})` },
    { value: "2", label: `已完成 (${st().countDone})` },
    { value: "3", label: `失败/取消 (${st().countCanceled})` },
  ];

  return (
    <View class="min-h-0 flex-1">
      {/* 固定顶部栏：保存路径 + 批量操作 */}
      <PageToolbar
        left={
          <View class="flex-row flex-wrap items-center gap-1.5">
            <Text class="max-w-[300px] truncate text-xs text-slate-600 dark:text-slate-300">
              {`保存至：${st().resolvedDir || st().dir || "默认下载目录"}`}
            </Text>
            <Button text="更改" size="small" variant="outline" onPress={() => void downloadPickDir()} />
            <Button text="打开" size="small" icon="folder-open" variant="ghost" onPress={() => void downloadOpenDir()} />
          </View>
        }
        right={
          <View class="flex-row flex-wrap items-center gap-1.5">
            <Button
              text="导入表格"
              size="small"
              icon="file-spreadsheet"
              variant="outline"
              onPress={() => void pickDownloadSheet()}
            />
            <Button
              text="站点说明"
              size="small"
              icon="book-open"
              variant="ghost"
              onPress={() => openDownloadGuide(true)}
            />
            <Button text="全部暂停" size="small" variant="outline" onPress={() => downloadPauseResumeAll(true)} />
            <Button text="全部继续" size="small" variant="outline" onPress={() => downloadPauseResumeAll(false)} />
            <Button text="清理" size="small" variant="ghost" onPress={() => downloadClearTrash("prune")} />
          </View>
        }
      />

      {/* 左：链接提交与 2 列过滤面板 / 右：独立滚动下载队列 */}
      <View class="min-h-0 flex-1 flex-row bg-[#F8FAFC] dark:bg-[#0D1118]">
        <View
          testID="dl-composer"
          class="h-full min-h-0 shrink-0 border-r border-[#E5E9F0] bg-white dark:border-[#222B3C] dark:bg-[#131924]"
          style={{ width: "27%", minWidth: 276, maxWidth: 360 }}
        >
          <Scroll class="min-h-0 flex-1" contentClass="gap-3 p-3">
            <View class="gap-2.5">
              <View class="gap-0.5">
                <Text class="text-[13px] font-semibold text-slate-900 dark:text-slate-100">添加下载链接</Text>
                <Text class="text-[11px] text-slate-500 dark:text-slate-400">每行一个 HTTP/HTTPS 图片或网页地址</Text>
              </View>

              <Input
                multiline
                value={st().urLs || ""}
                placeholder="粘贴图片直链或网页地址（支持多行批量）..."
                onChange={({ value }) => setDownloadField("urls", value)}
                class="h-[116px] rounded-md border border-[#CBD5E1] bg-white p-2.5 text-xs text-slate-900 dark:border-[#334155] dark:bg-[#0D1118] dark:text-slate-100"
              />

              <View class="flex-row items-center gap-2">
                <Switch
                  size="small"
                  checked={Boolean(st().browserMode)}
                  onChange={({ checked }) =>
                    setDownloadField("browserMode", checked ? "true" : "false")
                  }
                />
                <Text class="flex-1 text-xs text-slate-700 dark:text-slate-200">
                  使用托管 Camoufox 浏览器登录抓取
                </Text>
              </View>

              <View class="flex-row flex-wrap gap-2">
                <Button
                  text="加入下载"
                  size="small"
                  icon="download"
                  variant="primary"
                  onPress={() => submitDownloadURLs(st().urLs || "")}
                />
                <Button
                  text={st().browserBusy ? "正在启动 Camoufox..." : "Camoufox 抓取"}
                  size="small"
                  icon="globe"
                  variant="outline"
                  busy={Boolean(st().browserBusy)}
                  onPress={() => openDownloadBrowser(st().urLs || "")}
                />
              </View>

              <If when={Boolean(st().composerErr || st().browserErr || st().importNotice)}>
                <Text class="text-xs text-amber-600 dark:text-amber-400">
                  {st().composerErr || st().browserErr || st().importNotice || ""}
                </Text>
              </If>
            </View>

            <View class="h-px bg-[#E5E9F0] dark:bg-[#222B3C]" />

            <View class="gap-2.5">
              <View class="flex-row items-center justify-between gap-2">
                <View class="min-w-0 flex-1 gap-0.5">
                  <Text class="text-[13px] font-semibold text-slate-900 dark:text-slate-100">抓取深度与图片过滤</Text>
                  <Text
                    class="text-[11px] text-slate-500 dark:text-slate-400"
                    style={{ fontVariant: ["tabular-nums"] }}
                  >
                    {`深度 ${st().depth} · 尺寸/大小过滤`}
                  </Text>
                </View>
                <Button
                  text={filtersOpen() ? "收起" : "展开"}
                  size="small"
                  variant="ghost"
                  onPress={() => filtersOpen.set(!filtersOpen())}
                />
              </View>

              <If when={filtersOpen()}>
                <View style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
                  <View class="min-w-0 gap-1">
                    <Text class="text-[11px] text-slate-500 dark:text-slate-400">抓取深度</Text>
                    <Input
                      value={String(st().depth)}
                      onChange={({ value }) => setDownloadField("depth", value)}
                    />
                  </View>
                  <View class="min-w-0 gap-1">
                    <Text class="text-[11px] text-slate-500 dark:text-slate-400">最小(KB)</Text>
                    <Input
                      value={String(st().minKB)}
                      onChange={({ value }) => setDownloadField("minKB", value)}
                    />
                  </View>
                  <View class="min-w-0 gap-1">
                    <Text class="text-[11px] text-slate-500 dark:text-slate-400">最大(KB)</Text>
                    <Input
                      value={String(st().maxKB)}
                      onChange={({ value }) => setDownloadField("maxKB", value)}
                    />
                  </View>
                  <View class="min-w-0 gap-1">
                    <Text class="text-[11px] text-slate-500 dark:text-slate-400">最小宽度</Text>
                    <Input
                      value={String(st().minW)}
                      onChange={({ value }) => setDownloadField("minW", value)}
                    />
                  </View>
                  <View class="min-w-0 gap-1">
                    <Text class="text-[11px] text-slate-500 dark:text-slate-400">最小高度</Text>
                    <Input
                      value={String(st().minH)}
                      onChange={({ value }) => setDownloadField("minH", value)}
                    />
                  </View>
                  <View class="min-w-0 gap-1">
                    <Text class="text-[11px] text-slate-500 dark:text-slate-400">最大宽度</Text>
                    <Input
                      value={String(st().maxW)}
                      onChange={({ value }) => setDownloadField("maxW", value)}
                    />
                  </View>
                </View>
              </If>
            </View>
          </Scroll>
        </View>

        <View testID="dl-queue" class="min-w-0 min-h-0 flex-1 bg-white dark:bg-[#131924]">
          <PaneHeader
            title="下载任务队列"
            right={
              <Input
                value={st().search || ""}
                placeholder="搜索任务文件名或链接..."
                onChange={({ value }) => setDownloadField("search", value)}
                class="w-[190px]"
              />
            }
          />
          <View class="shrink-0 border-b border-[#E5E9F0] px-3 py-1.5 dark:border-[#222B3C]">
            <Tabs
              variant="segmented"
              size="small"
              options={filterTabs()}
              value={String(st().filter)}
              onChange={({ value }) => setDownloadFilter(Number(value) || 0)}
            />
          </View>

          <List
            testID="download-task-list"
            class="min-h-0 flex-1"
            contentClass="gap-1.5 p-3"
            each={st().tasks ?? []}
            keyExtractor={(t) => String(t.id)}
            estimatedItemSize={84}
            empty={
              <EmptyNotice
                icon="download"
                title="暂无下载任务"
                subtitle="在左侧粘贴网页/图片直链，或从顶部导入 CSV / XLSX 表格"
              />
            }
          >
            {(task) => (
              <View class="gap-1.5 rounded-md border border-[#E5E9F0] bg-[#F8FAFC] p-2.5 dark:border-[#222B3C] dark:bg-[#1A2232]">
                <View class="flex-row flex-wrap items-center justify-between gap-2">
                  <View class="min-w-0 flex-1 flex-row items-center gap-2">
                    <Checkbox
                      checked={Boolean(task().checked)}
                      onChange={({ checked }) => toggleDownloadCheck(task().id, checked)}
                    />
                    <StatusBadge text={task().phase || "等待"} tone={task().tone} />
                    <Text class="flex-1 truncate text-xs font-medium text-slate-900 dark:text-slate-100">
                      {task().name || task().url || `任务 #${task().id}`}
                    </Text>
                    <If when={Boolean(task().site)}>
                      <OutlineBadge text={task().site} />
                    </If>
                  </View>

                  <View class="flex-row items-center gap-1">
                    <If when={task().phase === "进行中" || task().phase === "下载中"}>
                      <Button
                        text="暂停"
                        size="small"
                        variant="outline"
                        onPress={() => void downloadRowAction(task().id, "pause")}
                      />
                    </If>
                    <If when={task().phase === "已暂停"}>
                      <Button
                        text="继续"
                        size="small"
                        variant="primary"
                        onPress={() => void downloadRowAction(task().id, "resume")}
                      />
                    </If>
                    <If when={task().phase === "失败" || task().phase === "已取消"}>
                      <Button
                        text="重试"
                        size="small"
                        variant="outline"
                        onPress={() => void downloadRowAction(task().id, "retry")}
                      />
                    </If>
                    <If when={Boolean(task().path)}>
                      <Button
                        text="打开"
                        size="small"
                        variant="ghost"
                        onPress={() => void downloadRowAction(task().id, "open")}
                      />
                      <Button
                        text="定位"
                        size="small"
                        variant="ghost"
                        onPress={() => void downloadRowAction(task().id, "reveal")}
                      />
                    </If>
                    <Button
                      text="移除"
                      size="small"
                      variant="ghost"
                      onPress={() => void downloadRowAction(task().id, "remove")}
                    />
                  </View>
                </View>

                <View class="flex-row items-center gap-3 pl-6">
                  <View class="flex-1">
                    <Progress value={Math.min(1, Math.max(0, task().fraction))} />
                  </View>
                  <Text
                    class="text-[11px] text-slate-500 dark:text-slate-400"
                    style={{ fontVariant: ["tabular-nums"] }}
                  >
                    {task().progressText || (task().isPage ? `${task().imagesDone}/${task().imagesTotal} 张` : "")}
                  </Text>
                </View>

                <If when={Boolean(task().err)}>
                  <Text class="pl-6 text-[11px] text-rose-600 dark:text-rose-400">{task().err}</Text>
                </If>
              </View>
            )}
          </List>
        </View>
      </View>

      {/* 表格导入预览弹窗 */}
      <Dialog
        open={Boolean(st().import.open)}
        title={`导入表格 · ${st().import.fileName || ""}`}
        description={`识别到 ${st().import.imageCount} 条链接（忽略无效 ${st().import.invalid} 条）`}
        style={{ width: 580, height: 500 }}
        onClose={() => downloadImportAction("close", "", 0)}
        closeOnEscape
      >
        <View class="min-h-0 w-full min-w-0 flex-1 gap-3">
          <View class="flex-row items-center justify-between gap-2">
            <Checkbox
              text="按列创建子目录"
              checked={Boolean(st().import.folders)}
              onChange={() => downloadImportAction("toggleFolders", "", 0)}
            />
            <Button
              text="确认导入"
              size="small"
              variant="primary"
              disabled={st().import.imageCount === 0}
              onPress={() => downloadImportAction("submit", "", 0)}
            />
          </View>
          <If when={Boolean(st().import.err)}>
            <Text class="text-xs text-rose-600 dark:text-rose-400">{st().import.err}</Text>
          </If>
          <Scroll class="min-h-0 flex-1" contentClass="gap-2 pr-1">
            <Each of={st().import.columns ?? []} key={(c) => String(c.index)}>
              {(col) => (
                <View class="flex-row items-center justify-between gap-2 rounded-lg border border-slate-200 p-2.5 dark:border-[#222B3C] dark:bg-[#1A2232]">
                  <View class="min-w-0 flex-1 gap-0.5">
                    <Text class="text-xs font-medium text-slate-900 dark:text-slate-100">{col().name}</Text>
                    <Text class="truncate text-[11px] text-slate-500">{col().sample || ""}</Text>
                  </View>
                  <View class="flex-row items-center gap-3">
                    <Checkbox
                      text="网址列"
                      checked={Boolean(col().isURL)}
                      onChange={() => downloadImportAction("toggleURL", "", col().index)}
                    />
                    <Checkbox
                      text="命名列"
                      checked={Boolean(col().isName)}
                      onChange={() => downloadImportAction("toggleName", "", col().index)}
                    />
                  </View>
                </View>
              )}
            </Each>
          </Scroll>
        </View>
      </Dialog>

      {/* 站点指南弹窗 */}
      <Dialog
        open={Boolean(st().guideOn)}
        title="支持站点与抓取说明"
        style={{ width: 560, height: 500 }}
        onClose={() => openDownloadGuide(false)}
        closeOnBackdropPress
        closeOnEscape
      >
        <View class="min-h-0 w-full min-w-0 flex-1 gap-2.5">
          <Input
            value={st().guideSearch || ""}
            placeholder="搜索站点名称..."
            onChange={({ value }) => setDownloadField("guideSearch", value)}
          />
          <Scroll class="min-h-0 flex-1" contentClass="gap-2.5 pr-1">
            <Each of={st().guideGroups ?? []} key={(g) => g.title || ""}>
              {(grp) => (
                <View class="gap-1.5">
                  <Text class="text-xs font-semibold text-slate-700 dark:text-slate-200">
                    {grp().title}
                  </Text>
                  <Each of={grp().entries ?? []} key={(e) => e.site || ""}>
                    {(entry) => (
                      <View class="flex-row items-center justify-between rounded border border-slate-200 p-2 dark:border-[#222B3C]">
                        <Text class="text-xs font-medium text-slate-900 dark:text-slate-100">
                          {entry().site}
                        </Text>
                        <Text class="text-[11px] text-slate-500">
                          {[entry().pages, entry().icons].filter(Boolean).join(" · ")}
                        </Text>
                      </View>
                    )}
                  </Each>
                </View>
              )}
            </Each>
          </Scroll>
        </View>
      </Dialog>
    </View>
  );
}
