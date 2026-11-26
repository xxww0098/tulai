import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HoverPreviewHost, HoverPreviewTarget, clearHoverPreview } from "@/components/HoverPreview";
import { Tooltip, TooltipGroup, TooltipProvider } from "@/components/ui";
import {
  PointerVelocityTracker,
  isPointInSafeCorridor,
} from "@/lib/hoverIntent";

describe("hoverIntent math & pointer speed", () => {
  it("classifies pointer movement > 0.1 px/ms over 90ms as fast and resting as slow", () => {
    const tracker = new PointerVelocityTracker(90);
    tracker.record(0, 0, 1000);
    tracker.record(25, 0, 1050);
    // 25px / 50ms = 0.5 px/ms -> fast
    expect(tracker.isFast(1050)).toBe(true);

    // After 90ms window expires with no movement, speed drops to 0 (slow/resting)
    expect(tracker.isFast(1150)).toBe(false);
    expect(tracker.getSpeed(1150)).toBe(0);
  });

  it("detects points inside the safe corridor triangle toward a right-side panel", () => {
    const origin = { x: 100, y: 200 };
    const panel = { left: 200, top: 100, right: 480, bottom: 300 };

    expect(isPointInSafeCorridor({ x: 150, y: 200 }, origin, panel)).toBe(true);
    expect(isPointInSafeCorridor({ x: 150, y: 160 }, origin, panel)).toBe(true);
    // Way above the corridor triangle
    expect(isPointInSafeCorridor({ x: 150, y: 20 }, origin, panel)).toBe(false);
  });
});

describe("Tooltip label timing, group swap, press suppression, and keyboard", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("waits 500ms to open, swaps in place inside group, keeps 400ms grace, and suppresses after press until leave", () => {
    render(
      <TooltipProvider>
        <TooltipGroup data-testid="toolbar">
          <Tooltip content="First label" side="top">
            <button type="button">Btn A</button>
          </Tooltip>
          <Tooltip content="Second label" side="top">
            <button type="button">Btn B</button>
          </Tooltip>
        </TooltipGroup>
        <button type="button">Outside</button>
      </TooltipProvider>,
    );

    const toolbar = screen.getByTestId("toolbar");
    const btnA = screen.getByRole("button", { name: "Btn A" });
    const btnB = screen.getByRole("button", { name: "Btn B" });
    const outside = screen.getByRole("button", { name: "Outside" });

    // Sweeping across opens nothing
    fireEvent.mouseEnter(btnA);
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

    fireEvent.mouseLeave(btnA, { relatedTarget: btnB });
    fireEvent.mouseEnter(btnB);
    act(() => {
      vi.advanceTimersByTime(490);
    });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

    // Resting 500ms opens it
    act(() => {
      vi.advanceTimersByTime(20);
    });
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent("Second label");
    expect(tip).toHaveAttribute("data-state", "entering");

    // Moving to neighbor inside group changes text in place with no fade-out-and-back
    fireEvent.mouseLeave(btnB, { relatedTarget: toolbar });
    fireEvent.mouseEnter(btnA);
    const sameTip = screen.getByRole("tooltip");
    expect(sameTip).toHaveTextContent("First label");
    expect(sameTip).toHaveAttribute("data-state", "steady");

    // Pressing trigger hides label and prevents re-opening until pointer leaves
    fireEvent.mouseDown(btnA);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    fireEvent.mouseEnter(btnA);
    act(() => {
      vi.advanceTimersByTime(600);
    });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

    // Leaving trigger and returning after >400ms restores 500ms wait
    fireEvent.mouseLeave(btnA, { relatedTarget: outside });
    act(() => {
      vi.advanceTimersByTime(450);
    });
    fireEvent.mouseEnter(btnA);
    act(() => {
      vi.advanceTimersByTime(450);
    });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(60);
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent("First label");

    // Leaving group hides at once (60ms exit fade), and re-entering within 400ms opens at once
    fireEvent.mouseLeave(toolbar, { relatedTarget: outside });
    act(() => {
      vi.advanceTimersByTime(70);
    });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(200);
    });
    fireEvent.mouseEnter(btnB);
    expect(screen.getByRole("tooltip")).toHaveTextContent("Second label");
  });

  it("opens at once on keyboard focus with aria-describedby and closes on Escape", () => {
    render(
      <TooltipProvider>
        <Tooltip content="Focus tip" side="top">
          <button type="button">Focus me</button>
        </Tooltip>
      </TooltipProvider>,
    );

    const btn = screen.getByRole("button", { name: "Focus me" });
    act(() => {
      btn.focus();
    });

    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent("Focus tip");
    expect(btn).toHaveAttribute("aria-describedby", tip.id);

    act(() => {
      fireEvent.keyDown(window, { key: "Escape" });
      vi.advanceTimersByTime(70);
    });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });
});

