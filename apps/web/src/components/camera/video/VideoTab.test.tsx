import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { CameraChangeProvider, useCameraChange } from "../CameraChangeProvider";
import { VideoTab } from "./VideoTab";

afterEach(() => vi.unstubAllGlobals());

const state = { desired_revision: 1, applied_revision: 1, sync_state: "synced", last_error: "", drift: [] };
const stream = (over: object = {}) => ({ roles: [], ...over });
const model = (over: { capabilities?: string[]; streams?: object; analytics?: object } = {}) => ({
  state,
  capabilities: over.capabilities ?? ["onvif"],
  streams: over.streams ?? { primary: stream({ width: 1920, height: 1080, fps: 15, codec: "h264" }), detection: stream({ width: 640, height: 360, fps: 5, codec: "h264" }) },
  analytics: over.analytics ?? {},
});

const codecs = [
  { codec: "h264", resolutions: [{ width: 1920, height: 1080 }, { width: 1280, height: 720 }], frame_rates: [], fps: { min: 1, max: 25 }, bitrate_kbps: { min: 512, max: 8192 }, gop: { min: 1, max: 100 }, bitrate_modes: ["vbr"] },
  { codec: "h265", resolutions: [{ width: 2560, height: 1440 }, { width: 1920, height: 1080 }], frame_rates: [], fps: { min: 1, max: 20 }, bitrate_kbps: { min: 512, max: 8192 }, gop: { min: 1, max: 100 }, bitrate_modes: [] },
];
const primaryOptions = (over: object = {}) => ({
  role: "primary", available: true, current: { codec: "h264", width: 1920, height: 1080, fps: 15, bitrate_kbps: 4096, gop: 30, bitrate_mode: "vbr" }, codecs,
  apply_via: { resolution: "onvif", fps: "onvif", codec: "onvif", bitrate_kbps: "onvif", gop: "onvif" }, ...over,
});

function Probe() {
  const c = useCameraChange();
  return <output data-testid="patch">{JSON.stringify(c.patch)}</output>;
}
const patchSeen = () => JSON.parse(screen.getByTestId("patch").textContent ?? "null") as unknown;

function mount(opts: { model?: object; options: object | (() => Response); legacy?: boolean; readOnlyReason?: string }) {
  vi.stubGlobal("fetch", stubApi({
    "/api/v1/cameras/c1/normalized-config": () => json(opts.model ?? model()),
    "/api/v1/cameras/c1/video-options": typeof opts.options === "function" ? (opts.options as () => Response) : () => json(opts.options),
  }));
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <CameraChangeProvider cameraId="c1">
        <VideoTab cameraId="c1" readOnlyReason={opts.readOnlyReason} legacy={opts.legacy === false ? undefined : <p>editor ffmpeg heredado</p>} />
        <Probe />
      </CameraChangeProvider>
    </QueryClientProvider>,
  );
}

const available = (streams: object[] = [primaryOptions()]) => ({ camera_id: "c1", available: true, streams });

