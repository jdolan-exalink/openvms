import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ServerOption } from "@/lib/serverSelector";
import { ServerSelector } from "./ServerSelector";

const option = (id: string, name: string, siteName: string, cameras: number, checked: boolean): ServerOption => ({ id, name, siteName, cameras, checked });
const options = [
  option("srv-x", "Anexo uno", "Anexo", 4, true),
  option("srv-a", "Alfa", "Campus", 3, true),
  option("srv-b", "Beta", "Campus", 5, false),
];

describe("ServerSelector", () => {
  it("is a closed drop-down that says how many servers are selected", () => {
    render(<ServerSelector options={options} onToggle={() => {}} />);
    const button = screen.getByRole("button", { name: /Servidores/ });
    expect(button).toHaveAttribute("aria-expanded", "false");
    expect(button).toHaveTextContent("2/3");
    expect(screen.queryByRole("group", { name: /Servidores seleccionados/i })).not.toBeInTheDocument();
  });

  it("opens a list with one check per server, grouped by site, with its number of cameras", () => {
    render(<ServerSelector options={options} onToggle={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /Servidores/ }));
    const list = screen.getByRole("group", { name: /Servidores seleccionados/i });
    expect(within(list).getAllByRole("checkbox").map((b) => (b as HTMLInputElement).checked)).toEqual([true, true, false]);
    expect(within(list).getByRole("checkbox", { name: /Alfa/ })).toBeChecked();
    expect(within(list).getByRole("checkbox", { name: /Beta/ })).not.toBeChecked();
    expect(within(list).getByText("Anexo")).toBeInTheDocument();
    expect(within(list).getByText("Campus")).toBeInTheDocument();
    expect(within(list).getByText("5")).toBeInTheDocument();
  });

  it("reports what was toggled and stays open so several servers can be changed", () => {
    const onToggle = vi.fn();
    render(<ServerSelector options={options} onToggle={onToggle} />);
    fireEvent.click(screen.getByRole("button", { name: /Servidores/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Beta/ }));
    expect(onToggle).toHaveBeenCalledWith("srv-b", true);
    fireEvent.click(screen.getByRole("checkbox", { name: /Alfa/ }));
    expect(onToggle).toHaveBeenLastCalledWith("srv-a", false);
    expect(screen.getByRole("group", { name: /Servidores seleccionados/i })).toBeInTheDocument();
  });

  it("will not let the last selected server be unchecked", () => {
    const onToggle = vi.fn();
    const onlyAlfa = [option("srv-a", "Alfa", "Campus", 3, true), option("srv-b", "Beta", "Campus", 5, false)];
    render(<ServerSelector options={onlyAlfa} onToggle={onToggle} />);
    fireEvent.click(screen.getByRole("button", { name: /Servidores/ }));
    const alfa = screen.getByRole("checkbox", { name: /Alfa/ });
    expect(alfa).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: /Beta/ })).toBeEnabled();
    fireEvent.click(alfa);
    expect(onToggle).not.toHaveBeenCalled();
  });

  it("closes with Escape and with a click outside", () => {
    render(<div><ServerSelector options={options} onToggle={() => {}} /><p>afuera</p></div>);
    const open = () => fireEvent.click(screen.getByRole("button", { name: /Servidores/ }));
    open();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("group", { name: /Servidores seleccionados/i })).not.toBeInTheDocument();
    open();
    fireEvent.mouseDown(screen.getByText("afuera"));
    expect(screen.queryByRole("group", { name: /Servidores seleccionados/i })).not.toBeInTheDocument();
  });

  it("is not worth showing with a single server", () => {
    const { container } = render(<ServerSelector options={[options[0]!]} onToggle={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });
});
