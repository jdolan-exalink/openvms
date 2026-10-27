import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { countPatch, mergePatch, patchFromDrift } from "@/lib/useCameraConfigChange";
import { PendingChangesBar } from "./PendingChangesBar";
import { SyncStatusBadge, type SyncState } from "./SyncStatusBadge";
import { SyncStatusPanel } from "./SyncStatusPanel";

const base = { desired_revision: 2, applied_revision: 1, last_error: "", drift: [] };

describe("SyncStatusBadge", () => {
  it.each([
    ["synced", "Sincronizado"],
    ["pending", "Cambios pendientes"],
    ["drift", "Diferencia detectada"],
    ["error", "Error"],
    ["unverifiable", "No verificable"],
  ] as Array<[SyncState, string]>)("labels %s as %s", (state, label) => {
    render(<SyncStatusBadge state={state} />);
    expect(screen.getByTestId("sync-badge")).toHaveTextContent(label);
  });
});

describe("SyncStatusPanel", () => {
  it("renders nothing when synced", () => {
    const { container } = render(<SyncStatusPanel state={{ ...base, sync_state: "synced" }} onUseDesired={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("offers the three drift actions: diff list, OpenVMS config and a disabled import", () => {
    const onUse = vi.fn();
    const drift = [{ path: "detection.enabled", desired: true, effective: false }];
    render(<SyncStatusPanel state={{ ...base, sync_state: "drift", drift }} onUseDesired={onUse} />);
    expect(screen.getByText("La cámara fue modificada externamente.")).toBeInTheDocument();
    const imp = screen.getByRole("button", { name: "Importar configuración actual" });
    expect(imp).toBeDisabled();
    expect(imp).toHaveAttribute("title", "Disponible próximamente");
    fireEvent.click(screen.getByRole("button", { name: "Ver diferencias" }));
    expect(screen.getByText("detection.enabled: OpenVMS ON · Cámara OFF")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Usar configuración de OpenVMS" }));
    expect(onUse).toHaveBeenCalledWith({ detection: { enabled: true } });
  });

  it("hides the engine error behind Ver información técnica", () => {
    render(<SyncStatusPanel state={{ ...base, sync_state: "error", last_error: "ffmpeg boom" }} onUseDesired={vi.fn()} />);
    expect(screen.queryByText("ffmpeg boom")).not.toBeVisible();
  });

  it("shows the plain message, keeps the technical text collapsed and renders the rollback offer", () => {
    render(<SyncStatusPanel state={{ ...base, sync_state: "error", last_error: "ffmpeg exited with code 1" }} plainError={{ message: "No se pudo iniciar el stream de grabación.", technical: "ffmpeg exited with code 1" }} offer={<button type="button">Restaurar configuración anterior</button>} onUseDesired={vi.fn()} />);
    expect(screen.getByText("No se pudo iniciar el stream de grabación.")).toBeInTheDocument();
    expect(screen.getByText("ffmpeg exited with code 1")).not.toBeVisible();
    expect(screen.getByRole("button", { name: "Restaurar configuración anterior" })).toBeInTheDocument();
  });
});

describe("patch helpers", () => {
  it("merges nested patches, counts leaf fields and rebuilds desired from drift", () => {
    const p = mergePatch({ detection: { enabled: true } }, { detection: { objects: ["car"] }, recording: { enabled: false } });
    expect(p).toEqual({ detection: { enabled: true, objects: ["car"] }, recording: { enabled: false } });
    expect(countPatch(p)).toBe(3);
    expect(patchFromDrift([{ path: "analytics.lpr", desired: true }, { path: "detection.enabled", desired: false }])).toEqual({ analytics: { lpr: true }, detection: { enabled: false } });
  });
});

describe("PendingChangesBar with two change sources", () => {
  it("counts both sources and opens the Frigate review from Revisar cambios, the camera one from its own button", () => {
    const [review, camera] = [vi.fn(), vi.fn()];
    render(<PendingChangesBar count={2} cameraCount={1} onDiscard={vi.fn()} onReview={review} onSave={vi.fn()} onReviewCamera={camera} />);
    expect(screen.getByText("3 cambios sin guardar")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Revisar cambios" }));
    fireEvent.click(screen.getByRole("button", { name: "Revisar cambios de la cámara" }));
    expect([review.mock.calls.length, camera.mock.calls.length]).toEqual([1, 1]);
  });

  it("shows no extra button when only the camera source has changes", () => {
    render(<PendingChangesBar count={0} cameraCount={1} onDiscard={vi.fn()} onReview={vi.fn()} onSave={vi.fn()} onReviewCamera={vi.fn()} />);
    expect(screen.getByText("1 cambio sin guardar")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Revisar cambios de la cámara" })).not.toBeInTheDocument();
  });
});
