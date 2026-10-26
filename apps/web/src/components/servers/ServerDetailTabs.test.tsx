import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ServerDetailTabs } from "./ServerDetailTabs";

const tabs = [
  { id: "summary", label: "Resumen", content: <p>panel resumen</p> },
  { id: "connection", label: "Conexión", content: <p>panel conexión</p> },
  { id: "actions", label: "Acciones", content: <p>panel acciones</p> },
];

describe("ServerDetailTabs", () => {
  it("selects the first tab by default and exposes tab roles", () => {
    render(<ServerDetailTabs label="Secciones" tabs={tabs} />);
    expect(screen.getByRole("tablist", { name: "Secciones" })).toBeInTheDocument();
    const [first, second] = screen.getAllByRole("tab");
    expect(first).toHaveAttribute("aria-selected", "true");
    expect(second).toHaveAttribute("aria-selected", "false");
    const panel = screen.getByRole("tabpanel");
    expect(panel).toHaveTextContent("panel resumen");
    expect(panel).toHaveAttribute("aria-labelledby", first!.id);
    expect(first).toHaveAttribute("aria-controls", panel.id);
    expect(screen.queryByText("panel conexión")).not.toBeInTheDocument();
  });

  it("switches panels on click", () => {
    render(<ServerDetailTabs label="Secciones" tabs={tabs} />);
    fireEvent.click(screen.getByRole("tab", { name: "Acciones" }));
    expect(screen.getByRole("tabpanel")).toHaveTextContent("panel acciones");
    expect(screen.getByRole("tab", { name: "Acciones" })).toHaveAttribute("aria-selected", "true");
  });

  it("moves between tabs with the arrow keys, wrapping around, plus Home and End", () => {
    render(<ServerDetailTabs label="Secciones" tabs={tabs} />);
    const tab = (name: string) => screen.getByRole("tab", { name });
    fireEvent.keyDown(tab("Resumen"), { key: "ArrowRight" });
    expect(tab("Conexión")).toHaveAttribute("aria-selected", "true");
    expect(tab("Conexión")).toHaveFocus();
    fireEvent.keyDown(tab("Conexión"), { key: "ArrowLeft" });
    fireEvent.keyDown(tab("Resumen"), { key: "ArrowLeft" });
    expect(tab("Acciones")).toHaveAttribute("aria-selected", "true");
    fireEvent.keyDown(tab("Acciones"), { key: "ArrowRight" });
    expect(tab("Resumen")).toHaveAttribute("aria-selected", "true");
    fireEvent.keyDown(tab("Resumen"), { key: "End" });
    expect(tab("Acciones")).toHaveAttribute("aria-selected", "true");
    fireEvent.keyDown(tab("Acciones"), { key: "Home" });
    expect(tab("Resumen")).toHaveAttribute("aria-selected", "true");
  });

  it("keeps only the selected tab in the tab order", () => {
    render(<ServerDetailTabs label="Secciones" tabs={tabs} />);
    expect(screen.getByRole("tab", { name: "Resumen" })).toHaveAttribute("tabindex", "0");
    expect(screen.getByRole("tab", { name: "Conexión" })).toHaveAttribute("tabindex", "-1");
  });
});
