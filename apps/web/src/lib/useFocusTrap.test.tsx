import { useRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useFocusTrap } from "./useFocusTrap";

function Trap({ name, onEscape }: { name: string; onEscape: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useFocusTrap(ref, onEscape);
  return (
    <div ref={ref} role="dialog" aria-label={name}>
      <button>{name} first</button>
      <button>{name} last</button>
    </div>
  );
}

describe("useFocusTrap", () => {
  it("focuses the first control, wraps Tab, calls onEscape and returns focus", () => {
    const opener = document.createElement("button");
    document.body.append(opener);
    opener.focus();
    const onEscape = vi.fn();
    const { unmount } = render(<Trap name="a" onEscape={onEscape} />);
    const first = screen.getByRole("button", { name: "a first" });
    const last = screen.getByRole("button", { name: "a last" });
    expect(first).toHaveFocus();
    last.focus();
    fireEvent.keyDown(document, { key: "Tab" });
    expect(first).toHaveFocus();
    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(last).toHaveFocus();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onEscape).toHaveBeenCalledTimes(1);
    unmount();
    expect(opener).toHaveFocus();
    opener.remove();
  });

  it("lets only the topmost trap react to Escape and Tab, and hands over when it unmounts", () => {
    const lower = vi.fn();
    const upper = vi.fn();
    const { rerender } = render(
      <>
        <Trap name="lower" onEscape={lower} />
        <Trap name="upper" onEscape={upper} />
      </>,
    );
    fireEvent.keyDown(document, { key: "Escape" });
    expect(upper).toHaveBeenCalledTimes(1);
    expect(lower).not.toHaveBeenCalled();

    // Tab wraps inside the upper trap only.
    screen.getByRole("button", { name: "upper last" }).focus();
    fireEvent.keyDown(document, { key: "Tab" });
    expect(screen.getByRole("button", { name: "upper first" })).toHaveFocus();

    rerender(<Trap name="lower" onEscape={lower} />);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(lower).toHaveBeenCalledTimes(1);
    expect(upper).toHaveBeenCalledTimes(1);
  });
});
