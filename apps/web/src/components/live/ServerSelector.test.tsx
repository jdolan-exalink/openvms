import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ServerChoice } from "@/lib/serverSelector";
import { ServerSelector } from "./ServerSelector";

const choice = (id: string, name: string, siteName: string, inGrid: number, total: number): ServerChoice => ({ id, name, siteName, inGrid, total, checked: inGrid > 0 });
const choices = [
  choice("srv-a", "Alfa", "Campus", 3, 3),
  choice("srv-b", "Beta", "Campus", 0, 5),
  choice("srv-x", "Anexo uno", "Anexo", 2, 4),
];

describe("ServerSelector", () => {
  it("is a closed drop-down that says how many servers are in the view", () => {
    render(<ServerSelector choices={choices} onToggle={() => {}} />);
    const button = screen.getByRole("button", { name: /Servidores/ });
    expect(button).toHaveAttribute("aria-expanded", "false");
    expect(button).toHaveTextContent("2/3");
    expect(screen.queryByRole("group", { name: /Servidores en la vista/i })).not.toBeInTheDocument();
  });

  it("opens a list with one check per server, grouped by site, checked while it is in the grid", () => {
    render(<ServerSelector choices={choices} onToggle={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /Servidores/ }));
    const list = screen.getByRole("group", { name: /Servidores en la vista/i });
    const boxes = within(list).getAllByRole("checkbox");
    expect(boxes.map((b) => (b as HTMLInputElement).checked)).toEqual([true, false, true]);
    expect(within(list).getByRole("checkbox", { name: /Alfa/ })).toBeChecked();
    expect(within(list).getByRole("checkbox", { name: /Beta/ })).not.toBeChecked();
    expect(within(list).getByText("Anexo")).toBeInTheDocument();
    expect(within(list).getByText("Campus")).toBeInTheDocument();
    expect(within(list).getByText("3/3")).toBeInTheDocument();
    expect(within(list).getByText("0/5")).toBeInTheDocument();
  });

  it("reports what was toggled and stays open so several servers can be changed", () => {
    const onToggle = vi.fn();
    render(<ServerSelector choices={choices} onToggle={onToggle} />);
    fireEvent.click(screen.getByRole("button", { name: /Servidores/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Beta/ }));
    expect(onToggle).toHaveBeenCalledWith("srv-b", true);
    fireEvent.click(screen.getByRole("checkbox", { name: /Alfa/ }));
    expect(onToggle).toHaveBeenLastCalledWith("srv-a", false);
    expect(screen.getByRole("group", { name: /Servidores en la vista/i })).toBeInTheDocument();
  });

  it("closes with Escape and with a click outside", () => {
    render(<div><ServerSelector choices={choices} onToggle={() => {}} /><p>afuera</p></div>);
    const open = () => fireEvent.click(screen.getByRole("button", { name: /Servidores/ }));
    open();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("group", { name: /Servidores en la vista/i })).not.toBeInTheDocument();
    open();
    fireEvent.mouseDown(screen.getByText("afuera"));
    expect(screen.queryByRole("group", { name: /Servidores en la vista/i })).not.toBeInTheDocument();
  });

  it("is not worth showing with a single server", () => {
    const { container } = render(<ServerSelector choices={[choices[0]!]} onToggle={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });
});
