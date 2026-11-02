import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { HealthTiles, healthTiles } from "./HealthTiles";

const camera = (over: Partial<Schemas["Camera"]> = {}) => ({ id: "c1", display_name: "acceso", enabled: true, status: "online", tags: [], ...over }) as Schemas["Camera"];

describe("HealthTiles", () => {
  it("builds the tiles from the camera row only", () => {
    const tiles = healthTiles(camera({ fps: 14.6, live_codec: "h264", onvif_endpoint: "http://10.0.0.2/onvif", onvif_profile: "ONVIF-T" }));
    expect(tiles.map((t) => [t.label, t.value])).toEqual([
      ["Estado", "En línea"],
      ["Cuadros por segundo", "15 fps"],
      ["Códec", "H264"],
      ["ONVIF", "ONVIF-T"],
    ]);
  });

  it("shows a dash for unknown data and marks a disabled camera", () => {
    const tiles = healthTiles(camera({ enabled: false, status: "unknown" }));
    expect(tiles.find((t) => t.id === "status")?.value).toBe("Deshabilitada");
    expect(tiles.find((t) => t.id === "fps")?.value).toBe("—");
    expect(tiles.find((t) => t.id === "onvif")?.value).toBe("No configurado");
  });

  it("renders nothing without a camera and a labelled list with one", () => {
    const { rerender } = render(<HealthTiles camera={undefined} />);
    expect(screen.queryByRole("list", { name: "Salud de la cámara" })).not.toBeInTheDocument();
    rerender(<HealthTiles camera={camera()} />);
    expect(within(screen.getByRole("list", { name: "Salud de la cámara" })).getByText("En línea")).toBeInTheDocument();
  });
});
