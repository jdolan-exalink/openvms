import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Modal } from "./Modal";

describe("Modal", () => {
  it("is an aria-modal dialog, closes on Esc and on the close button, and restores focus", () => {
    const trigger = document.createElement("button");
    trigger.textContent = "abrir";
    document.body.appendChild(trigger);
    trigger.focus();
    expect(document.activeElement).toBe(trigger);

    const onClose = vi.fn();
    const { unmount } = render(
      <Modal title="Detalle" onClose={onClose}>
        <button type="button">Adentro</button>
      </Modal>,
    );

    const dialog = screen.getByRole("dialog", { name: "Detalle" });
    expect(dialog).toHaveAttribute("aria-modal", "true");

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "Cerrar" }));
    expect(onClose).toHaveBeenCalledTimes(2);

    unmount();
    expect(document.activeElement).toBe(trigger);
    trigger.remove();
  });

  it("traps Tab focus inside the dialog", () => {
    render(
      <Modal title="Detalle" onClose={() => {}}>
        <button type="button">Primero</button>
        <button type="button">Último</button>
      </Modal>,
    );

    const last = screen.getByRole("button", { name: "Último" });
    last.focus();
    expect(document.activeElement).toBe(last);

    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cerrar" }));
  });
});
