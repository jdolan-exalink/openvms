import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { applyTheme } from "@/lib/theme";
import { ThemePicker } from "./ThemePicker";

beforeEach(() => {
  localStorage.clear();
  applyTheme("ristretto");
});

describe("ThemePicker", () => {
  it("renders a radiogroup with three named themes", () => {
    render(<ThemePicker />);
    expect(screen.getByRole("radiogroup", { name: "Tema" })).toBeInTheDocument();
    const radios = screen.getAllByRole("radio");
    expect(radios.map((r) => r.textContent)).toEqual(["Ristretto", "Drácula", "Claro"]);
  });

  it("reflects the current theme in aria-checked", () => {
    applyTheme("dracula");
    render(<ThemePicker />);
    expect(screen.getByRole("radio", { name: "Drácula" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("radio", { name: "Ristretto" })).toHaveAttribute("aria-checked", "false");
  });

  it("selects on click, applies the theme and persists it", () => {
    render(<ThemePicker />);
    fireEvent.click(screen.getByRole("radio", { name: "Claro" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(localStorage.getItem("openvms.theme")).toBe("light");
    expect(screen.getByRole("radio", { name: "Claro" })).toHaveAttribute("aria-checked", "true");
  });

  it("uses roving tabindex and arrow keys to move focus and selection", () => {
    render(<ThemePicker compact />);
    const [ristretto, dracula, light] = screen.getAllByRole("radio") as [HTMLElement, HTMLElement, HTMLElement];
    expect(ristretto).toHaveAttribute("tabindex", "0");
    expect(dracula).toHaveAttribute("tabindex", "-1");
    ristretto.focus();
    fireEvent.keyDown(ristretto, { key: "ArrowRight" });
    expect(document.documentElement.dataset.theme).toBe("dracula");
    expect(dracula).toHaveFocus();
    fireEvent.keyDown(dracula, { key: "End" });
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(light).toHaveFocus();
    fireEvent.keyDown(light, { key: "ArrowRight" });
    expect(document.documentElement.dataset.theme).toBe("ristretto");
    fireEvent.keyDown(ristretto, { key: "ArrowLeft" });
    expect(document.documentElement.dataset.theme).toBe("light");
    fireEvent.keyDown(light, { key: "Home" });
    expect(document.documentElement.dataset.theme).toBe("ristretto");
  });
});
