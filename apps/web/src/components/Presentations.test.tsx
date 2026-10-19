import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PresentationEditor } from "./Presentations";
import { DEFAULT_PRESENTATIONS, exportPresentations, uniformPresentation, type Presentation } from "@/lib/presentations";

const custom: Presentation = { ...uniformPresentation(2, 2, "c-1"), name: "Plaza", builtin: false };

function open(catalog: Presentation[], onAccept = vi.fn()) {
  render(<PresentationEditor catalog={catalog} active={{ columns: 2, panes: custom.panes }} onCancel={() => {}} onAccept={onAccept} />);
  return onAccept;
}

afterEach(() => vi.restoreAllMocks());

describe("PresentationEditor export and import", () => {
  it("disables export when there are no custom presentations", () => {
    open(DEFAULT_PRESENTATIONS);
    expect(screen.getByRole("button", { name: "Exportar vistas" })).toBeDisabled();
  });

  it("downloads the custom presentations as JSON", () => {
    let blob: Blob | undefined;
    URL.createObjectURL = vi.fn((b: Blob) => ((blob = b), "blob:x"));
    URL.revokeObjectURL = vi.fn();
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    open([...DEFAULT_PRESENTATIONS, custom]);
    fireEvent.click(screen.getByRole("button", { name: "Exportar vistas" }));
    expect(click).toHaveBeenCalledTimes(1);
    expect(blob).toBeDefined();
  });

  it("imports a file into the draft, reports the counts and accepts the merged catalog", async () => {
    const onAccept = open(DEFAULT_PRESENTATIONS);
    const file = new File([exportPresentations([custom, { ...custom, id: "p1" }])], "vistas.json", { type: "application/json" });
    fireEvent.change(screen.getByLabelText("Importar vistas"), { target: { files: [file] } });
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Importadas: 1"));
    expect(screen.getByRole("status")).toHaveTextContent("Rechazadas: 1");
    fireEvent.click(screen.getByRole("button", { name: "Aceptar" }));
    const accepted = onAccept.mock.calls[0]?.[0] as Presentation[];
    expect(accepted.filter((item) => !item.builtin).map((item) => item.name)).toEqual(["Plaza"]);
  });

  it("reports a file that is not a presentations export", async () => {
    open(DEFAULT_PRESENTATIONS);
    fireEvent.change(screen.getByLabelText("Importar vistas"), { target: { files: [new File(["nope"], "x.json")] } });
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("no es un archivo de vistas"));
  });
});
