import { Animated, Canvas, cleanup, createAnimatedValue, effect, Image, reduceMotion, signal, Text, View } from "nsgo";
import { Badge, Button, Icon } from "nsgo/ui";
import { type ResultTileDTO, openBrowserDrawer } from "tulai/backend";
import { shell } from "../store";
import { HoverPreviewTarget } from "./HoverPreview";
import { LivePulseDot } from "./MotionPrimitives";

export type WashBotMode = "working" | "celebrate" | "alert" | "paused";

/**
 * 「小白洗图君」 ceramic-capsule anthropomorphic character drawn cleanly on Canvas
 * with Light porcelain / Dark titanium contrast, expressive faces, and front-load wave drum.
 */
export function WashBotAvatar(props: {
  mode: WashBotMode;
  testID?: string;
}) {
  const bob = createAnimatedValue(0);
  const cycle = createAnimatedValue(0);
  const wave = createAnimatedValue(0);
  const pulse = createAnimatedValue(0.35);

  const bobAnim = Animated.loop(
    Animated.sequence([
      Animated.timing(bob, { toValue: 1, duration: 1050, easing: "easeInOut" }),
      Animated.timing(bob, { toValue: 0, duration: 1050, easing: "easeInOut" }),
    ]),
  );
  const cycleAnim = Animated.loop(
    Animated.timing(cycle, { toValue: 1, duration: 4200, easing: "linear" }),
    { resetBeforeIteration: true },
  );
  const waveAnim = Animated.loop(
    Animated.timing(wave, { toValue: 1, duration: 1500, easing: "linear" }),
    { resetBeforeIteration: true },
  );
  const pulseAnim = Animated.loop(
    Animated.sequence([
      Animated.timing(pulse, { toValue: 1, duration: 700, easing: "easeInOut" }),
      Animated.timing(pulse, { toValue: 0.35, duration: 700, easing: "easeInOut" }),
    ]),
  );

  effect(() => {
    const active = (props.mode === "working" || props.mode === "celebrate") && !reduceMotion();
    if (active) {
      bobAnim.start();
      cycleAnim.start();
      waveAnim.start();
      pulseAnim.start();
    } else {
      bobAnim.stop();
      cycleAnim.stop();
      waveAnim.stop();
      pulseAnim.stop();
      bob.set(0);
      cycle.set(0.22);
      wave.set(0.25);
      pulse.set(props.mode === "alert" ? 0.9 : 0.4);
    }
  });

  cleanup(() => {
    bobAnim.stop();
    cycleAnim.stop();
    waveAnim.stop();
    pulseAnim.stop();
  });

  const isDark = () => Boolean(shell().dark);

  return (
    <View testID={props.testID} class="h-[60px] w-[84px] shrink-0 items-center justify-center">
      <Canvas
        class="h-full w-full"
        draw={(cx, size) => {
          if (size.width <= 8 || size.height <= 8) return;
          const m = props.mode;
          const dark = isDark();
          const scale = Math.min(size.width / 88, size.height / 64);

          const beaconColor =
            m === "alert" ? "#FBBF24" : m === "celebrate" ? "#34D399" : m === "paused" ? "#94A3B8" : "#38BDF8";
          const bodyStroke =
            m === "alert"
              ? "#F59E0B"
              : m === "celebrate"
                ? "#10B981"
                : dark
                  ? "#64748B"
                  : "#CBD5E1";
          const earFill = dark ? "#475569" : "#E2E8F0";
          const earStroke = dark ? "#64748B" : "#94A3B8";
          const bezelFill = dark ? "#1E293B" : "#F1F5F9";
          const bezelStroke = dark ? "#64748B" : "#CBD5E1";

          cx.save();
          cx.scale(scale, scale);

          // Ground shadow pulsing opposite to chassis bob
          cx.save();
          cx.translate(44, 60.5);
          cx.scale(bob.interpolate({ inputRange: [0, 1], outputRange: [1, 0.88] }), 1);
          cx.globalAlpha = bob.interpolate({ inputRange: [0, 1], outputRange: [0.22, 0.13] });
          cx.fillStyle = "#0F172A";
          cx.beginPath();
          cx.ellipse(0, 0, 18, 2.6, 0, 0, Math.PI * 2, false);
          cx.fill();
          cx.restore();

          // Chassis group with vertical bob
          cx.save();
          cx.translate(0, bob.interpolate({ inputRange: [0, 1], outputRange: [0, -2] }));

          const bodyGrad = cx.createLinearGradient(22, 10, 66, 57);
          if (dark) {
            bodyGrad.addColorStop(0, "#475569");
            bodyGrad.addColorStop(0.55, "#334155");
            bodyGrad.addColorStop(1, "#1E293B");
          } else {
            bodyGrad.addColorStop(0, "#FFFFFF");
            bodyGrad.addColorStop(0.6, "#F8FAFC");
            bodyGrad.addColorStop(1, "#E2E8F0");
          }

          // Curved spring antenna + glowing pearl + white glint
          cx.strokeStyle = earStroke;
          cx.lineWidth = 1.8;
          cx.lineCap = "round";
          cx.beginPath();
          cx.moveTo(44, 10.5);
          cx.quadraticCurveTo(44, 6.8, 47.2, 4.8);
          cx.stroke();

          cx.save();
          cx.globalAlpha = pulse.mul(0.28);
          cx.fillStyle = beaconColor;
          cx.beginPath();
          cx.arc(47.8, 4.6, 4.2, 0, Math.PI * 2, false);
          cx.fill();
          cx.restore();

          cx.fillStyle = beaconColor;
          cx.beginPath();
          cx.arc(47.8, 4.6, 2.5, 0, Math.PI * 2, false);
          cx.fill();
          cx.fillStyle = "#FFFFFF";
          cx.beginPath();
          cx.arc(47, 3.8, 0.8, 0, Math.PI * 2, false);
          cx.fill();

          // Left stubby capsule arm
          cx.save();
          cx.translate(22.5, 36);
          cx.rotate(bob.interpolate({ inputRange: [0, 1], outputRange: [0.24, 0.05] }));
          cx.fillStyle = dark ? "#334155" : "#F8FAFC";
          cx.strokeStyle = bodyStroke;
          cx.lineWidth = 1.1;
          cx.beginPath();
          cx.roundRect(-6.2, -1.5, 6.2, 10.5, 3.1);
          cx.fill();
          cx.stroke();
          cx.restore();

          // Right stubby capsule arm
          cx.save();
          cx.translate(65.5, 36);
          cx.rotate(bob.interpolate({ inputRange: [0, 1], outputRange: [-0.24, -0.05] }));
          cx.fillStyle = dark ? "#334155" : "#F8FAFC";
          cx.strokeStyle = bodyStroke;
          cx.lineWidth = 1.1;
          cx.beginPath();
          cx.roundRect(0, -1.5, 6.2, 10.5, 3.1);
          cx.fill();
          cx.stroke();
          cx.restore();

          // Rounded headphone ear-pods
          cx.fillStyle = earFill;
          cx.strokeStyle = earStroke;
          cx.lineWidth = 1;
          cx.beginPath();
          cx.roundRect(19, 16.5, 6.2, 10.5, 3.1);
          cx.fill();
          cx.stroke();
          cx.beginPath();
          cx.roundRect(62.8, 16.5, 6.2, 10.5, 3.1);
          cx.fill();
          cx.stroke();

          cx.fillStyle = beaconColor;
          cx.beginPath();
          cx.roundRect(20.7, 19.2, 1.6, 5, 0.8);
          cx.roundRect(65.7, 19.2, 1.6, 5, 0.8);
          cx.fill();

          // Chubby ceramic capsule body
          cx.fillStyle = bodyGrad;
          cx.strokeStyle = bodyStroke;
          cx.lineWidth = 1.25;
          cx.beginPath();
          cx.roundRect(22, 10, 44, 47, 18.5);
          cx.fill();
          cx.stroke();

          // Top-left specular highlight arc & collar seam
          cx.strokeStyle = dark ? "rgba(255, 255, 255, 0.28)" : "#FFFFFF";
          cx.lineWidth = 1.8;
          cx.lineCap = "round";
          cx.beginPath();
          cx.moveTo(26.5, 21);
          cx.bezierCurveTo(26.5, 14.5, 32.5, 12, 38.5, 12);
          cx.stroke();

          cx.strokeStyle = dark ? "rgba(148, 163, 184, 0.28)" : "rgba(148, 163, 184, 0.38)";
          cx.lineWidth = 0.9;
          cx.beginPath();
          cx.moveTo(24, 30.8);
          cx.quadraticCurveTo(44, 32.2, 64, 30.8);
          cx.stroke();

          // Rounded dark visor faceplate
          const visorGrad = cx.createLinearGradient(27, 14, 27, 29);
          visorGrad.addColorStop(0, "#1E293B");
          visorGrad.addColorStop(1, "#0B1120");
          cx.fillStyle = visorGrad;
          cx.strokeStyle = dark ? "#475569" : "#334155";
          cx.lineWidth = 0.9;
          cx.beginPath();
          cx.roundRect(27, 14, 34, 15, 7.5);
          cx.fill();
          cx.stroke();

          cx.strokeStyle = "rgba(255, 255, 255, 0.18)";
          cx.lineWidth = 1.2;
          cx.lineCap = "round";
          cx.beginPath();
          cx.moveTo(30.5, 16.3);
          cx.quadraticCurveTo(37, 15.2, 44.5, 15.8);
          cx.stroke();

          // Pink cheek blush ellipses
          cx.fillStyle = "rgba(253, 164, 175, 0.52)";
          cx.beginPath();
          cx.ellipse(30.8, 24.5, 2.3, 1.2, 0, 0, Math.PI * 2, false);
          cx.ellipse(57.2, 24.5, 2.3, 1.2, 0, 0, Math.PI * 2, false);
          cx.fill();

          // Expressive face per mode
          if (m === "alert") {
            cx.strokeStyle = "#FBBF24";
            cx.lineWidth = 1.9;
            cx.lineCap = "round";
            cx.lineJoin = "round";
            cx.beginPath();
            cx.moveTo(34, 18.5);
            cx.lineTo(38.5, 21);
            cx.lineTo(34, 23.5);
            cx.moveTo(54, 18.5);
            cx.lineTo(49.5, 21);
            cx.lineTo(54, 23.5);
            cx.stroke();

            cx.strokeStyle = "#FCD34D";
            cx.lineWidth = 1.3;
            cx.beginPath();
            cx.moveTo(40.5, 25.2);
            cx.quadraticCurveTo(42.2, 23.8, 44, 25.2);
            cx.quadraticCurveTo(45.8, 26.6, 47.5, 25.2);
            cx.stroke();

            // Cute sky-blue sweat-drop on temple
            cx.fillStyle = "#38BDF8";
            cx.strokeStyle = "#0284C7";
            cx.lineWidth = 0.6;
            cx.beginPath();
            cx.moveTo(62, 11.5);
            cx.bezierCurveTo(63.6, 13.6, 64.2, 15.2, 63.6, 16.5);
            cx.bezierCurveTo(63, 17.6, 61, 17.6, 60.4, 16.5);
            cx.bezierCurveTo(59.8, 15.2, 60.4, 13.6, 62, 11.5);
            cx.fill();
            cx.stroke();
            cx.fillStyle = "rgba(255, 255, 255, 0.85)";
            cx.beginPath();
            cx.arc(61.5, 14.9, 0.65, 0, Math.PI * 2, false);
            cx.fill();
          } else if (m === "celebrate") {
            cx.strokeStyle = "#34D399";
            cx.lineWidth = 2;
            cx.lineCap = "round";
            cx.beginPath();
            cx.moveTo(33.5, 22);
            cx.quadraticCurveTo(36.5, 17.5, 39.5, 22);
            cx.moveTo(48.5, 22);
            cx.quadraticCurveTo(51.5, 17.5, 54.5, 22);
            cx.stroke();

            cx.strokeStyle = "#6EE7B7";
            cx.lineWidth = 1.4;
            cx.beginPath();
            cx.moveTo(41.2, 24.4);
            cx.quadraticCurveTo(44, 27.2, 46.8, 24.4);
            cx.stroke();

            // 4-point star sparkles
            cx.fillStyle = "#34D399";
            cx.beginPath();
            cx.moveTo(23, 11);
            cx.lineTo(23.9, 13.1);
            cx.lineTo(26, 14);
            cx.lineTo(23.9, 14.9);
            cx.lineTo(23, 17);
            cx.lineTo(22.1, 14.9);
            cx.lineTo(20, 14);
            cx.lineTo(22.1, 13.1);
            cx.closePath();
            cx.fill();

            cx.fillStyle = "#FBBF24";
            cx.beginPath();
            cx.moveTo(65.5, 9.5);
            cx.lineTo(66.2, 11.2);
            cx.lineTo(68, 11.9);
            cx.lineTo(66.2, 12.6);
            cx.lineTo(65.5, 14.3);
            cx.lineTo(64.8, 12.6);
            cx.lineTo(63, 11.9);
            cx.lineTo(64.8, 11.2);
            cx.closePath();
            cx.fill();
          } else if (m === "paused") {
            cx.strokeStyle = "#94A3B8";
            cx.lineWidth = 1.8;
            cx.lineCap = "round";
            cx.beginPath();
            cx.moveTo(33.5, 20.6);
            cx.quadraticCurveTo(36.5, 23.2, 39.5, 20.6);
            cx.moveTo(48.5, 20.6);
            cx.quadraticCurveTo(51.5, 23.2, 54.5, 20.6);
            cx.stroke();

            cx.fillStyle = "#94A3B8";
            cx.beginPath();
            cx.arc(44, 24.8, 1, 0, Math.PI * 2, false);
            cx.fill();

            cx.strokeStyle = "#94A3B8";
            cx.lineWidth = 1.2;
            cx.beginPath();
            cx.moveTo(61, 9.5);
            cx.lineTo(64.5, 9.5);
            cx.lineTo(61, 13);
            cx.lineTo(64.5, 13);
            cx.stroke();
          } else {
            cx.save();
            cx.translate(44, 21);
            cx.translate(
              cycle.interpolate({
                inputRange: [0, 0.12, 0.4, 0.66, 0.74, 0.92, 1],
                outputRange: [-2, -2, 0, 0, 2, 2, -2],
              }),
              0,
            );
            cx.scale(
              1,
              cycle.interpolate({
                inputRange: [0, 0.45, 0.48, 0.51, 0.93, 0.96, 0.99, 1],
                outputRange: [1, 1, 0.16, 1, 1, 0.16, 1, 1],
              }),
            );

            cx.fillStyle = "#38BDF8";
            cx.beginPath();
            cx.roundRect(-10, -3.5, 5, 6.8, 2.5);
            cx.roundRect(5, -3.5, 5, 6.8, 2.5);
            cx.fill();

            cx.fillStyle = "#FFFFFF";
            cx.beginPath();
            cx.arc(-8.4, -1.7, 1.2, 0, Math.PI * 2, false);
            cx.arc(6.6, -1.7, 1.2, 0, Math.PI * 2, false);
            cx.fill();

            cx.strokeStyle = "#7DD3FC";
            cx.lineWidth = 1.25;
            cx.lineCap = "round";
            cx.beginPath();
            cx.moveTo(-2.5, 3.4);
            cx.quadraticCurveTo(0, 5.3, 2.5, 3.4);
            cx.stroke();
            cx.restore();
          }

          // Round front-load washing porthole bezel + glass interior
          cx.fillStyle = bezelFill;
          cx.strokeStyle = bezelStroke;
          cx.lineWidth = 1.15;
          cx.beginPath();
          cx.arc(44, 43.5, 11.4, 0, Math.PI * 2, false);
          cx.fill();
          cx.stroke();

          cx.fillStyle = dark ? "#0F172A" : "#E0F2FE";
          cx.beginPath();
          cx.arc(44, 43.5, 9.6, 0, Math.PI * 2, false);
          cx.fill();

          // Porthole clipped contents: tumbling swatch, sloshing wave, rising bubbles
          cx.save();
          cx.beginPath();
          cx.arc(44, 43.5, 9.6, 0, Math.PI * 2, false);
          cx.clip();

          cx.save();
          cx.translate(44, 43.5);
          cx.rotate(bob.interpolate({ inputRange: [0, 1], outputRange: [-0.21, 0.21] }));
          const swatchScale = bob.interpolate({ inputRange: [0, 1], outputRange: [0.92, 1.04] });
          cx.scale(swatchScale, swatchScale);
          cx.fillStyle = "#FFFFFF";
          cx.strokeStyle = "#93C5FD";
          cx.lineWidth = 0.9;
          cx.beginPath();
          cx.roundRect(-4.5, -5, 9, 9.5, 2);
          cx.fill();
          cx.stroke();
          cx.fillStyle = m === "alert" ? "#F59E0B" : "#3B82F6";
          cx.beginPath();
          cx.arc(0, -0.3, 2.1, 0, Math.PI * 2, false);
          cx.fill();
          cx.restore();

          cx.save();
          cx.translate(wave.interpolate({ inputRange: [0, 1], outputRange: [0, -18] }), 0);
          const waveGrad = cx.createLinearGradient(0, 42, 0, 56);
          waveGrad.addColorStop(0, m === "alert" ? "rgba(251, 191, 36, 0.72)" : "rgba(56, 189, 248, 0.72)");
          waveGrad.addColorStop(1, m === "alert" ? "rgba(217, 119, 6, 0.92)" : "rgba(2, 132, 199, 0.92)");
          cx.fillStyle = waveGrad;
          cx.beginPath();
          cx.moveTo(24, 44.5);
          cx.quadraticCurveTo(28.5, 41.8, 33, 44.5);
          cx.quadraticCurveTo(37.5, 47.2, 42, 44.5);
          cx.quadraticCurveTo(46.5, 41.8, 51, 44.5);
          cx.quadraticCurveTo(55.5, 47.2, 60, 44.5);
          cx.quadraticCurveTo(64.5, 41.8, 69, 44.5);
          cx.quadraticCurveTo(73.5, 47.2, 78, 44.5);
          cx.lineTo(78, 56);
          cx.lineTo(24, 56);
          cx.closePath();
          cx.fill();
          cx.restore();

          // Rising micro-bubbles inside glass
          const drawBubble = (bx: number, by: number, r: number, phaseShift: number) => {
            cx.save();
            const dy = wave.interpolate({
              inputRange: [0, 1],
              outputRange: [phaseShift, phaseShift - 6],
            });
            cx.translate(0, dy);
            cx.globalAlpha = wave.interpolate({
              inputRange: [0, 0.35, 1],
              outputRange: [0.2, 0.9, 0.15],
            });
            cx.fillStyle = "#FFFFFF";
            cx.beginPath();
            cx.arc(bx, by, r, 0, Math.PI * 2, false);
            cx.fill();
            cx.restore();
          };
          drawBubble(39.5, 47.5, 1.2, 2);
          drawBubble(48.5, 46.5, 1.05, 0);
          drawBubble(44, 49, 0.95, 1);

          cx.restore(); // end porthole clip

          // Front glass reflection highlight arc
          cx.strokeStyle = "rgba(255, 255, 255, 0.68)";
          cx.lineWidth = 1.4;
          cx.lineCap = "round";
          cx.beginPath();
          cx.arc(44, 43.5, 7.5, Math.PI * 1.15, Math.PI * 1.55, false);
          cx.stroke();

          cx.restore(); // end chassis bob
          cx.restore(); // end viewport scale
        }}
      />
    </View>
  );
}

