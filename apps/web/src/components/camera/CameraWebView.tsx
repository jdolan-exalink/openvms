import { useMutation } from "@tanstack/react-query";
import { Check, Copy, Eye, EyeOff } from "lucide-react";
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
  const [customUser, setCustomUser] = useState("");
  const [customPass, setCustomPass] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [copied, setCopied] = useState(false);
  const [showCredEditor, setShowCredEditor] = useState(false);
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
      const ch = choice();
      if (!ch) throw new Error("Indicá un puerto entre 1 y 65535.");
      const body = {
        ...ch,
        ...(customUser.trim() ? { username: customUser.trim() } : {}),
        ...(customPass ? { password: customPass } : {}),
      };
      return unwrap(await api.POST("/api/v1/cameras/{cameraId}/web/session", { params: { path: { cameraId } }, body }));
    },
  });

  const activeUser = customUser || open.data?.username || "";
  const activePass = customPass || open.data?.password || "";

  const copyPassword = async () => {
    if (!activePass) return;
    try {
      await navigator.clipboard.writeText(activePass);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // clipboard write might be blocked in some contexts
    }
  };

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
          <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
            <div className="flex items-center gap-3">
              <Button type="button" variant="tonal" onClick={() => setReloadKey((k) => k + 1)}>
                Recargar
              </Button>
              <a className="underline" href={open.data.url} target="_blank" rel="noopener noreferrer">
                Abrir en otra pestaña
              </a>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {activeUser && (
                <span className="text-xs bg-surface-2 px-2.5 py-1 rounded-m3-md font-mono flex items-center gap-1.5 border border-outline/20">
                  <span className="text-muted font-sans font-normal">Usuario:</span> <strong>{activeUser}</strong>
                </span>
              )}
              {activePass && (
                <div className="flex items-center gap-1 bg-surface-2 px-2.5 py-1 rounded-m3-md font-mono text-xs border border-outline/20">
                  <span className="text-muted font-sans font-normal">Contraseña:</span>
                  <span>{showPassword ? activePass : "••••••••"}</span>
                  <button
                    type="button"
                    aria-label={showPassword ? "Ocultar contraseña" : "Ver contraseña"}
                    onClick={() => setShowPassword((s) => !s)}
                    className="ml-1 text-muted hover:text-foreground p-0.5"
                  >
                    {showPassword ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                  </button>
                  <button
                    type="button"
                    aria-label="Copiar contraseña"
                    onClick={copyPassword}
                    className="ml-1 text-muted hover:text-foreground p-0.5 flex items-center gap-1"
                  >
                    {copied ? <Check className="size-3.5 text-good" /> : <Copy className="size-3.5" />}
                    {copied ? <span className="text-[10px] text-good font-sans">Copiada</span> : null}
                  </button>
                </div>
              )}
              <button
                type="button"
                className="text-xs text-primary underline ml-1 cursor-pointer"
                onClick={() => setShowCredEditor((e) => !e)}
              >
                {showCredEditor ? "Cerrar credenciales" : "Cambiar credenciales"}
              </button>
            </div>
          </div>

          {showCredEditor && (
            <div className="flex flex-wrap items-end gap-3 p-3 bg-surface-2 rounded-m3-md border border-outline/20 text-sm">
              <label className="grid gap-1">
                <span className="text-xs text-muted">Usuario para la cámara</span>
                <TextInput
                  aria-label="Usuario para la cámara"
                  value={customUser || activeUser}
                  onChange={(e) => setCustomUser(e.target.value)}
                  placeholder="admin"
                />
              </label>
              <label className="grid gap-1">
                <span className="text-xs text-muted">Contraseña para la cámara</span>
                <TextInput
                  aria-label="Contraseña para la cámara"
                  type="password"
                  value={customPass || activePass}
                  onChange={(e) => setCustomPass(e.target.value)}
                />
              </label>
              {activeUser !== "admin" && (
                <Button
                  type="button"
                  variant="tonal"
                  size="sm"
                  onClick={() => {
                    setCustomUser("admin");
                    open.mutate();
                  }}
                >
                  Probar con &apos;admin&apos;
                </Button>
              )}
              <Button
                type="button"
                size="sm"
                disabled={open.isPending}
                onClick={() => open.mutate()}
              >
                Reconectar
              </Button>
            </div>
          )}

          <div className="flex items-center justify-between text-xs text-muted">
            <span>
              Auto-login: las credenciales configuradas se rellenan automáticamente vía proxy.
            </span>
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
