import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { CameraEventPopups } from "./CameraEventPopups";
import type { CameraEntity } from "@/lib/maps/types";
import type { Map } from "maplibre-gl";
const harness = vi.hoisted(() => ({ listener: undefined as ((f: unknown) => void) | undefined, get: vi.fn(), unsubscribe: vi.fn() }));
vi.mock("@/lib/realtime", () => ({ subscribeFrames: (fn: (f: unknown) => void) => { harness.listener = fn; return harness.unsubscribe; } }));
vi.mock("@/api/client", () => ({ api: { GET: harness.get }, unwrap: (r: { data: unknown }) => r.data }));
const camera = { id: "c", name: "Entrance", siteId: "s", position: { kind: "geo", lng: 2, lat: 1 } } as CameraEntity;
const map = { project: vi.fn(() => ({ x: 100, y: 200 })), on: vi.fn(), off: vi.fn() } as unknown as Map;
const detail = (id = "e") => ({ id, camera_id: "c", labels: ["car"], plates: ["AB123CD"], severity: "alert", has_snapshot: true });
const frame = (id = "e", extra = {}) => ({ type: "event.created", id: "transport", camera_id: "c", site_id: "s", tenant_id: "t", ts: new Date().toISOString(), data: { id }, ...extra });
const props = { map, cameras: [camera], tenantId: "t", siteId: "s", canEvents: true, canSnapshots: true };
beforeEach(() => { vi.useFakeTimers(); harness.get.mockReset(); harness.get.mockResolvedValue({ data: detail() }); harness.listener = undefined; });
afterEach(() => vi.useRealTimers());
async function emit(f = frame()) { await act(async () => { harness.listener?.(f); }); }
it("anchors an actual plate and snapshot to its placed camera for exactly five seconds", async () => {
  render(<CameraEventPopups {...props} />); await emit();
  expect(screen.getByText("AB123CD")).toBeInTheDocument();
  expect(map.project).toHaveBeenCalledWith([2, 1]);
  expect(screen.getByRole("img")).toHaveAttribute("src", "/media/v1/events/e/snapshot.jpg");
  expect(harness.get.mock.calls[0]![1].params.path.eventId).toBe("e");
  act(() => vi.advanceTimersByTime(4999)); expect(screen.getByText("AB123CD")).toBeInTheDocument();
  act(() => vi.advanceTimersByTime(1)); expect(screen.queryByText("AB123CD")).not.toBeInTheDocument();
});
it("deduplicates event IDs, ignores old frames and never extends replacement with a stale request", async () => {
  let resolve!: (r: unknown) => void;
  harness.get.mockImplementationOnce(() => new Promise(r => { resolve = r; }));
  render(<CameraEventPopups {...props} />); await emit(); await emit();
  expect(harness.get).toHaveBeenCalledTimes(1);
  harness.get.mockResolvedValue({ data: detail("new") }); await emit(frame("new"));
  await act(async () => resolve({ data: { ...detail(), plates: ["OLD"] } }));
  expect(screen.queryByText("OLD")).not.toBeInTheDocument();
  await emit(frame("historical", { ts: new Date(Date.now() - 120001).toISOString() }));
  expect(harness.get).toHaveBeenCalledTimes(2);
});
it("does not subscribe without event permission and clears requests on site change", async () => {
  const view = render(<CameraEventPopups {...props} canEvents={false} />);
  expect(harness.listener).toBeUndefined();
  view.rerender(<CameraEventPopups {...props} />); await emit();
  view.rerender(<CameraEventPopups {...props} siteId="other" />);
  expect(screen.queryByText("AB123CD")).not.toBeInTheDocument();
});
it("uses thumbnails without snapshot permission and reports image failure honestly", async () => {
  render(<CameraEventPopups {...props} canSnapshots={false} />); await emit();
  expect(screen.getByRole("img")).toHaveAttribute("src", "/api/v1/events/e/thumbnail");
  fireEvent.error(screen.getByRole("img")); expect(screen.getByText("Image unavailable")).toBeInTheDocument();
});
it("rejects unauthorized context and unplaced cameras without detail requests", async () => {
  render(<CameraEventPopups {...props} />);
  await emit(frame("wrong", { tenant_id: "other" })); await emit(frame("wrong-site", { site_id: "other" }));
  await emit(frame("unplaced", { camera_id: "absent" })); expect(harness.get).not.toHaveBeenCalled();
});
it("keeps cameras independent and replacement receives a fresh lifetime", async () => {
  const other = { ...camera, id: "other", name: "Other" };
  harness.get.mockImplementation((_path, options) => Promise.resolve({ data: { ...detail(options.params.path.eventId), camera_id: options.params.path.eventId === "other-event" ? "other" : "c" } }));
  render(<CameraEventPopups {...props} cameras={[camera, other]} />);
  await emit(); act(() => vi.advanceTimersByTime(2000));
  await emit(frame("other-event", { camera_id: "other" })); await emit(frame("replacement"));
  act(() => vi.advanceTimersByTime(3000)); expect(screen.getAllByText("AB123CD")).toHaveLength(2);
  act(() => vi.advanceTimersByTime(2000)); expect(screen.queryByText("AB123CD")).not.toBeInTheDocument();
});
it("retries detail once and grants five seconds of actual visibility and cancels after unmount", async () => {
  harness.get.mockRejectedValueOnce(new Error("not indexed yet")).mockResolvedValue({ data: detail() });
  const view = render(<CameraEventPopups {...props} />); await emit();
  await act(async () => { vi.advanceTimersByTime(500); });
  expect(screen.getByText("AB123CD")).toBeInTheDocument(); expect(harness.get).toHaveBeenCalledTimes(2);
  act(() => vi.advanceTimersByTime(4999)); expect(screen.getByText("AB123CD")).toBeInTheDocument();
  act(() => vi.advanceTimersByTime(1)); expect(screen.queryByText("AB123CD")).not.toBeInTheDocument();
  harness.get.mockRejectedValue(new Error("unavailable")); await emit(frame("next")); view.unmount();
  await act(async () => { vi.advanceTimersByTime(500); }); expect(harness.get).toHaveBeenCalledTimes(3);
});
it("ignores mismatched REST identities and skips unavailable snapshots", async () => {
  harness.get.mockResolvedValueOnce({ data: { ...detail(), camera_id: "wrong" } });
  render(<CameraEventPopups {...props} />); await emit(); expect(screen.queryByText("AB123CD")).not.toBeInTheDocument();
  harness.get.mockResolvedValue({ data: { ...detail("next"), has_snapshot: false } }); await emit(frame("next"));
  expect(screen.getByRole("img")).toHaveAttribute("src", "/api/v1/events/next/thumbnail");
});