function ConveyorPillRail(props: {
  tone: "blue" | "emerald";
  moving: boolean;
}) {
  const phase = createAnimatedValue(0);
  const anim = Animated.loop(
    Animated.timing(phase, { toValue: 1, duration: 720, easing: "linear" }),
    { resetBeforeIteration: true },
  );

  effect(() => {
    if (props.moving && !reduceMotion()) {
      anim.start();
    } else {
      anim.stop();
      phase.set(0);
    }
  });
  cleanup(() => anim.stop());

  return (
    <View class="h-[6px] w-full overflow-hidden rounded-full border border-[#E5E9F0] bg-[#F1F5F9] dark:border-[#232E40] dark:bg-[#1A2230]">
      <Canvas
        class="h-full w-full"
        draw={(cx, size) => {
          if (size.width <= 4 || size.height <= 2) return;
          cx.fillStyle =
            props.tone === "emerald"
              ? "rgba(21, 128, 61, 0.42)"
              : "rgba(37, 99, 235, 0.36)";
          cx.save();
          cx.translate(phase.interpolate({ inputRange: [0, 1], outputRange: [0, 15] }), 0);
          for (let x = -15; x < size.width + 15; x += 15) {
            cx.fillRect(x, 1, 5, Math.max(1, size.height - 2));
          }
          cx.restore();
        }}
      />
    </View>
  );
}

