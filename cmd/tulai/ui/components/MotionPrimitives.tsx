import { Animated, Canvas, cleanup, createAnimatedValue, Text, View } from "nsgo";

export function LivePulseDot(props: {
  color?: "blue" | "emerald" | "amber" | "slate";
  size?: number;
  ping?: boolean;
}) {
  const size = () => props.size ?? 8;
  const dotClass = () => {
    switch (props.color) {
      case "emerald":
        return "bg-emerald-500";
      case "amber":
        return "bg-amber-500";
      case "slate":
        return "bg-slate-400";
      default:
        return "bg-blue-500";
    }
  };
  const ringClass = () => {
    switch (props.color) {
      case "emerald":
        return "bg-emerald-400";
      case "amber":
        return "bg-amber-400";
      case "slate":
        return "bg-slate-400";
      default:
        return "bg-blue-400";
    }
  };

  return (
    <View
      class="relative shrink-0 items-center justify-center"
      style={{ width: size(), height: size() }}
    >
      <If when={props.ping !== false}>
        <View
          class={`rounded-full animate-ping ${ringClass()}`}
          style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, opacity: 0.65 }}
        />
      </If>
      <View
        class={`rounded-full ${dotClass()}`}
        style={{ width: size(), height: size() }}
      />
    </View>
  );
}

export function ActiveScanOverlay(props: {
  tone?: "blue" | "amber";
  compact?: boolean;
  testID?: string;
}) {
  const scan = createAnimatedValue(0);
  const pulse = createAnimatedValue(0.35);

  const scanDur = props.compact ? 1500 : 2000;
  const scanAnim = Animated.loop(
    Animated.sequence([
      Animated.timing(scan, { toValue: 1, duration: scanDur, easing: "easeInOut" }),
      Animated.timing(scan, { toValue: 0, duration: scanDur, easing: "easeInOut" }),
    ]),
  );
  const pulseAnim = Animated.loop(
    Animated.sequence([
      Animated.timing(pulse, { toValue: 0.95, duration: 1200, easing: "easeInOut" }),
      Animated.timing(pulse, { toValue: 0.35, duration: 1200, easing: "easeInOut" }),
    ]),
  );
  scanAnim.start();
  pulseAnim.start();
  cleanup(() => {
    scanAnim.stop();
    pulseAnim.stop();
  });

  const isAmber = () => props.tone === "amber";

  return (
    <View
      testID={props.testID}
      class="pointer-events-none"
      style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, pointerEvents: "none" }}
    >
      <Canvas
        class="pointer-events-none h-full w-full"
        draw={(cx, size) => {
          if (size.width <= 4 || size.height <= 4) return;
          const bandFill = isAmber()
            ? "rgba(245, 158, 11, 0.14)"
            : "rgba(59, 130, 246, 0.14)";
          const lineColor = isAmber() ? "#F59E0B" : "#3B82F6";
          const bracketColor = isAmber()
            ? "rgba(251, 191, 36, 0.72)"
            : "rgba(96, 165, 250, 0.72)";

          const bandH = Math.min(props.compact ? 14 : 34, size.height * 0.28);
          const bandY = scan.mul(Math.max(1, size.height - bandH));
          cx.fillStyle = bandFill;
          cx.fillRect(0, bandY, size.width, bandH);

          const lineH = props.compact ? 1.5 : 2;
          const lineY = scan.mul(Math.max(1, size.height - lineH));
          cx.fillStyle = lineColor;
          cx.fillRect(0, lineY, size.width, lineH);

          const pad = props.compact ? 4 : 8;
          const arm = Math.min(props.compact ? 7 : 13, (size.width - pad * 2) * 0.3);
          if (arm > 2) {
            cx.save();
            cx.globalAlpha = pulse;
            cx.strokeStyle = bracketColor;
            cx.lineWidth = props.compact ? 1.5 : 2;
            cx.lineCap = "round";
            cx.lineJoin = "round";
            cx.beginPath();
            // Top-left
            cx.moveTo(pad, pad + arm);
            cx.lineTo(pad, pad);
            cx.lineTo(pad + arm, pad);
            // Top-right
            cx.moveTo(size.width - pad - arm, pad);
            cx.lineTo(size.width - pad, pad);
            cx.lineTo(size.width - pad, pad + arm);
            // Bottom-left
            cx.moveTo(pad, size.height - pad - arm);
            cx.lineTo(pad, size.height - pad);
            cx.lineTo(pad + arm, size.height - pad);
            // Bottom-right
            cx.moveTo(size.width - pad - arm, size.height - pad);
            cx.lineTo(size.width - pad, size.height - pad);
            cx.lineTo(size.width - pad, size.height - pad - arm);
            cx.stroke();
            cx.restore();
          }
        }}
      />
    </View>
  );
}

