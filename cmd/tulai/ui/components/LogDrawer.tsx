import { effect, Scroll, Text, View, windowSize, type YNode } from "nsgo";
import { Input } from "nsgo/base";
import { Icon } from "nsgo/ui";
import {
  clearLogs,
  copyText,
  exportLogs,
  setLogFilter,
  toggleLogItem,
} from "tulai/backend";
import { logs, showToast, toggleLogDrawer } from "../store";

interface LevelChipSpec {
  pick: number;
  label: string;
  count: (st: ReturnType<typeof logs>) => number;
  activeBg: string;
  activeBorder: string;
  activeText: string;
}

const LEVEL_CHIPS: LevelChipSpec[] = [
  {
    pick: 0,
    label: "全部",
    count: (st) => st.countAll,
    activeBg: "#F1F5F9",
    activeBorder: "#F1F5F9",
    activeText: "#090D16",
  },
  {
    pick: 1,
    label: "调试",
    count: (st) => st.countDebug,
    activeBg: "#242D3F",
    activeBorder: "#475569",
    activeText: "#F1F5F9",
  },
  {
    pick: 2,
    label: "信息",
    count: (st) => st.countInfo,
    activeBg: "#1E3A5F",
    activeBorder: "#3B82F6",
    activeText: "#93C5FD",
  },
  {
    pick: 3,
    label: "警告",
    count: (st) => st.countWarn,
    activeBg: "#451A03",
    activeBorder: "#D97706",
    activeText: "#FCD34D",
  },
  {
    pick: 4,
    label: "错误",
    count: (st) => st.countError,
    activeBg: "#450A0A",
    activeBorder: "#DC2626",
    activeText: "#FCA5A5",
  },
];

function levelTone(lvl?: string): { fg: string; bg: string; border: string } {
  switch (lvl) {
    case "DBG":
    case "DEBUG":
      return { fg: "#CBD5E1", bg: "#1B2230", border: "#334155" };
    case "WRN":
    case "WARN":
      return { fg: "#FCD34D", bg: "#451A03", border: "#D97706" };
    case "ERR":
    case "ERROR":
      return { fg: "#FCA5A5", bg: "#450A0A", border: "#DC2626" };
    case "OK":
      return { fg: "#86EFAC", bg: "#052E16", border: "#16A34A" };
    default:
      return { fg: "#93C5FD", bg: "#1E3A5F", border: "#3B82F6" };
  }
}

function LogToolButton(props: {
  text: string;
  accessibilityLabel?: string;
  testID?: string;
  onPress: () => void;
}) {
  return (
    <View
      role="button"
      accessibilityLabel={props.accessibilityLabel || props.text}
      testID={props.testID}
      onPress={props.onPress}
      class="h-6 items-center justify-center rounded-[5px] border border-[#252F42] bg-[#1B2230] px-2 hover:border-[#34425A] hover:bg-[#242D3F]"
    >
      <Text class="text-[11.5px] text-[#CBD5E1]">{props.text}</Text>
    </View>
  );
}