function formatOrderNo(taskIndex: number): string {
  const num = taskIndex > 0 ? taskIndex : 1;
  return `#${String(num).padStart(2, "0")}`;
}

export function WashBotCard(props: {
  tile: ResultTileDTO;
  recentOutURL?: string;
  celebrating?: boolean;
  animPaused?: boolean;
}) {
  const trackW = signal(72);
  const progress = createAnimatedValue(props.tile.taskIndex % 2 === 1 ? 0.18 : 0.65);

  const slideAnim = Animated.loop(
    Animated.timing(progress, { toValue: 1, duration: 4200, easing: "linear" }),
    { resetBeforeIteration: true },
  );

  const isAlert = () => props.tile.status === "需要人工验证";
  const isCelebrate = () => Boolean(props.celebrating && !isAlert());
  const isPaused = () => Boolean(props.animPaused);
  const botMode = (): WashBotMode =>
    isAlert() ? "alert" : isCelebrate() ? "celebrate" : isPaused() ? "paused" : "working";

  effect(() => {
    const runSlide = !isAlert() && !isPaused() && !reduceMotion();
    if (runSlide) {
      slideAnim.start();
    } else {
      slideAnim.stop();
      progress.set(isAlert() ? 0.32 : 0.24);
    }
  });
  cleanup(() => slideAnim.stop());

  const inputSrc = () => props.tile.srcURL || "";
  const outputSrc = () => props.tile.fileURL || props.recentOutURL || "";
  const stationLabel = () => {
    const acct = (props.tile.accountName || "").trim();
    if (acct) return acct;
    const plat = (props.tile.platform || "").trim();
    return plat ? `${plat}工位` : "洗图工位";
  };

  const w = () => Math.max(52, trackW());
  const inLeft = () =>
    isAlert() || isPaused() || reduceMotion()
      ? Math.max(8, Math.round(w() * 0.3))
      : progress.interpolate({
          inputRange: [0, 0.12, 0.44, 0.54, 1],
          outputRange: [2, Math.max(8, w() * 0.14), Math.max(12, w() - 22), Math.max(14, w() - 8), Math.max(14, w() - 8)],
        });
  const inOpacity = () =>
    isAlert()
      ? 1
      : isPaused() || reduceMotion()
        ? 0.92
        : progress.interpolate({
            inputRange: [0, 0.1, 0.44, 0.54, 1],
            outputRange: [0.35, 1, 1, 0, 0],
          });
  const inScale = () =>
    isAlert() || isPaused() || reduceMotion()
      ? 1
      : progress.interpolate({
          inputRange: [0, 0.12, 0.44, 0.54, 1],
          outputRange: [0.86, 1, 0.84, 0.52, 0.52],
        });

  const outLeft = () =>
    isAlert()
      ? 0
      : isPaused() || reduceMotion()
        ? Math.max(8, Math.round(w() * 0.28))
        : progress.interpolate({
            inputRange: [0, 0.5, 0.58, 0.92, 1],
            outputRange: [-4, -4, Math.max(6, w() * 0.1), Math.max(12, w() - 34), Math.max(14, w() - 24)],
          });
  const outOpacity = () =>
    isAlert()
      ? 0
      : isPaused() || reduceMotion()
        ? 0.9
        : progress.interpolate({
            inputRange: [0, 0.5, 0.58, 0.92, 1],
            outputRange: [0, 0, 1, 1, 0.25],
          });

  return (
    <View
      testID={`result-tile-card-${props.tile.taskIndex}`}
      class="gap-1.5 rounded-lg border px-2.5 py-2 transition-colors duration-200"
      classList={{
        "border-amber-500/70 bg-white dark:border-amber-400/60 dark:bg-[#131924]": isAlert(),
        "border-emerald-600/80 bg-white dark:border-emerald-400/70 dark:bg-[#131924]": isCelebrate(),
        "border-blue-500/65 bg-white dark:border-blue-400/55 dark:bg-[#131924]": !isAlert() && !isCelebrate(),
      }}
    >
      {/* Card Header */}
      <View class="min-h-[22px] flex-row items-center justify-between gap-2">
        <View class="min-w-0 flex-1 flex-row items-center gap-1.5">
          <LivePulseDot
            color={isAlert() ? "amber" : isCelebrate() ? "emerald" : "blue"}
            size={6}
            ping={!isPaused()}
          />
          <Text class="truncate text-xs font-semibold text-slate-900 dark:text-slate-100">
            {`工位 · ${stationLabel()}`}
          </Text>
          <Badge text={props.tile.engine || "Camoufox"} variant="neutral" />
          <Text
            class="min-w-0 flex-1 truncate text-[11px] text-slate-500 dark:text-slate-400"
            style={{ fontVariant: ["tabular-nums"] }}
          >
            {`${formatOrderNo(props.tile.taskIndex)} · ${props.tile.title || "洗图任务"}`}
          </Text>
        </View>

        <View class="shrink-0 flex-row items-center gap-1.5">
          <Text
            class="text-[11px] font-semibold"
            classList={{
              "text-amber-700 dark:text-amber-300": isAlert(),
              "text-emerald-700 dark:text-emerald-300": isCelebrate(),
              "text-blue-600 dark:text-blue-400": !isAlert() && !isCelebrate(),
            }}
            style={{ fontVariant: ["tabular-nums"] }}
          >
            {isAlert() ? "需人工验证" : props.tile.elapsed || "正在洗图"}
          </Text>
          <Text class="text-[10.5px] font-medium text-slate-500 dark:text-slate-400">
            {isAlert() ? "验证卡点" : "正在洗图"}
          </Text>
          <If when={props.tile.accountID > 0 && props.tile.platform !== "Antigravity"}>
            <Button
              text={isAlert() ? "去验证" : "可视"}
              size="small"
              variant={isAlert() ? "primary" : "ghost"}
              onPress={() => openBrowserDrawer(props.tile.accountID)}
            />
          </If>
        </View>
      </View>

      {/* Studio Conveyor Track + Ceramic Capsule Bot */}
      <View class="h-[60px] flex-row items-center gap-1.5">
        {/* Left Intake Rail */}
        <View
          class="relative h-[54px] min-w-[54px] flex-1 justify-center overflow-hidden"
          onLayout={(e) => trackW.set(e.bounds.size.width)}
        >
          <Text style={{ position: "absolute", top: 1, left: 2 }} class="text-[9px] text-slate-400 dark:text-slate-500">
            原图
          </Text>
          <ConveyorPillRail tone="blue" moving={!isAlert() && !isPaused()} />
          <View
            style={{
              position: "absolute",
              top: 12,
              left: inLeft(),
              width: 30,
              height: 30,
              opacity: inOpacity(),
              transform: [{ scale: inScale() }],
            }}
          >
            <HoverPreviewTarget
              previewKey={`wb-in-${props.tile.taskIndex}`}
              src={inputSrc()}
              title={props.tile.title || `素材 ${formatOrderNo(props.tile.taskIndex)}`}
              subtitle={`${stationLabel()} 正在输入原图 · ${formatOrderNo(props.tile.taskIndex)}`}
              badge={isAlert() ? "等待验证" : "进入洗图舱"}
              badgeVariant={isAlert() ? "warn" : "accent"}
              class="h-[30px] w-[30px] items-center justify-center overflow-hidden rounded-md border border-[#CBD5E1] bg-white p-0.5 dark:border-[#334155] dark:bg-[#1A2232]"
            >
              <If
                when={Boolean(inputSrc())}
                else={<Icon name="image" size={14} color="#64748B" />}
              >
                <Image
                  src={inputSrc()}
                  fadeDuration={150}
                  class="h-full w-full object-contain"
                  testID={`result-tile-image-${props.tile.taskIndex}`}
                />
              </If>
            </HoverPreviewTarget>
          </View>
        </View>

        {/* Center Character: 小白洗图君 */}
        <WashBotAvatar mode={botMode()} testID={`washbot-canvas-${props.tile.taskIndex}`} />

        {/* Right Eject Rail */}
        <View class="relative h-[54px] min-w-[54px] flex-1 justify-center overflow-hidden">
          <Text style={{ position: "absolute", top: 1, right: 2 }} class="text-[9px] font-medium text-emerald-700 dark:text-emerald-300">
            成图
          </Text>
          <ConveyorPillRail tone="emerald" moving={!isAlert() && !isPaused()} />
          <View
            style={{
              position: "absolute",
              top: 12,
              left: outLeft(),
              width: 30,
              height: 30,
              opacity: outOpacity(),
            }}
          >
            <HoverPreviewTarget
              previewKey={`wb-out-${props.tile.taskIndex}`}
              src={outputSrc()}
              title={`洗后预览 · ${props.tile.title || formatOrderNo(props.tile.taskIndex)}`}
              subtitle={`${stationLabel()} 实时洗出白底成图`}
              badge="洗出完成"
              badgeVariant="success"
              class="relative h-[30px] w-[30px] items-center justify-center overflow-hidden rounded-md border border-emerald-600/65 bg-white p-0.5 dark:border-emerald-400/60 dark:bg-[#1A2232]"
            >
              <If
                when={Boolean(outputSrc())}
                else={<Icon name="sparkles" size={13} color="#15803D" />}
              >
                <Image src={outputSrc()} fadeDuration={150} class="h-full w-full object-contain" />
              </If>
              <View
                style={{ position: "absolute", top: -2, right: -2, width: 11, height: 11 }}
                class="items-center justify-center rounded-full border border-white bg-[#15803D] dark:border-[#131924]"
              >
                <Icon name="check" size={7} color="#FFFFFF" />
              </View>
            </HoverPreviewTarget>
          </View>
        </View>
      </View>
    </View>
  );
}
