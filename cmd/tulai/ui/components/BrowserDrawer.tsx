import { cleanup, computed, keymap, PanResponder, signal, Text, View, windowSize, type JSX, type Point, type YNode } from "nsgo";
import { Badge, Button } from "nsgo/ui";
import {
  clearBrowserDrawerCopy,
  closeBrowserDrawer,
  copyText,
  openBrowserDrawer,
  resumeBrowserDrawer,
} from "tulai/backend";
import { BrowserStage, OutlineBadge } from "tulai/native";
import { browserDrawer, showToast, viewedAccountID } from "../store";

const accountListLeft = signal(0);

export function AccountListFrame(props: { class: string; children?: JSX.Element }) {
  let list: YNode | undefined;
  let mounted = true;
  cleanup(() => { mounted = false; });
  return (
    <View
      ref={(node) => { list = node; }}
      class={props.class}
      onLayout={() => {
        void list?.measure().then((layout) => {
          if (mounted && layout) accountListLeft.set(layout.pagePosition.x);
        });
      }}
    >
      {props.children}
    </View>
  );
}

export function AccountViewButton(props: { accountID: number; name?: string }) {
  return (
    <Button
      text="可视"
      icon="eye"
      size="small"
      variant={viewedAccountID() === props.accountID ? "primary" : "outline"}
      accessibilityLabel={`查看浏览器 · ${props.name || `账号 #${props.accountID}`}`}
      onPress={() => openBrowserDrawer(props.accountID)}
    />
  );
}

export function BrowserDrawer() {
  const st = browserDrawer;
  keymap.bind("escape", () => closeBrowserDrawer());
  const accountName = () => st().name || (st().accountID ? `账号 #${st().accountID}` : "账号浏览器");
  const position = signal<Point>();
  const frame = computed(() => {
    const { width: w, height: h } = windowSize();
    const gap = 12;
    const targetWidth = Math.min(840, w * 0.58);
    // Leave the account buttons and names at the start of the list exposed.
    const listLeft = accountListLeft();
    const roomLeft = listLeft - 2 * gap;
    const roomRight = w - listLeft - 220 - gap;
    const placeRight = roomRight >= targetWidth * 0.8 || roomRight >= roomLeft;
    const width = Math.max(1, Math.min(targetWidth, placeRight ? roomRight : roomLeft));
    const height = Math.max(1, Math.min(720, h * 0.84));
    const x = position()?.x ?? (placeRight ? w - width - gap : gap);
    const y = position()?.y ?? (h - height) / 2;
    return {
      left: Math.max(gap, Math.min(x, w - width - gap)),
      top: Math.max(gap, Math.min(y, h - height - gap)),
      width, height,
    };
  });
  let dragStart = { x: 0, y: 0 };
  const drag = PanResponder.create({
    onStartShouldSetPanResponder: () => true,
    onPanResponderGrant: () => { dragStart = { x: frame().left, y: frame().top }; },
    onPanResponderMove: (_, g) => position.set({ x: dragStart.x + g.dx, y: dragStart.y + g.dy }),
  });

  return (
    <View
      role="dialog"
      accessibilityLabel={`浏览器 · ${accountName()}`}
      // The page handles pointer down/up itself. Consume the separate click inside the window.
      onPress={() => {}}
      style={{
        position: "absolute", ...frame(), zIndex: 45,
        boxShadow: [{ x: 0, y: 8, blur: 28, spread: 0, color: "rgba(15,23,42,0.28)" }],
      }}
      class="overflow-hidden rounded-xl border border-slate-200 bg-white dark:border-[#334155] dark:bg-[#131924]"
      testID="browser-drawer-dialog"
    >
      <View class="flex-row flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-3.5 py-2.5 dark:border-[#222B3C]">
        <View testID="browser-drawer-handle" class="min-w-0 flex-1 gap-1" style={{ cursor: "grab" }} {...drag.panHandlers}>
          <View class="flex-row flex-wrap items-center gap-2">
            <Text class="truncate text-sm font-semibold text-slate-900 dark:text-slate-100">{accountName()}</Text>
            <If when={Boolean(st().platform)}>
              <OutlineBadge text={st().platform} />
            </If>
            <If when={Boolean(st().manual && !st().err && !st().loading)}>
              <Badge text="手动操作" variant="warn" />
            </If>
          </View>
          <If when={Boolean(st().title || st().url)}>
            <Text class="truncate text-xs text-slate-500 dark:text-slate-400">
              {[st().title, st().url].filter(Boolean).join(" · ")}
            </Text>
          </If>
        </View>

        <View class="flex-row flex-wrap items-center gap-2">
          <If when={Boolean(st().copyReady && st().copyText)}>
            <Button
              text="复制捕获内容"
              size="small"
              icon="copy"
              variant="outline"
              onPress={() => {
                copyText(st().copyText || "");
                clearBrowserDrawerCopy();
                showToast("已复制到剪贴板", "success");
              }}
            />
          </If>
          <If when={Boolean((st().manual || st().resuming) && !st().loading && !st().err)}>
            <Button
              text={st().resuming ? "正在恢复任务..." : "我已完成，继续任务"}
              size="small"
              variant="primary"
              busy={Boolean(st().resuming)}
              onPress={() => resumeBrowserDrawer()}
            />
          </If>
          <Button text="关闭" size="small" variant="ghost" onPress={() => closeBrowserDrawer()} />
        </View>
      </View>
      <If when={Boolean(st().loading || st().err)}>
        <View class="px-3.5 py-2">
          <Text
            class="text-xs"
            classList={{
              "text-slate-500 dark:text-slate-400": !st().err,
              "text-rose-600 dark:text-rose-400": Boolean(st().err),
            }}
          >
            {st().err || "正在加载账号页面…"}
          </Text>
        </View>
      </If>
      <BrowserStage testID="browser-drawer-stage" class="min-h-0 w-full flex-1 bg-slate-950" />
    </View>
  );
}
