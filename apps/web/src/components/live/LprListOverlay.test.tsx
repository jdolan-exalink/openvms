import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { LprListOverlay } from "./LprListOverlay";

const read = (id: string, plate: string, seen_at: string, camera_id = "cam-1") => ({
  id, site_id: "s", site_name: "Sitio", server_id: "sv", server_name: "Srv", camera_id, camera_name: "Acceso Norte",
  plate, plate_normalized: plate, label: "car", zones: ["entrada"], seen_at,
});

let reads: unknown[];
let requests: URL[];

beforeEach(() => {
  reads = [];
  requests = [];
  localStorage.clear();
  const routes = stubApi({ "/api/v1/lpr/reads": () => json({ items: reads }) });
  vi.stubGlobal("fetch", vi.fn(async (input: Request) => {
    requests.push(new URL(input.url));
    return routes(input);
  }));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("LprListOverlay", () => {
  it("asks only for the plates of its camera", async () => {
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    await waitFor(() => expect(requests.length).toBeGreaterThan(0));
    expect(requests[0]?.searchParams.getAll("camera_id")).toEqual(["cam-1"]);
  });

  it("lists the newest read first with its plate and time", async () => {
    reads = [
      read("r1", "AB123CD", "2026-10-21T10:00:00Z"),
      read("r2", "AE526KV", "2026-10-21T10:00:30Z"),
    ];
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    const list = await screen.findByRole("list", { name: /Lecturas de patentes/i });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent("AE526KV");
    expect(items[1]).toHaveTextContent("AB123CD");
    expect(items[0]).toHaveAttribute("data-latest", "true");
    expect(items[1]).not.toHaveAttribute("data-latest");
  });

  it("shows a waiting state when there are no reads", async () => {
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    expect(await screen.findByText(/Esperando patentes/i)).toBeInTheDocument();
  });

  it("does not intercept pointer events over the video", async () => {
    reads = [read("r1", "AB123CD", "2026-10-21T10:00:00Z")];
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    const overlay = await screen.findByTestId("lpr-list-overlay");
    expect(overlay).toHaveClass("pointer-events-none");
  });

  it("can be collapsed with the toggle and remembers the choice", async () => {
    reads = [read("r1", "AB123CD", "2026-10-21T10:00:00Z")];
    const first = renderPage(() => <LprListOverlay cameraId="cam-1" />);
    expect(await screen.findByRole("list", { name: /Lecturas de patentes/i })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Ocultar lista de patentes/i }));
    expect(screen.queryByRole("list", { name: /Lecturas de patentes/i })).not.toBeInTheDocument();
    first.unmount();

    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    expect(await screen.findByRole("button", { name: /Mostrar lista de patentes/i })).toBeInTheDocument();
    expect(screen.queryByRole("list", { name: /Lecturas de patentes/i })).not.toBeInTheDocument();
  });

  it("renders nothing when disabled by the caller", () => {
    renderPage(() => <LprListOverlay cameraId="cam-1" enabled={false} />);
    expect(screen.queryByTestId("lpr-list-overlay")).not.toBeInTheDocument();
    expect(requests).toHaveLength(0);
  });
});
