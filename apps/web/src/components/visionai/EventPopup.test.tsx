import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { VisionEventView } from "@/lib/visionEvents";
import { EventPopup } from "./EventPopup";

const view = (extra: Partial<VisionEventView> = {}): VisionEventView => ({
  key: "slot:0:ev-1", source: "slot", cameraName: "Acceso", object: "car", confidence: 0.91, zone: "entrada",
  plate: "AB123CD", at: "2026-10-21T10:00:00Z", images: ["/media/v1/events/ev-1/snapshot.jpg", "/api/v1/events/ev-1/thumbnail"], ...extra,
});

describe("EventPopup", () => {
  it("shows the event data and its plate", () => {
    render(<EventPopup event={view()} onClose={() => {}} />);
    const dialog = screen.getByRole("dialog", { name: /Detalle del evento/i });
    expect(dialog).toHaveTextContent("Acceso");
    expect(dialog).toHaveTextContent("car");
    expect(dialog).toHaveTextContent("91%");
    expect(dialog).toHaveTextContent("entrada");
    expect(screen.getByLabelText("Patente AB123CD")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /Acceso/ })).toHaveAttribute("src", "/media/v1/events/ev-1/snapshot.jpg");
  });

  it("omits the plate block when the event has no plate", () => {
    render(<EventPopup event={view({ plate: undefined })} onClose={() => {}} />);
    expect(screen.queryByLabelText(/^Patente /)).not.toBeInTheDocument();
  });

  it("falls through the image list when one fails, then to a placeholder", () => {
    render(<EventPopup event={view()} onClose={() => {}} />);
    fireEvent.error(screen.getByRole("img", { name: /Acceso/ }));
    expect(screen.getByRole("img", { name: /Acceso/ })).toHaveAttribute("src", "/api/v1/events/ev-1/thumbnail");
    fireEvent.error(screen.getByRole("img", { name: /Acceso/ }));
    expect(screen.queryByRole("img", { name: /Acceso/ })).not.toBeInTheDocument();
    expect(screen.getByTestId("event-popup-placeholder")).toBeInTheDocument();
  });

  it("names the recognized person of a face event", () => {
    render(<EventPopup event={view({ plate: undefined, object: "face", person: "Ana" })} onClose={() => {}} />);
    expect(screen.getByRole("dialog")).toHaveTextContent("Ana");
  });

  it("closes with the button and with Escape", () => {
    const onClose = vi.fn();
    render(<EventPopup event={view()} onClose={onClose} />);
    fireEvent.click(screen.getByRole("button", { name: /Cerrar evento/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("offers to pin an active slot in the focus view only when it can", () => {
    const onPin = vi.fn();
    const slot = { slot: 0, camera_id: "cam-1", pinned: false };
    const { rerender } = render(<EventPopup event={view({ slot })} onClose={() => {}} onPin={onPin} />);
    fireEvent.click(screen.getByRole("button", { name: /Fijar en foco/i }));
    expect(onPin).toHaveBeenCalledTimes(1);
    rerender(<EventPopup event={view({ slot: undefined })} onClose={() => {}} onPin={onPin} />);
    expect(screen.queryByRole("button", { name: /Fijar en foco/i })).not.toBeInTheDocument();
  });
});
