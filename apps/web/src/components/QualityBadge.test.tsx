import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { QualityBadge } from "@/components/QualityBadge";

describe("QualityBadge", () => {
  it("shows SUB for the secondary stream", () => {
    render(<QualityBadge visible="sub" pending={null} />);
    const badge = screen.getByTestId("quality-badge");
    expect(badge).toHaveTextContent("SUB");
    expect(badge).toHaveAttribute("data-quality", "sub");
    expect(badge).toHaveAttribute("aria-label", "Calidad: secundaria");
  });

  it("shows MAIN for the primary stream", () => {
    render(<QualityBadge visible="main" pending={null} />);
    const badge = screen.getByTestId("quality-badge");
    expect(badge).toHaveTextContent("MAIN");
    expect(badge).toHaveAttribute("data-quality", "main");
    expect(badge).toHaveAttribute("aria-label", "Calidad: principal");
  });

  it("shows the handoff in progress until the target has a frame", () => {
    render(<QualityBadge visible="sub" pending="main" />);
    const badge = screen.getByTestId("quality-badge");
    expect(badge).toHaveTextContent("SUB → MAIN");
    expect(badge).toHaveAttribute("data-quality", "switching");
    expect(badge).toHaveAttribute("aria-label", "Calidad: secundaria, cambiando a principal");
  });
});
