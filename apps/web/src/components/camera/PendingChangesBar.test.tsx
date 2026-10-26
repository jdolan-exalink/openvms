import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { PendingChangesBar } from "./PendingChangesBar";

describe("PendingChangesBar", () => {
  it("renders nothing without changes", () => {
    const { container } = render(<PendingChangesBar count={0} onDiscard={vi.fn()} onReview={vi.fn()} onSave={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("counts the changes and wires Descartar, Revisar cambios and Guardar", () => {
    const [discard, review, save] = [vi.fn(), vi.fn(), vi.fn()];
    render(<PendingChangesBar count={3} onDiscard={discard} onReview={review} onSave={save} />);
    expect(screen.getByText("3 cambios sin guardar")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Descartar" }));
    fireEvent.click(screen.getByRole("button", { name: "Revisar cambios" }));
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect([discard, review, save].map((f) => f.mock.calls.length)).toEqual([1, 1, 1]);
  });

  it("blocks review and save while there are invalid values", () => {
    render(<PendingChangesBar count={1} errorCount={2} onDiscard={vi.fn()} onReview={vi.fn()} onSave={vi.fn()} />);
    expect(screen.getByText("1 cambio sin guardar")).toBeInTheDocument();
    expect(screen.getByText(/2 errores/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revisar cambios" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Guardar" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Descartar" })).toBeEnabled();
  });
});
