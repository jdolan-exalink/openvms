import { screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { DetectionChannel } from "./DetectionChannel";

const NOW = Date.parse("2026-10-21T10:00:10Z");

const read = (id: string, plate: string, seen_at: string) => ({
  id, site_id: "s", site_name: "Sitio", server_id: "sv", server_name: "Srv", camera_id: "c1", camera_name: "Acceso Norte",
  plate, plate_normalized: plate, label: "car", zones: ["entrada"], seen_at,
});

const faceEvent = (id: string, start_time: string, sub_labels: string[] = [], has_thumbnail = true) => ({
  id, tenant_id: "t", site_id: "s", site_name: "Sitio", server_id: "sv", server_name: "Srv", camera_id: "c2", camera_name: "Hall",
  remote_id: `r-${id}`, severity: "detection", labels: ["face"], sub_labels, zones: ["recepcion"], plates: [], start_time,
  reviewed: false, has_thumbnail, has_snapshot: false, has_preview: false, tracks: [],
});

let reads: unknown[];
let events: unknown[];
let requests: URL[];

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
  reads = [];
  events = [];
  requests = [];
  const routes = stubApi({
    "/api/v1/lpr/reads": () => json({ items: reads }),
    "/api/v1/events": () => json({ items: events }),
  });
  vi.stubGlobal("fetch", vi.fn(async (input: Request) => {
    requests.push(new URL(input.url));
    return routes(input);
  }));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("DetectionChannel", () => {
  it("shows the latest plate large with its age and the previous ones in a strip", async () => {
    reads = [read("a", "AB123CD", "2026-10-21T09:59:50Z"), read("b", "AC456EF", "2026-10-21T10:00:06Z")];
    renderPage(() => <DetectionChannel kind="lpr" />);

    const latest = await screen.findByTestId("channel-latest");
    expect(within(latest).getByText("Acceso Norte")).toBeInTheDocument();
    // The age sits in the channel header, next to the title.
    expect(within(screen.getByTestId("detection-channel")).getByText("hace 4s")).toBeInTheDocument();
    expect(latest.querySelector("img")?.getAttribute("src")).toContain("/media/v1/lpr/reads/b/snapshot.jpg");
    const strip = screen.getByTestId("channel-history");
    expect(within(strip).getAllByTestId("channel-history-item")).toHaveLength(1);
    expect(requests.find((u) => u.pathname === "/api/v1/lpr/reads")?.searchParams.get("limit")).toBe("9");
  });

  it("names recognized faces and labels the rest as unknown", async () => {
    events = [faceEvent("e1", "2026-10-21T10:00:00Z"), faceEvent("e2", "2026-10-21T10:00:08Z", ["Juan Pérez"])];
    renderPage(() => <DetectionChannel kind="faces" />);

    const latest = await screen.findByTestId("channel-latest");
    expect(within(latest).getByText("Juan Pérez")).toBeInTheDocument();
    expect(within(screen.getByTestId("channel-history")).getByText("Desconocido")).toBeInTheDocument();
    expect(requests.find((u) => u.pathname === "/api/v1/events")?.searchParams.getAll("label")).toEqual(["face"]);
  });

  it("shows a placeholder instead of a broken image when there is no thumbnail", async () => {
    events = [faceEvent("e1", "2026-10-21T10:00:08Z", [], false)];
    renderPage(() => <DetectionChannel kind="faces" />);
    const latest = await screen.findByTestId("channel-latest");
    expect(latest.querySelector("img")).toBeNull();
    expect(within(latest).getByTestId("channel-image-placeholder")).toBeInTheDocument();
  });

  it("shows an empty state until the first detection", async () => {
    renderPage(() => <DetectionChannel kind="lpr" />);
    expect(await screen.findByText("Esperando lecturas de patentes")).toBeInTheDocument();
  });
});