it("preserves visible events across camera status updates and gives delayed details five visible seconds", async () => {
  let resolve!: (r: unknown) => void;
  harness.get.mockImplementationOnce(() => new Promise(r => { resolve = r; }));
  const view = render(<CameraEventPopups {...props} />); await emit();
  act(() => vi.advanceTimersByTime(1000));
  await act(async () => resolve({ data: detail() }));
  view.rerender(<CameraEventPopups {...props} cameras={[{ ...camera, status: "offline" }]} />);
  expect(screen.getByText("AB123CD")).toBeInTheDocument();
  act(() => vi.advanceTimersByTime(4999)); expect(screen.getByText("AB123CD")).toBeInTheDocument();
  act(() => vi.advanceTimersByTime(1)); expect(screen.queryByText("AB123CD")).not.toBeInTheDocument();
});

it("allows recently indexed reviews whose event start precedes publication by thirty seconds", async () => {
  render(<CameraEventPopups {...props} />);
  await emit(frame("e", { ts: new Date(Date.now() - 30000).toISOString() }));
  expect(screen.getByText("AB123CD")).toBeInTheDocument();
});
it("uses an explicit floor projection without passing normalized coordinates to MapLibre", async()=>{
 const floor={...camera,position:{kind:"floor",floorId:"f",x:.2,y:.3}} as CameraEntity;
 const project=vi.fn(()=>({x:45,y:80}));
 render(<CameraEventPopups {...props} map={null} cameras={[floor]} projectCamera={project} projectionKey="f"/>);
 await emit();expect(screen.getByText("AB123CD")).toBeInTheDocument();
 expect(project).toHaveBeenCalledWith(floor);
 expect(screen.getByRole("region",{name:"Camera event: Entrance"})).toHaveStyle({left:"45px",top:"68px"});
 act(()=>vi.advanceTimersByTime(5000));expect(screen.queryByText("AB123CD")).not.toBeInTheDocument();
});
