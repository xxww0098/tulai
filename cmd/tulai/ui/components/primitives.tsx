import type { JSX } from "nsgo";
import { Text, View } from "nsgo";
import { Badge, Icon } from "nsgo/ui";

export function PageToolbar(props: {
  left?: JSX.Element;
  right?: JSX.Element;
  bottom?: JSX.Element;
  class?: string;
}) {
  return (
    <View
      class={`border-b border-slate-200/80 bg-white px-3 py-1.5 dark:border-[#1E2738] dark:bg-[#0F141F] ${props.class ?? ""}`}
    >
      <View class="gap-1.5">
        <View class="flex-row items-center justify-between gap-2">
          <View class="min-w-0 flex-1 flex-row items-center gap-2">{props.left}</View>
          <View class="shrink-0 flex-row items-center gap-1.5">{props.right}</View>
        </View>
        {props.bottom}
      </View>
    </View>
  );
}

export function FormRow(props: {
  label: string;
  hint?: string;
  class?: string;
  children?: JSX.Element;
}) {
  return (
    <View class={`gap-1.5 md:flex-row md:items-center md:gap-4 ${props.class ?? ""}`}>
      <View class="md:w-[152px] gap-0.5">
        <Text class="text-xs font-medium text-slate-700 dark:text-slate-200">{props.label}</Text>
        <If when={Boolean(props.hint)}>
          <Text class="text-[11px] text-slate-500 dark:text-slate-400">{props.hint}</Text>
        </If>
      </View>
      <View class="min-w-0 flex-1">{props.children}</View>
    </View>
  );
}

export function SectionCard(props: {
  title?: string;
  subtitle?: string;
  right?: JSX.Element;
  compact?: boolean;
  class?: string;
  children?: JSX.Element;
}) {
  // TSX slots are getters. Checking their presence must not build their contents.
  const hasBody = "children" in props;
  return (
    <View
      class={`rounded-xl border border-slate-200/80 bg-white dark:border-[#1E2738] dark:bg-[#111722] ${props.compact ? "p-2.5" : "p-3"} ${props.class ?? ""}`}
    >
      <If when={Boolean(props.title || props.subtitle || "right" in props)}>
        <View
          class="flex-row items-center justify-between gap-2"
          classList={{
            "mb-2": Boolean(props.compact && hasBody),
            "mb-2.5": !props.compact && hasBody,
          }}
        >
          <View class="min-w-0 flex-1 gap-0.5">
            <If when={Boolean(props.title)}>
              <Text class="text-[13px] font-semibold text-slate-900 dark:text-slate-100">{props.title}</Text>
            </If>
            <If when={Boolean(props.subtitle)}>
              <Text class="text-[11px] text-slate-500 dark:text-slate-400">{props.subtitle}</Text>
            </If>
          </View>
          {props.right}
        </View>
      </If>
      {props.children}
    </View>
  );
}

export function PillToggle(props: {
  label: string;
  active: boolean;
  onPress: () => void;
  testID?: string;
}) {
  return (
    <View
      testID={props.testID}
      onPress={props.onPress}
      style={{ cursor: "pointer" }}
      class="flex-row items-center gap-1.5 rounded-lg border px-2.5 py-1.5"
      classList={{
        "border-blue-500/40 bg-blue-50/90 dark:border-blue-400/50 dark:bg-blue-500/15": props.active,
        "border-slate-200/80 bg-white hover:bg-slate-50 dark:border-[#1E2738] dark:bg-[#161E2E] dark:hover:bg-[#1C2639]":
          !props.active,
      }}
    >
      <Text
        class="text-xs font-medium"
        classList={{
          "text-blue-700 dark:text-blue-300": props.active,
          "text-slate-600 dark:text-slate-300": !props.active,
        }}
      >
        {props.label}
      </Text>
    </View>
  );
}

export function EmptyNotice(props: { icon: string; title: string; subtitle?: string; action?: JSX.Element }) {
  return (
    <View class="items-center justify-center gap-2 rounded-xl border border-dashed border-slate-200/90 bg-white/60 px-4 py-6 dark:border-[#1E2738] dark:bg-[#111722]/60">
      <View class="size-10 items-center justify-center rounded-full bg-blue-50 dark:bg-blue-500/10">
        <Icon name={props.icon} size={18} color="#3B82F6" />
      </View>
      <Text class="text-xs font-semibold text-slate-700 dark:text-slate-200">{props.title}</Text>
      <If when={Boolean(props.subtitle)}>
        <Text class="max-w-[300px] text-center text-[11px] text-slate-500 dark:text-slate-400">
          {props.subtitle}
        </Text>
      </If>
      {props.action}
    </View>
  );
}

export function FieldLabel(props: { label: string; hint?: string; class?: string }) {
  return (
    <View class={`gap-0.5 ${props.class ?? ""}`}>
      <Text class="text-xs font-medium text-slate-700 dark:text-slate-200">{props.label}</Text>
      <If when={Boolean(props.hint)}>
        <Text class="text-[11px] text-slate-500 dark:text-slate-400">{props.hint}</Text>
      </If>
    </View>
  );
}

export function toneVariant(tone?: string): "accent" | "danger" | "neutral" | "success" | "warn" {
  switch (tone) {
    case "ok":
    case "ready":
    case "success":
    case "up":
      return "success";
    case "warn":
    case "warning":
    case "cooling":
    case "verify":
      return "warn";
    case "danger":
    case "error":
    case "bad":
    case "failed":
      return "danger";
    case "info":
    case "running":
    case "busy":
    case "accent":
      return "accent";
    default:
      return "neutral";
  }
}

export function StatusBadge(props: { text?: string; tone?: string }) {
  return <Badge text={props.text || "-"} variant={toneVariant(props.tone)} />;
}