function FlowRail(props: {
  flow: ReturnType<typeof createAnimatedValue>;
  moving: boolean;
  tone: "blue" | "emerald";
}) {
  const railColor = props.tone === "emerald"
    ? props.moving
      ? "#10B981"
      : "#CBD5E1"
    : props.moving
      ? "#3B82F6"
      : "#CBD5E1";

  return (
    <Canvas
      class="h-[14px] min-w-[40px] flex-1"
      draw={(cx, size) => {
        if (size.width <= 8 || size.height <= 4) return;
        const midY = size.height / 2;
        cx.save();
        cx.strokeStyle = railColor;
        cx.lineWidth = 1.5;
        cx.lineCap = "round";
        cx.setLineDash([7, 7]);
        if (props.moving) {
          cx.lineDashOffset = props.flow.mul(-14);
        }
        cx.beginPath();
        cx.moveTo(2, midY);
        cx.lineTo(size.width - 2, midY);
        cx.stroke();

        if (props.moving) {
          cx.setLineDash([]);
          cx.lineDashOffset = 0;
          const trackW = Math.max(20, size.width - 10);
          for (const base of [0, 0.33, 0.66]) {
            const x = props.flow.mul(trackW * 0.34).add(5 + trackW * base);
            cx.beginPath();
            cx.moveTo(x.sub(3.5), midY - 3);
            cx.lineTo(x, midY);
            cx.lineTo(x.sub(3.5), midY + 3);
            cx.stroke();
          }
        }
        cx.restore();
      }}
    />
  );
}

export function ConveyorFlowTrack(props: {
  active: boolean;
  activeCount: number;
  pendingCount: number;
  doneCount: number;
}) {
  const flow = createAnimatedValue(0);
  const flowAnim = Animated.loop(
    Animated.timing(flow, { toValue: 1, duration: 1100, easing: "linear" }),
    { resetBeforeIteration: true },
  );
  flowAnim.start();
  cleanup(() => flowAnim.stop());

  const isProcessing = () => props.active || props.activeCount > 0;
  const hasQueuedFlow = () => isProcessing() && props.pendingCount > 0;

  return (
    <View
      testID="wb-conveyor-track"
      class="flex-row items-center gap-2 rounded-lg border border-slate-200/80 bg-white/90 px-2.5 py-1 dark:border-[#1E2738] dark:bg-[#111722]/90"
    >
      {/* 1. Pending queue stage */}
      <View class="shrink-0 flex-row items-center gap-1.5 rounded-full bg-slate-100/90 px-2 py-0.5 dark:bg-[#161E2E]">
        <LivePulseDot color="slate" size={6} ping={props.pendingCount > 0} />
        <Text
          class="text-[11px] font-medium text-slate-600 dark:text-slate-300"
          style={{ fontVariant: ["tabular-nums"] }}
        >
          {`待洗 ${props.pendingCount}`}
        </Text>
      </View>

      <If
        when={hasQueuedFlow()}
        else={<FlowRail flow={flow} moving={false} tone="blue" />}
      >
        <FlowRail flow={flow} moving tone="blue" />
      </If>

      {/* 2. Active station processing stage */}
      <View
        class="shrink-0 flex-row items-center gap-1.5 rounded-full border px-2 py-0.5 transition-colors duration-200"
        classList={{
          "border-blue-200/90 bg-blue-50 dark:border-blue-500/35 dark:bg-blue-500/15": isProcessing(),
          "border-slate-200/80 bg-slate-50 dark:border-[#263248] dark:bg-[#161E2E]": !isProcessing(),
        }}
      >
        <LivePulseDot color="blue" size={6} ping={isProcessing()} />
        <Text
          class="text-[11px] font-semibold"
          classList={{
            "text-blue-700 dark:text-blue-300": isProcessing(),
            "text-slate-600 dark:text-slate-300": !isProcessing(),
          }}
          style={{ fontVariant: ["tabular-nums"] }}
        >
          {`工位加工 ${props.activeCount}`}
        </Text>
      </View>

      <If
        when={isProcessing()}
        else={<FlowRail flow={flow} moving={false} tone="emerald" />}
      >
        <FlowRail flow={flow} moving tone="emerald" />
      </If>

      {/* 3. Completed output stage */}
      <View class="shrink-0 flex-row items-center gap-1.5 rounded-full bg-emerald-50/90 px-2 py-0.5 dark:bg-emerald-500/15">
        <View class="size-1.5 rounded-full bg-emerald-500" />
        <Text
          class="text-[11px] font-medium text-emerald-700 dark:text-emerald-300"
          style={{ fontVariant: ["tabular-nums"] }}
        >
          {`成图 ${props.doneCount}`}
        </Text>
      </View>
    </View>
  );
}
