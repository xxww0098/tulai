import {
  cleanup,
  Image,
  Overlay,
  signal,
  Text,
  View,
  type JSX,
  type StyleProp,
  type ViewStyle,
  type YNode,
} from "nsgo";
import { Badge } from "nsgo/ui";

export interface HoverPreviewItem {
  key: string;
  anchor: YNode;
  src: string;
  title: string;
  subtitle?: string;
  badge?: string;
  badgeVariant?: "accent" | "success" | "warn" | "danger" | "neutral";
}

const hoverItem = signal<HoverPreviewItem | null>(null);
let openTimer: ReturnType<typeof setTimeout> | undefined;
let closeTimer: ReturnType<typeof setTimeout> | undefined;

function cancelTimers() {
  if (openTimer !== undefined) {
    clearTimeout(openTimer);
    openTimer = undefined;
  }
  if (closeTimer !== undefined) {
    clearTimeout(closeTimer);
    closeTimer = undefined;
  }
}

export function clearHoverPreview() {
  cancelTimers();
  if (hoverItem() !== null) hoverItem.set(null);
}

function scheduleOpen(next: HoverPreviewItem) {
  cancelTimers();
  const delay = hoverItem() ? 0 : 140;
  if (delay === 0) {
    hoverItem.set(next);
    return;
  }
  openTimer = setTimeout(() => {
    openTimer = undefined;
    hoverItem.set(next);
  }, delay);
}

function scheduleClose(key: string) {
  if (openTimer !== undefined) {
    clearTimeout(openTimer);
    openTimer = undefined;
  }
  if (hoverItem()?.key !== key) return;
  if (closeTimer !== undefined) clearTimeout(closeTimer);
  closeTimer = setTimeout(() => {
    closeTimer = undefined;
    if (hoverItem()?.key === key) hoverItem.set(null);
  }, 80);
}

export function HoverPreviewTarget(props: {
  previewKey: string;
  src?: string;
  title: string;
  subtitle?: string;
  badge?: string;
  badgeVariant?: "accent" | "success" | "warn" | "danger" | "neutral";
  class?: string;
  style?: StyleProp<ViewStyle>;
  onMouseEnterExtra?: () => void;
  onMouseLeaveExtra?: () => void;
  children?: JSX.Element;
}) {
  let anchorNode: YNode | undefined;

  cleanup(() => {
    if (hoverItem()?.key === props.previewKey) {
      hoverItem.set(null);
    }
  });

  return (
    <View
      ref={(node) => {
        anchorNode = node;
      }}
      class={props.class}
      style={props.style}
      onMouseEnter={() => {
        props.onMouseEnterExtra?.();
        if (!props.src || !anchorNode) return;
        scheduleOpen({
          key: props.previewKey,
          anchor: anchorNode,
          src: props.src,
          title: props.title,
          subtitle: props.subtitle,
          badge: props.badge,
          badgeVariant: props.badgeVariant,
        });
      }}
      onMouseLeave={() => {
        props.onMouseLeaveExtra?.();
        scheduleClose(props.previewKey);
      }}
      onPress={() => {
        clearHoverPreview();
      }}
    >
      {props.children}
    </View>
  );
}

export function HoverPreviewHost() {
  return (
    <Each of={hoverItem() ? [hoverItem()!] : []} key={(p) => p.key}>
      {(item) => (
        <Overlay
          testID="hover-preview-card"
          anchor={item().anchor}
          placement="right"
          align="center"
          offset={10}
          margin={12}
          pointerEvents="none"
        >
          <View
            class="w-[280px] overflow-hidden rounded-lg border border-slate-200 bg-white dark:border-[#334155] dark:bg-[#131924]"
            style={{
              boxShadow: [{ x: 0, y: 12, blur: 32, spread: 0, color: "rgba(15,23,42,0.22)" }],
            }}
          >
            <View class="h-[220px] w-full items-center justify-center bg-slate-50 p-2 dark:bg-[#0B0F17]">
              <Image
                src={item().src}
                fadeDuration={100}
                class="h-full w-full object-contain"
              />
            </View>
            <View class="gap-1 border-t border-[#E5E9F0] px-2.5 py-2 dark:border-[#222B3C]">
              <View class="flex-row items-center justify-between gap-2">
                <Text class="min-w-0 flex-1 truncate text-xs font-semibold text-slate-900 dark:text-slate-100">
                  {item().title}
                </Text>
                <If when={Boolean(item().badge)}>
                  <Badge text={item().badge!} variant={item().badgeVariant ?? "neutral"} />
                </If>
              </View>
              <If when={Boolean(item().subtitle)}>
                <Text
                  class="truncate text-[11px] text-slate-500 dark:text-slate-400"
                  style={{ fontVariant: ["tabular-nums"] }}
                >
                  {item().subtitle}
                </Text>
              </If>
            </View>
          </View>
        </Overlay>
      )}
    </Each>
  );
}