describe("VideoTab", () => {
  it("is read-only with the reason when the user lacks cameras.config.video", async () => {
    mount({ options: available(), readOnlyReason: "Requiere el permiso cameras.config.video." });
    const card = await screen.findByRole("region", { name: "Stream principal" });
    expect(await within(card).findByLabelText(/^Códec/)).toBeDisabled();
    expect(screen.getByRole("note")).toHaveTextContent("cameras.config.video");
  });

  it("stays editable without a reason", async () => {
    mount({ options: available() });
    const card = await screen.findByRole("region", { name: "Stream principal" });
    expect(await within(card).findByLabelText(/^Códec/)).toBeEnabled();
  });

  it("shows principal and detection cards with their summary line, and no recording card", async () => {
    mount({ options: available() });
    await waitFor(() => expect(screen.getByTestId("summary-Stream principal")).toHaveTextContent("1920×1080 · 15 FPS · H.264"));
    expect(screen.getByTestId("summary-Stream de detección")).toHaveTextContent("640×360 · 5 FPS · H.264");
    expect(screen.queryByRole("region", { name: "Stream de grabación" })).not.toBeInTheDocument();
  });

  it("adds the recording card only when the model has a distinct recording stream", async () => {
    mount({ options: available(), model: model({ streams: { primary: stream({ width: 1920, height: 1080 }), recording: stream({ width: 1280, height: 720, fps: 10, codec: "h265" }) } }) });
    await waitFor(() => expect(screen.getByTestId("summary-Stream de grabación")).toHaveTextContent("1280×720 · 10 FPS · H.265"));
  });

  it("lists only the resolutions supported by the chosen codec", async () => {
    mount({ options: available() });
    const card = await screen.findByRole("region", { name: "Stream principal" });
    const res = await within(card).findByLabelText(/^Resolución/);
    expect(within(res).getAllByRole("option").map((o) => o.textContent)).toEqual(["1920×1080", "1280×720"]);
    fireEvent.change(within(card).getByLabelText(/^Códec/), { target: { value: "h265" } });
    await waitFor(() => expect(within(card).getByLabelText(/^Resolución/)).toHaveValue("1920x1080"));
    expect(within(within(card).getByLabelText(/^Resolución/)).getAllByRole("option").map((o) => o.textContent)).toEqual(["2560×1440", "1920×1080"]);
    // h265 caps fps at 20: the 15 fps current value stays valid, so only the codec is patched.
    expect(patchSeen()).toEqual({ streams: { primary: { codec: "h265" } } });
  });

  it("sends a sparse patch and announces the restart as soon as a value changes", async () => {
    mount({ options: available() });
    const card = await screen.findByRole("region", { name: "Stream principal" });
    expect(screen.queryByText("Este cambio reiniciará el stream durante aproximadamente algunos segundos.")).not.toBeInTheDocument();
    fireEvent.change(await within(card).findByLabelText(/^Resolución/), { target: { value: "1280x720" } });
    expect(await screen.findByText("Este cambio reiniciará el stream durante aproximadamente algunos segundos.")).toBeInTheDocument();
    expect(patchSeen()).toEqual({ streams: { primary: { resolution: { width: 1280, height: 720 } } } });
  });

  it("rejects values outside the reported range", async () => {
    mount({ options: available() });
    const card = await screen.findByRole("region", { name: "Stream principal" });
    const fps = await within(card).findByLabelText(/^FPS/);
    fireEvent.change(fps, { target: { value: "40" } });
    expect(await within(card).findByText("Debe estar entre 1 y 25.")).toBeInTheDocument();
    expect(patchSeen()).toEqual({});
    fireEvent.change(fps, { target: { value: "20" } });
    await waitFor(() => expect(patchSeen()).toEqual({ streams: { primary: { fps: 20 } } }));
  });

  it("keeps fields read-only when the device cannot change them", async () => {
    mount({ options: available([primaryOptions({ apply_via: { resolution: "none", fps: "onvif", codec: "none", bitrate_kbps: "none", gop: "none" } })]) });
    const card = await screen.findByRole("region", { name: "Stream principal" });
    await within(card).findByLabelText(/^FPS/);
    expect(within(card).queryByLabelText(/^Resolución/)).not.toBeInTheDocument();
    expect(within(card).queryByLabelText(/^Códec/)).not.toBeInTheDocument();
    expect(within(card).getAllByText("La cámara no permite cambiar estos valores").length).toBeGreaterThan(0);
  });

  it.each([
    ["no_onvif", "Esta cámara no expone ONVIF"],
    ["no_credentials", "Faltan las credenciales de la cámara"],
    ["unreachable", "No se pudo contactar a la cámara"],
    ["unsupported", "La cámara no permite cambiar estos valores"],
  ])("is read-only with reason %s", async (reason, text) => {
    mount({ options: { camera_id: "c1", available: false, reason, streams: [] } });
    const card = await screen.findByRole("region", { name: "Stream principal" });
    expect(await within(card).findByText(`${text}.`)).toBeInTheDocument();
    expect(within(card).queryByRole("combobox")).not.toBeInTheDocument();
    expect(within(card).queryByRole("spinbutton")).not.toBeInTheDocument();
  });

  it("explains a failure in plain Spanish with technical information on demand", async () => {
    mount({ options: () => new Response(JSON.stringify({ detail: "ffmpeg exited with code 1" }), { status: 500, headers: { "content-type": "application/json" } }) });
    expect(await screen.findByText("No se pudieron leer las opciones de video de la cámara.")).toBeInTheDocument();
    expect(screen.getByText("Ver información técnica")).toBeInTheDocument();
  });

  it("hides Audio without the capability and shows it with it", async () => {
    mount({ options: available() });
    await screen.findByRole("region", { name: "Stream principal" });
    expect(screen.queryByRole("region", { name: "Audio" })).not.toBeInTheDocument();
  });

  it("shows Audio when the camera has audio", async () => {
    mount({ options: available(), model: model({ capabilities: ["onvif", "audio"], analytics: { audio: true } }) });
    const audio = await screen.findByRole("region", { name: "Audio" });
    expect(within(audio).getByText("Activada")).toBeInTheDocument();
    expect(within(audio).getByText(/todavía no se pueden editar/)).toBeInTheDocument();
  });

  it("keeps the legacy Frigate video editors under a collapsed disclosure", async () => {
    mount({ options: available() });
    const summary = await screen.findByText("Avanzado de video (Frigate)");
    const details = summary.closest("details")!;
    expect(details.open).toBe(false);
    expect(within(details).getByText("editor ffmpeg heredado")).toBeInTheDocument();
  });
});
