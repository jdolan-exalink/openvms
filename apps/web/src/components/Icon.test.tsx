import { render, screen } from "@testing-library/react";
import { Video } from "lucide-react";
import { describe, expect, it } from "vitest";
import { Icon } from "./Icon";

function svg(container: HTMLElement) {
  const el = container.querySelector("svg");
  if (!el) throw new Error("no svg rendered");
  return el;
}

describe("Icon", () => {
  it("defaults to 20px with a 1.75 stroke and is decorative", () => {
    const { container } = render(<Icon icon={Video} />);
    const el = svg(container);
    expect(el.getAttribute("width")).toBe("20");
    expect(el.getAttribute("height")).toBe("20");
    expect(el.getAttribute("stroke-width")).toBe("1.75");
    expect(el.getAttribute("aria-hidden")).toBe("true");
    expect(el.classList.contains("lucide")).toBe(true);
  });

  it("maps named sizes to the 16/20/24/32 scale", () => {
    const sizes = { xs: "16", sm: "20", md: "24", lg: "32" } as const;
    for (const [name, px] of Object.entries(sizes)) {
      const { container, unmount } = render(<Icon icon={Video} size={name as keyof typeof sizes} />);
      expect(svg(container).getAttribute("width")).toBe(px);
      unmount();
    }
  });

  it("uses a lighter 1.5 stroke for the lg size", () => {
    const { container } = render(<Icon icon={Video} size="lg" />);
    expect(svg(container).getAttribute("stroke-width")).toBe("1.5");
  });

  it("lets an explicit strokeWidth win", () => {
    const { container } = render(<Icon icon={Video} size="lg" strokeWidth={2} />);
    expect(svg(container).getAttribute("stroke-width")).toBe("2");
  });

  it("accepts a numeric size", () => {
    const { container } = render(<Icon icon={Video} size={18} />);
    expect(svg(container).getAttribute("width")).toBe("18");
    expect(svg(container).getAttribute("stroke-width")).toBe("1.75");
  });

  it("exposes an accessible name when a label is given", () => {
    render(<Icon icon={Video} label="Camera" />);
    const img = screen.getByRole("img", { name: "Camera" });
    expect(img.getAttribute("aria-hidden")).toBeNull();
  });

  it("merges className and inherits currentColor", () => {
    const { container } = render(<Icon icon={Video} className="text-accent shrink-0" />);
    const el = svg(container);
    expect(el.classList.contains("text-accent")).toBe(true);
    expect(el.getAttribute("stroke")).toBe("currentColor");
  });
});
