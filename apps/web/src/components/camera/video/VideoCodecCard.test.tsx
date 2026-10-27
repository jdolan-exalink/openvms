import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { VideoCodecCard } from "./VideoCodecCard";

afterEach(() => vi.unstubAllGlobals());

type Sent = { path: string; body: Record<string, unknown> };

const config = (over: object = {}) => ({
  token: "vec1", name: "Principal", encoding: "H264", width: 1920, height: 1080, fps: 15, bitrate: 4096, supported_encodings: ["H264", "H265"], ...over,
});

/** Serves the ONVIF video-encoding route; `set` answers with `setResult` and flips the listed encoding. */
function stub(opts: { options?: object; setResult?: object; setStatus?: number } = {}) {
  const sent: Sent[] = [];
  let encoding = "H264";
  vi.stubGlobal("fetch", async (req: Request) => {
    const path = new URL(req.url).pathname;
    const body = JSON.parse(await req.text()) as Record<string, unknown>;
    sent.push({ path, body });
    if (!path.endsWith("/onvif/video-encoding")) return json({ code: "not_found", message: "x" }, 404);
    if (body.action === "options") return json(opts.options ?? { success: true, supported: true, configurations: [config({ encoding })] });
    encoding = "H265";
    return json(opts.setResult ?? { success: true, supported: true, previous_encoding: "H264", encoding: "H265" }, opts.setStatus ?? 200);
  });
  return sent;
}

const mount = (props: { readOnlyReason?: string } = {}) =>
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <VideoCodecCard cameraId="c1" {...props} />
    </QueryClientProvider>,
  );

const read = async () => {
  fireEvent.click(screen.getByRole("button", { name: "Leer códecs de la cámara" }));
  await screen.findByText("Principal");
};

describe("VideoCodecCard", () => {
  it("reads the camera options without sending credentials and shows each configuration", async () => {
    const sent = stub();
    mount();
    await read();
    expect(sent).toEqual([{ path: "/api/v1/cameras/c1/onvif/video-encoding", body: { action: "options" } }]);
    expect(screen.getByText("H.264 · 1920×1080 · 15 fps · 4096 kbps")).toBeInTheDocument();
    const select = screen.getByLabelText("Nuevo códec de Principal");
    expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual(["H.265"]);
  });

  it("changes the encoding only after confirming, sends the listed target, and reports the result", async () => {
    const sent = stub();
    mount();
    await read();
    fireEvent.click(screen.getByRole("button", { name: "Cambiar códec" }));
    expect(sent.filter((s) => s.body.action === "set")).toHaveLength(0);
    expect(screen.getByText(/pasará de H\.264 a H\.265/)).toBeInTheDocument();
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cambiar códec" }));
    await screen.findByText(/cambiado de H\.264 a H\.265/);
    expect(sent.filter((s) => s.body.action === "set").map((s) => s.body)).toEqual([{ action: "set", config_token: "vec1", encoding: "H265" }]);
    // The list is read again so the new encoding shows.
    await waitFor(() => expect(sent.filter((s) => s.body.action === "options")).toHaveLength(2));
  });

  it("does not send anything when the confirmation is cancelled", async () => {
    const sent = stub();
    mount();
    await read();
    fireEvent.click(screen.getByRole("button", { name: "Cambiar códec" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: /cancel/i }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(sent.filter((s) => s.body.action === "set")).toHaveLength(0);
  });

  it("explains a refused change using the API code and keeps the list as it was", async () => {
    const sent = stub({ setResult: { success: false, supported: true, code: "encoding_unsupported" } });
    mount();
    await read();
    fireEvent.click(screen.getByRole("button", { name: "Cambiar códec" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cambiar códec" }));
    await screen.findByText("La cámara no lista ese códec para esta configuración.");
    expect(sent.filter((s) => s.body.action === "options")).toHaveLength(1);
  });

  it("says so when the camera has no Media2 service", async () => {
    stub({ options: { success: false, supported: false, code: "media2_unsupported" } });
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Leer códecs de la cámara" }));
    await screen.findByText(/no tiene el servicio ONVIF Media2/);
    expect(screen.queryByRole("button", { name: "Cambiar códec" })).not.toBeInTheDocument();
  });

  it("offers no change when the camera lists only the current encoding", async () => {
    stub({ options: { success: true, supported: true, configurations: [config({ supported_encodings: ["H264"] })] } });
    mount();
    await read();
    expect(screen.getByText("No hay otro códec disponible.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cambiar códec" })).not.toBeInTheDocument();
  });

  it("is read-only with a reason and never contacts the camera", () => {
    const sent = stub();
    mount({ readOnlyReason: "Requiere permisos." });
    expect(screen.getByText(/Solo lectura\. Requiere permisos\./)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Leer códecs de la cámara" })).toBeDisabled();
    expect(sent).toHaveLength(0);
  });

  it("shows the API error when reading fails", async () => {
    vi.stubGlobal("fetch", async () => json({ code: "frigate_unreachable", message: "La cámara no responde." }, 502));
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Leer códecs de la cámara" }));
    await screen.findByText(/No se pudieron leer las opciones: La cámara no responde\./);
  });
});
