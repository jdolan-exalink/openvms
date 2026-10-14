import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Sparkline } from "./Sparkline";

describe("Sparkline", () => {
  it("exposes the summary as its text alternative", () => {
    render(<Sparkline values={[1, 2, 3]} label="3 puntos" />);
    expect(screen.getByRole("img", { name: "3 puntos" })).toBeInTheDocument();
  });

  it("draws one point per value, scaled into the viewbox", () => {
    const { container } = render(<Sparkline values={[0, 5, 10]} label="x" />);
    const pts = container.querySelector('[data-series="mbps"]')!.getAttribute("points")!.split(" ");
    expect(pts).toHaveLength(3);
    const ys = pts.map((p) => Number(p.split(",")[1]));
    expect(ys[0]).toBeGreaterThan(ys[1]!);
    expect(ys[1]).toBeGreaterThan(ys[2]!);
    expect(Math.min(...ys)).toBeGreaterThanOrEqual(0);
    expect(Math.max(...ys)).toBeLessThanOrEqual(56);
  });

  it("draws a flat segment for a single sample and nothing for none", () => {
    const one = render(<Sparkline values={[4]} label="x" />);
    expect(one.container.querySelector('[data-series="mbps"]')!.getAttribute("points")!.split(" ")).toHaveLength(2);
    const none = render(<Sparkline values={[]} label="x" />);
    expect(none.container.querySelector("polyline")).toBeNull();
  });

  it("draws the dropped-frames series only when something was dropped", () => {
    const quiet = render(<Sparkline values={[1, 2]} secondary={[0, 0]} label="x" />);
    expect(quiet.container.querySelector('[data-series="dropped"]')).toBeNull();
    const noisy = render(<Sparkline values={[1, 2]} secondary={[0, 3]} label="x" />);
    expect(noisy.container.querySelector('[data-series="dropped"]')).not.toBeNull();
  });
});