export function LogDrawer() {
  const st = logs;
  let scrollRef: YNode | undefined;

  const panelRect = () => {
    const ws = windowSize();
    const winW = ws.width || 1280;
    const winH = ws.height || 820;
    const width = Math.min(880, Math.max(320, winW - 24));
    const height = Math.min(520, Math.max(260, winH - 54));
    const left = Math.max(12, winW - width - 12);
    const top = 46;
    return { left, top, width, height };
  };

  effect(() => {
    const s = st();
    if (s.open && !s.paused && (s.entries ?? []).length > 0) {
      scrollRef?.scrollToEnd?.({ animated: false });
    }
  });

  const cycleSource = () => {
    const s = st();
    const options = ["", ...(s.sources ?? [])];
    const idx = options.indexOf(s.source || "");
    const next = options[(idx + 1) % options.length] ?? "";
    setLogFilter(s.pick, s.query || "", next, Boolean(s.paused));
  };

  const copyAllShown = () => {
    const lines = (st().entries ?? []).map(
      (e) =>
        `[${e.time || ""}] ${e.level || ""} ${e.source || ""} ${e.text || ""}${e.detail ? `\n${e.detail}` : ""}`,
    );
    copyText(lines.join("\n"));
    showToast("已复制当前日志", "success");
  };

  const hasFilter = () => Boolean(st().query?.trim() || st().source || st().pick !== 0);

  return (
    <If when={Boolean(st().open)}>
      <View
        testID="log-drawer"
        accessibilityLabel="运行日志区"
        style={{
          position: "absolute",
          left: panelRect().left,
          top: panelRect().top,
          width: panelRect().width,
          height: panelRect().height,
          zIndex: 40,
          boxShadow: [{ x: 0, y: 16, blur: 40, spread: 0, color: "rgba(0,0,0,0.50)" }],
        }}
        class="overflow-hidden rounded-lg border border-[#252F42] bg-[#0B0E14]"
      >
        {/* Top Chrome: Header + Level Chips + Search & Source */}
        <View class="gap-2 border-b border-[#252F42] bg-[#0F131B] px-3.5 py-2.5">
          {/* Header Row */}
          <View class="flex-row items-center justify-between gap-2">
            <View class="flex-row items-center gap-2">
              <Icon name="terminal" size={13} color="#94A3B8" />
              <Text class="text-[13.5px] font-semibold text-[#F1F5F9]">运行日志</Text>
              <View class="h-5 flex-row items-center gap-1.5 rounded-full border border-[#252F42] bg-[#141923] px-2">
                <View
                  class="size-1.5 rounded-full"
                  classList={{
                    "bg-[#34D399]": !st().paused,
                    "bg-[#94A3B8]": Boolean(st().paused),
                  }}
                />
                <Text class="text-[11px] text-[#CBD5E1]">
                  {st().paused ? "已暂停" : "实时接收"}
                </Text>
              </View>
            </View>

            <View class="flex-row items-center gap-1.5">
              <LogToolButton
                text={st().paused ? "继续跟随" : "暂停跟随"}
                onPress={() =>
                  setLogFilter(st().pick, st().query || "", st().source || "", !st().paused)
                }
              />
              <LogToolButton text="导出" onPress={() => void exportLogs()} />
              <LogToolButton text="复制" onPress={copyAllShown} />
              <LogToolButton text="清空" onPress={() => clearLogs()} />
              <LogToolButton
                text="关闭"
                accessibilityLabel="关闭运行日志"
                testID="log-close-btn"
                onPress={() => toggleLogDrawer(false)}
              />
            </View>
          </View>

          {/* Level Chips */}
          <View class="flex-row flex-wrap items-center gap-1.5">
            <Text class="text-[11.5px] text-[#94A3B8]">级别</Text>
            <Each of={LEVEL_CHIPS} key={(c) => String(c.pick)}>
              {(chip) => {
                const on = () => st().pick === chip().pick;
                return (
                  <View
                    role="button"
                    accessibilityLabel={`${chip().label} ${chip().count(st())}`}
                    onPress={() =>
                      setLogFilter(
                        chip().pick,
                        st().query || "",
                        st().source || "",
                        Boolean(st().paused),
                      )
                    }
                    class="h-[22px] items-center justify-center rounded-[5px] border px-2"
                    classList={{
                      "border-[#252F42] bg-[#141923] hover:border-[#34425A] hover:bg-[#1B2230]":
                        !on(),
                    }}
                    style={{
                      backgroundColor: on() ? chip().activeBg : "#141923",
                      borderColor: on() ? chip().activeBorder : "#252F42",
                    }}
                  >
                    <Text
                      class="text-[11.5px]"
                      classList={{
                        "font-semibold": on(),
                      }}
                      style={{ color: on() ? chip().activeText : "#CBD5E1" }}
                    >
                      {`${chip().label} ${chip().count(st())}`}
                    </Text>
                  </View>
                );
              }}
            </Each>
          </View>

          {/* Search + Source Filter */}
          <View class="flex-row items-center gap-2">
            <Input
              value={st().query || ""}
              placeholder="搜索事件或字段"
              accessibilityLabel="搜索事件或字段"
              onChange={({ value }) =>
                setLogFilter(st().pick, value, st().source || "", Boolean(st().paused))
              }
              class="min-w-[120px] flex-1"
              style={{
                height: 26,
                backgroundColor: "#141923",
                borderWidth: 1,
                borderColor: "#252F42",
                borderRadius: 5,
                paddingHorizontal: 10,
                fontSize: 12,
                color: "#F1F5F9",
                placeholderColor: "#64748B",
                caretColor: "#3B82F6",
              }}
            />
            <LogToolButton
              text={st().source ? st().source! : "全部来源"}
              onPress={cycleSource}
            />
          </View>
        </View>

        {/* Dense Console Rows */}
        <View class="min-h-0 flex-1 bg-[#0B0E14] px-2.5 py-1.5">
          <Scroll
            ref={(el) => {
              scrollRef = el;
            }}
            class="min-h-0 flex-1"
          >
            <If
              when={(st().entries ?? []).length > 0}
              else={
                <View class="items-center justify-center py-8">
                  <Text class="text-xs text-[#94A3B8]">
                    {hasFilter()
                      ? "没有匹配的日志"
                      : "每个任务的开始、提交和结束会写在这里"}
                  </Text>
                </View>
              }
            >
              <Each of={st().entries ?? []} key={(e) => String(e.seq)}>
                {(entry) => {
                  const tone = () => levelTone(entry().level);
                  return (
                    <View
                      onPress={() => toggleLogItem(entry().seq)}
                      class="rounded px-1.5 hover:bg-[#141923]"
                      classList={{
                        "bg-[#141923]": Boolean(entry().expanded),
                      }}
                    >
                      <View class="h-[22px] flex-row items-center gap-2">
                        <Text class="w-[84px] shrink-0 text-[11.5px] text-[#94A3B8]">
                          {entry().time || ""}
                        </Text>
                        <View
                          class="h-4 w-9 shrink-0 items-center justify-center rounded border"
                          style={{
                            backgroundColor: tone().bg,
                            borderColor: tone().border,
                          }}
                        >
                          <Text
                            class="text-[10px] font-bold"
                            style={{ color: tone().fg }}
                          >
                            {entry().level || "INF"}
                          </Text>
                        </View>
                        <Text class="w-[104px] shrink-0 truncate text-[11.5px] text-[#7EB6FF]">
                          {entry().source ? `[${entry().source}]` : ""}
                        </Text>
                        <Text class="min-w-0 flex-1 truncate text-xs text-[#F1F5F9]">
                          {entry().text || ""}
                        </Text>
                        <If when={Boolean(entry().elapsed)}>
                          <Text class="w-[60px] shrink-0 text-right text-[11.5px] text-[#93C5FD]">
                            {entry().elapsed}
                          </Text>
                        </If>
                      </View>
                      <If when={Boolean(entry().expanded && entry().detail)}>
                        <Text class="px-2 pb-1 text-[11px] text-[#94A3B8]">
                          {entry().detail}
                        </Text>
                      </If>
                    </View>
                  );
                }}
              </Each>
            </If>
          </Scroll>
        </View>

        {/* Footer */}
        <View class="flex-row items-center justify-between gap-2.5 border-t border-[#252F42] bg-[#0F131B] px-3.5 py-2">
          <View class="flex-row items-center gap-2.5">
            <View
              class="size-[7px] rounded-full"
              classList={{
                "bg-[#34D399]": !st().paused,
                "bg-[#94A3B8]": Boolean(st().paused),
              }}
            />
            <Text class="text-[11.5px] text-[#CBD5E1]">
              {st().paused ? "已暂停跟随" : "实时跟随"}
            </Text>
            <Text class="text-[11.5px] text-[#CBD5E1]">{`共 ${st().total} 条`}</Text>
            <Text class="text-[11.5px] text-[#94A3B8]">{`显示 ${st().shown} 条`}</Text>
          </View>
          <If when={Boolean(st().lastAt)}>
            <Text class="text-[11.5px] text-[#94A3B8]">{`最近收到 ${st().lastAt}`}</Text>
          </If>
        </View>
      </View>
    </If>
  );
}
