import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ObjectBadge } from "./ObjectBadge";

describe("ObjectBadge", () => {
  it("shows only the icon: no words, no percentage", () => {
    render(<ObjectBadge object="person" confidence={0.9} />);
    const badge = screen.getByTestId("object-badge");
    expect(badge.textContent).toBe("");
    expect(badge.querySelector("svg")).not.toBeNull();
  });

  it("keeps what it detected in the tooltip and the accessible name", () => {
    render(<ObjectBadge object="person" confidence={0.9} />);
    const badge = screen.getByRole("img", { name: "person 90%" });
    expect(badge).toHaveAttribute("title", "person 90%");
  });

  it("works without a confidence", () => {
    render(<ObjectBadge object="car" />);
    expect(screen.getByRole("img", { name: "car" })).toHaveAttribute("title", "car");
  });

  it("marks an alarm in the name and the look", () => {
    render(<ObjectBadge object="person" confidence={0.5} alarm />);
    const badge = screen.getByTestId("object-badge");
    expect(badge).toHaveAttribute("data-alarm", "true");
    expect(badge.getAttribute("aria-label")).toMatch(/alarma/i);
  });

  it("renders nothing without an object", () => {
    const { container } = render(<ObjectBadge object={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });
});
