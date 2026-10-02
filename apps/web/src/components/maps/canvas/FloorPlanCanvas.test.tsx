import { render, screen, fireEvent, createEvent } from "@testing-library/react";
import { expect, it, vi, beforeEach, afterEach } from "vitest";
import { FloorPlanCanvas } from "./FloorPlanCanvas";
import type { CameraEntity } from "@/lib/maps/types";
beforeEach(() => vi.stubGlobal("ResizeObserver", class {
  observe() { }
  disconnect() { }
}));
afterEach(() => vi.unstubAllGlobals());
const cams = ["online", "offline", "unknown"].map((status, n) => ({ id: `c${n}`, name: `Camera ${n}`, siteId: "s", metadata: {}, status, activeAlarms: n === 0 ? 2 : 0, position: { kind: "floor", floorId: "f", x: .25 + n * .1, y: .4 } })) as CameraEntity[];
it("keeps every state visible with canonical fixed-size icons above the image", () => {
  render(<FloorPlanCanvas imageUrl="blob:plan" width={400} height={200} cameras={cams} editable={false} onPlace={vi.fn()} onSelect={vi.fn()}/>);
  expect(screen.getAllByRole("button", { name: /Camera \d/ })).toHaveLength(3);
  expect(screen.getByRole("img", { name: "Map background" })).toHaveClass("pointer-events-none");
  expect(screen.getByRole("button", { name: "Camera 1" })).toHaveAttribute("data-connection", "offline");
  expect(screen.getByText("2")).toBeInTheDocument();
});
it("accepts direct tray drop only on the canvas, with normalized coordinates", () => {
  const place = vi.fn();
  render(<FloorPlanCanvas width={400} height={200} cameras={[]} editable onPlace={place} onSelect={vi.fn()}/>);
  const viewport = screen.getByTestId("floor-viewport");
  vi.spyOn(viewport, "getBoundingClientRect").mockReturnValue({ left: 0, top: 0, width: 400, height: 200 } as DOMRect);
  const drop = createEvent.drop(viewport, { dataTransfer: { getData: () => "c", types: ["application/x-openvms-map-camera"] } });
  Object.defineProperties(drop, { clientX: { value: 100 }, clientY: { value: 100 } });
  fireEvent(viewport, drop);
  expect(place).toHaveBeenCalledWith("c", { x: .25, y: .5 });
});
it("shows a server-outage camera in gray with X even if its last camera status is online", () => {
  const camera = { ...cams[0]!, metadata: { serverOffline: true }, activeAlarms: 3 };
  render(<FloorPlanCanvas width={400} height={200} cameras={[camera]} editable={false} onPlace={vi.fn()} onSelect={vi.fn()}/>);
  const marker = screen.getByRole("button", { name: "Camera 0" });
  expect(marker).toHaveClass("border-muted", "text-muted");
  expect(marker).not.toHaveClass("text-ok");
  expect(marker).toHaveAttribute("data-connection", "unreachable");
  expect(screen.getByLabelText("Unavailable")).toBeInTheDocument();
  expect(screen.getByText("3")).toBeInTheDocument();
});