describe("HoverPreview preview intent, grace period, velocity swap, and keyboard", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    clearHoverPreview();
  });

  afterEach(() => {
    clearHoverPreview();
    vi.useRealTimers();
  });

  it("opens after 900ms, honors 400ms leave grace, ignores fast sweeps, and swaps in place on 60ms slow dwell", () => {
    render(
      <div>
        <HoverPreviewTarget
          data-testid="target-a"
          previewKey="a"
          src="tulaimedia://localhost/a.png"
          title="Item A"
          tabIndex={0}
          openOnEnter
        >
          <div>A</div>
        </HoverPreviewTarget>
        <HoverPreviewTarget
          data-testid="target-b"
          previewKey="b"
          src="tulaimedia://localhost/b.png"
          title="Item B"
          tabIndex={0}
          openOnEnter
        >
          <div>B</div>
        </HoverPreviewTarget>
        <HoverPreviewHost />
      </div>,
    );

    const targetA = screen.getByTestId("target-a");
    const targetB = screen.getByTestId("target-b");

    // 900ms wait before opening
    fireEvent.mouseEnter(targetA);
    act(() => {
      vi.advanceTimersByTime(850);
    });
    expect(screen.queryByTestId("hover-preview-card")).not.toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(60);
    });
    expect(screen.getByTestId("hover-preview-card")).toHaveTextContent("Item A");

    // Leaving starts 400ms grace; returning within 400ms keeps it open
    fireEvent.mouseLeave(targetA);
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(screen.getByTestId("hover-preview-card")).toHaveTextContent("Item A");
    fireEvent.mouseEnter(targetA);
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(screen.getByTestId("hover-preview-card")).toHaveTextContent("Item A");

    // Fast movement across target B does not swap immediately
    fireEvent.mouseMove(targetA, { clientX: 10, clientY: 10 });
    act(() => {
      vi.advanceTimersByTime(30);
    });
    fireEvent.mouseMove(targetB, { clientX: 80, clientY: 10 });
    fireEvent.mouseLeave(targetA, { relatedTarget: targetB });
    fireEvent.mouseEnter(targetB);
    act(() => {
      vi.advanceTimersByTime(60);
    });
    expect(screen.getByTestId("hover-preview-card")).toHaveTextContent("Item A");

    // Once pointer slows/rests for 60ms after 90ms window cools, it swaps in place to Item B
    act(() => {
      vi.advanceTimersByTime(110);
    });
    expect(screen.getByTestId("hover-preview-card")).toHaveTextContent("Item B");
    expect(screen.getByTestId("hover-preview-card")).toHaveAttribute("data-state", "steady");

    // Leaving completely closes after 400ms grace + 140ms fade
    fireEvent.mouseLeave(targetB);
    act(() => {
      vi.advanceTimersByTime(350);
    });
    expect(screen.getByTestId("hover-preview-card")).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(screen.queryByTestId("hover-preview-card")).not.toBeInTheDocument();
  });

  it("opens on Enter when openOnEnter is set and closes on Escape returning focus", () => {
    render(
      <div>
        <HoverPreviewTarget
          data-testid="target-kb"
          previewKey="kb"
          src="tulaimedia://localhost/kb.png"
          title="Keyboard Item"
          tabIndex={0}
          openOnEnter
        >
          <div>KB</div>
        </HoverPreviewTarget>
        <HoverPreviewHost />
      </div>,
    );

    const target = screen.getByTestId("target-kb");
    act(() => {
      target.focus();
      fireEvent.keyDown(target, { key: "Enter" });
    });

    const card = screen.getByTestId("hover-preview-card");
    expect(card).toHaveTextContent("Keyboard Item");
    expect(target).toHaveAttribute("aria-describedby", card.id);

    act(() => {
      fireEvent.keyDown(window, { key: "Escape" });
    });
    expect(document.activeElement).toBe(target);
    act(() => {
      vi.advanceTimersByTime(150);
    });
    expect(screen.queryByTestId("hover-preview-card")).not.toBeInTheDocument();
  });
});
