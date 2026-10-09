import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Trash2 } from "lucide-react";
import { clampToViewport, ContextMenu, type MenuItem } from "@/components/ContextMenu";

const setup = (over: Partial<{ onSelect: () => void; onClose: () => void }> = {}) => {
  const onSelect = over.onSelect ?? vi.fn();
  const onClose = over.onClose ?? vi.fn();
  const sub = vi.fn();
  const items: MenuItem[] = [
    { id: "a", label: "Uno", onSelect },
    { id: "b", label: "Dos", disabled: true, hint: "Próximamente", onSelect: vi.fn() },
    { id: "c", label: "Agregar", children: [{ id: "c1", label: "Grilla actual", onSelect: sub }] },
  ];
  render(<ContextMenu x={10} y={10} items={items} onClose={onClose} />);
  return { onSelect, onClose, sub };
};

describe("ContextMenu", () => {
  it("renders item icons as decorative lucide glyphs and a chevron on submenu rows", () => {
    const items: MenuItem[] = [
      { id: "del", label: "Borrar", icon: Trash2, onSelect: vi.fn() },
      { id: "sub", label: "Mas", children: [{ id: "x", label: "X" }] },
    ];
    render(<ContextMenu x={10} y={10} items={items} onClose={vi.fn()} />);
    const del = screen.getByRole("menuitem", { name: "Borrar" });
    const glyph = del.querySelector("svg");
    expect(glyph).not.toBeNull();
    expect(glyph).toHaveClass("lucide-trash-2");
    expect(glyph).toHaveAttribute("aria-hidden", "true");
    expect(screen.getByRole("menuitem", { name: "Mas" }).querySelector("svg.lucide-chevron-right")).not.toBeNull();
  });

  it("focuses the first item, moves with arrows and activates with Enter", () => {
    const { onSelect, onClose } = setup();
    expect(screen.getByRole("menu")).toBeInTheDocument();
    const first = screen.getByRole("menuitem", { name: "Uno" });
    expect(first).toHaveFocus();
    fireEvent.keyDown(first, { key: "ArrowDown" });
    expect(screen.getByRole("menuitem", { name: /Dos/ })).toHaveFocus();
    expect(screen.getByRole("menuitem", { name: /Dos/ })).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(first);
    expect(onSelect).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it("does not activate disabled items", () => {
    const { onClose } = setup();
    fireEvent.click(screen.getByRole("menuitem", { name: /Dos/ }));
    expect(onClose).not.toHaveBeenCalled();
  });

  it("opens a submenu with ArrowRight and closes it with ArrowLeft", () => {
    const { sub } = setup();
    const parent = screen.getByRole("menuitem", { name: "Agregar" });
    parent.focus();
    fireEvent.keyDown(parent, { key: "ArrowRight" });
    const child = screen.getByRole("menuitem", { name: "Grilla actual" });
    expect(child).toHaveFocus();
    fireEvent.keyDown(child, { key: "ArrowLeft" });
    expect(screen.queryByRole("menuitem", { name: "Grilla actual" })).not.toBeInTheDocument();
    expect(parent).toHaveFocus();
    fireEvent.keyDown(parent, { key: "ArrowRight" });
    fireEvent.click(screen.getByRole("menuitem", { name: "Grilla actual" }));
    expect(sub).toHaveBeenCalled();
  });

  it("closes on Escape and on an outside pointer down", () => {
    const { onClose } = setup();
    fireEvent.keyDown(screen.getByRole("menuitem", { name: "Uno" }), { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.pointerDown(document.body);
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("clamps to the viewport", () => {
    expect(clampToViewport(990, 790, 200, 100, 1000, 800)).toEqual({ left: 792, top: 692 });
    expect(clampToViewport(-5, -5, 200, 100, 1000, 800)).toEqual({ left: 8, top: 8 });
  });
});
