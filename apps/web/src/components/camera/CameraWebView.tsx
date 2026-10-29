import { useMutation } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api, ApiError, unwrap } from "@/api/client";
import { Button, Select, TextInput } from "@/components/ui";

type Scheme = "http" | "https";

const PRESETS: { value: string; label: string; scheme: Scheme; port: number }[] = [
  { value: "http:80", label: "HTTP 80 (predeterminado)", scheme: "http", port: 80 },
  { value: "https:443", label: "HTTPS 443", scheme: "https", port: 443 },
  { value: "http:8080", label: "HTTP 8080", scheme: "http", port: 8080 },
  { value: "http:8000", label: "HTTP 8000", scheme: "http", port: 8000 },
];

/**
 * The iframe sandbox. It must never include allow-same-origin: the camera page is an arbitrary
 * device web app and, without that flag, it runs in an opaque origin and cannot act as OpenVMS.
 * The server enforces the same with a CSP sandbox header on every proxied response.
 */
export const CAMERA_WEB_SANDBOX = "allow-scripts allow-forms allow-popups allow-modals";

export function cameraWebErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 409) {
      return err.code === "agent_unsupported"
        ? "El agente de este servidor es antiguo y no puede abrir la web de la cámara. Actualizá el agente."
        : "El servidor de esta cámara no tiene un agente registrado, y la web de la cámara se abre a través del agente.";
    }
    if (err.status === 404) {
      return "No se conoce la dirección de la cámara. Vinculá primero su endpoint ONVIF (debe ser una IP privada).";
    }
    if (err.status === 502) {
      return "El agente no pudo conectar con la web de la cámara. Revisá la dirección y el puerto de la cámara y que el agente tenga alcance de red hasta ella.";
    }
    if (err.status === 403) return "No tenés permiso para abrir la web de esta cámara.";
    return err.message;
  }
  return err instanceof Error ? err.message : String(err);
}

/** CameraWebView shows the camera's own web interface, reached through the edge agent. */
export function CameraWebView({ cameraId, autoOpen = false }: { cameraId: string; autoOpen?: boolean }) {
  const [preset, setPreset] = useState("http:80");
  const [customScheme, setCustomScheme] = useState<Scheme>("http");
  const [customPort, setCustomPort] = useState("");
  const [reloadKey, setReloadKey] = useState(0);

  const choice = (): { scheme: Scheme; port: number } | null => {
    if (preset !== "custom") {
      const p = PRESETS.find((x) => x.value === preset);
      return p ? { scheme: p.scheme, port: p.port } : null;
    }
    const port = Number(customPort);
    return Number.isInteger(port) && port >= 1 && port <= 65535 ? { scheme: customScheme, port } : null;
  };
  const selected = choice();

  const open = useMutation({
    mutationFn: async () => {
      const body = choice();
      if (!body) throw new Error("Indicá un puerto entre 1 y 65535.");
      return unwrap(await api.POST("/api/v1/cameras/{cameraId}/web/session", { params: { path: { cameraId } }, body }));
    },
  });

  useEffect(() => {
    if (autoOpen && open.isIdle) {
      open.mutate();
    }
  }, [autoOpen, open.isIdle]);

  return (
    <section aria-label="Web de la cámara" className="grid gap-3">
      <div className="flex flex-wrap items-end gap-3">
        <label className="grid gap-1 text-sm">
          <span>Puerto de la web de la cámara</span>
          <Select aria-label="Puerto de la web de la cámara" value={preset} onChange={(e) => setPreset(e.target.value)}>
            {PRESETS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
            <option value="custom">Otro puerto…</option>
          </Select>
        </label>
        {preset === "custom" && (
          <>
            <label className="grid gap-1 text-sm">
              <span>Protocolo</span>
              <Select aria-label="Protocolo" value={customScheme} onChange={(e) => setCustomScheme(e.target.value as Scheme)}>
                <option value="http">http</option>
                <option value="https">https</option>
              </Select>
            </label>
            <label className="grid gap-1 text-sm">
              <span>Puerto</span>
              <TextInput aria-label="Puerto personalizado" inputMode="numeric" value={customPort} onChange={(e) => setCustomPort(e.target.value)} />
            </label>
          </>
        )}
        <Button type="button" disabled={open.isPending || !selected} onClick={() => open.mutate()}>
          {open.isPending ? "Abriendo…" : "Abrir"}
        </Button>
      </div>

      {open.isPending && <p role="status">Conectando con la cámara a través del agente…</p>}
      {open.isError && (
        <p role="alert" className="rounded-m3-lg bg-bad/12 px-4 py-3 text-sm break-words text-bad">
          {cameraWebErrorMessage(open.error)}
        </p>
      )}

      {open.data && !open.isPending && (
        <div className="grid gap-2">
          <div className="flex flex-wrap items-center gap-3 text-sm">
            <Button type="button" variant="tonal" onClick={() => setReloadKey((k) => k + 1)}>
              Recargar
            </Button>
            <a className="underline" href={open.data.url} target="_blank" rel="noopener noreferrer">
              Abrir en otra pestaña
            </a>
          </div>
          <iframe
            key={reloadKey}
            title="Web de la cámara"
            src={open.data.url}
            sandbox={CAMERA_WEB_SANDBOX}
            referrerPolicy="no-referrer"
            className="h-[70vh] w-full rounded-m3-lg border border-outline bg-white"
          />
        </div>
      )}
    </section>
  );
}
