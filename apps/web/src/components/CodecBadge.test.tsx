import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CodecBadge } from "./CodecBadge";

describe("CodecBadge", () => {
  it("flags an H.265 stream with a label and an explanation", () => {
    render(<CodecBadge codec="h265" />);
    const badge = screen.getByTestId("codec-badge");
    expect(badge).toHaveTextContent("H.265");
    expect(badge).toHaveAttribute("role", "img");
    expect(badge.getAttribute("aria-label")).toMatch(/H\.265/);
    expect(badge.getAttribute("aria-label")).toMatch(/navegadores/i);
    expect(badge).toHaveAttribute("title", badge.getAttribute("aria-label"));
  });

  it("also flags HEVC", () => {
    render(<CodecBadge codec="HEVC" />);
    expect(screen.getByTestId("codec-badge")).toHaveTextContent("H.265");
  });

  it("renders nothing for H.264, an unknown codec or no codec", () => {
    for (const codec of ["h264", "jpeg", "", null, undefined]) {
      const { container, unmount } = render(<CodecBadge codec={codec} />);
      expect(container).toBeEmptyDOMElement();
      unmount();
    }
  });
});
