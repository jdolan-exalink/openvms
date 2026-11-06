import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ArPlate } from "./ArPlate";

describe("ArPlate", () => {
  // jsdom has no layout, so the guard is on the classes that keep the text inside the plate when
  // a narrow card squeezes it: the plate must not shrink and its text must not wrap.
  it.each(["AB123CD", "ABC123", "MERCOSUR1"])("keeps %s on one line at its own width", (text) => {
    render(<ArPlate plate={text} />);
    const plate = screen.getByLabelText(`Patente ${text}`);
    expect(plate).toHaveClass("shrink-0");
    expect(plate.lastElementChild).toHaveClass("whitespace-nowrap");
  });

  it("formats a Mercosur plate in groups and an old plate in two", () => {
    render(<><ArPlate plate="ab123cd" /><ArPlate plate="abc123" /></>);
    expect(screen.getByText("AB 123 CD")).toBeInTheDocument();
    expect(screen.getByText("ABC 123")).toBeInTheDocument();
  });
});
