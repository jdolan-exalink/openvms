import { useQuery } from "@tanstack/react-query";
import {
  AlertCircle,
  CheckCircle2,
  Download,
  Key,
  Lock,
  Play,
  ShieldCheck,
  Video,
} from "lucide-react";
import { useRef, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { EvidencePlayerModal, type EvidencePlayerJob } from "@/components/EvidencePlayerModal";
import { Icon } from "@/components/Icon";
import { Button, Field, LinkButton, TextInput } from "@/components/ui";
import { fmtBytes, fmtDateTime } from "@/lib/format";

interface PublicShareResponse {
  share_token: string;
  expires_at?: string;
  has_password: boolean;
  job: EvidencePlayerJob;
}

type ShareErrorCode = "password_required" | "invalid_password" | "not_found" | "gone" | "failed";

class ShareError extends Error {
  constructor(
    readonly code: ShareErrorCode,
    message: string,
  ) {
    super(message);
  }
}

export function PublicEvidenceShare() {
  const { token } = useParams({ strict: false }) as { token: string };
  const [password, setPassword] = useState("");
  // The password only travels in a one-off POST body; it never enters a URL or query key.
  const pendingPassword = useRef("");
  const [submitCount, setSubmitCount] = useState(0);
  const [playerOpen, setPlayerOpen] = useState(false);

  const query = useQuery({
    queryKey: ["public-share", token, submitCount],
    queryFn: async (): Promise<PublicShareResponse> => {
      const url = `/media/v1/public/shares/${token}`;
      const password = pendingPassword.current;
      pendingPassword.current = "";
      const request = password
        ? fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/json", "X-OpenVMS-Request": "1" },
            credentials: "same-origin",
            body: JSON.stringify({ password }),
          })
        : fetch(url, { credentials: "same-origin" });
      const res = await request.catch(() => {
        throw new ShareError("failed", "No se pudo conectar. Revisá la conexión y volvé a intentar.");
      });
      if (res.status === 401) throw new ShareError("password_required", "Ingresá la clave de acceso.");
      if (res.status === 403) throw new ShareError("invalid_password", "Contraseña incorrecta");
      if (res.status === 404) throw new ShareError("not_found", "El enlace de evidencia solicitado no existe");
      if (res.status === 410) {
        throw new ShareError("gone", "Este enlace de evidencia ha expirado o fue revocado por el administrador");
      }
      if (!res.ok) throw new ShareError("failed", "Error al consultar la evidencia");
      return res.json();
    },
    retry: false,
  });

  const handlePasswordSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    pendingPassword.current = password;
    setSubmitCount((n) => n + 1);
  };

  const errorCode = query.error instanceof ShareError ? query.error.code : query.error ? "failed" : undefined;
  // Keep the form (and the typed password) after a wrong password or a failed attempt, so the
  // viewer can correct or resubmit it without reloading the page.
  const isPasswordRequired =
    errorCode === "password_required" ||
    errorCode === "invalid_password" ||
    (errorCode === "failed" && submitCount > 0);
  const formError = isPasswordRequired && errorCode !== "password_required" ? query.error?.message : undefined;

  return (
    <div className="min-h-screen bg-surface-0 text-on-surface flex flex-col items-center p-4 md:p-8">
      {/* Header bar */}
      <div className="w-full max-w-4xl flex items-center justify-between py-4 border-b border-outline-variant/60 mb-6">
        <div className="flex items-center gap-2">
          <div className="size-8 rounded-m3-md bg-primary flex items-center justify-center text-on-primary font-bold">
            VMS
          </div>
          <span className="font-bold text-lg tracking-tight">OpenVMS Evidence Portal</span>
        </div>
        <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold bg-ok-container text-on-ok-container border border-ok/30">
          <Icon icon={ShieldCheck} size="xs" />
          <span>Firma Digital SHA-256</span>
        </span>
      </div>

      <div className="w-full max-w-4xl flex flex-col gap-6">
        {/* Password Prompt */}
        {isPasswordRequired && (
          <div className="mx-auto w-full max-w-md p-6 bg-surface-1 rounded-m3-xl border border-outline-variant/80 shadow-md flex flex-col gap-4">
            <div className="flex items-center gap-3">
              <div className="size-10 rounded-full bg-surface-3 flex items-center justify-center text-primary">
                <Icon icon={Lock} size="sm" />
              </div>
              <div className="flex flex-col">
                <h2 className="text-base font-semibold">Evidencia protegida</h2>
                <span className="text-xs text-muted">Ingresá la clave de acceso para ver el material</span>
              </div>
            </div>

            <form onSubmit={handlePasswordSubmit} className="flex flex-col gap-4">
              <Field label="Contraseña">
                <TextInput
                  type="password"
                  placeholder="Clave de seguridad..."
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoFocus
                />
              </Field>

              {formError && (
                <p role="alert" className="text-sm text-bad">
                  {formError}
                </p>
              )}

              <Button variant="filled" size="md" type="submit" disabled={!password.trim() || query.isFetching}>
                <Icon icon={Key} size="xs" />
                <span>Acceder a la evidencia</span>
              </Button>
            </form>
          </div>
        )}

        {/* Error Note if any other error */}
        {!isPasswordRequired && query.isError && (
          <div className="p-4 bg-bad-container/40 border border-bad/40 rounded-m3-lg flex items-center gap-3 text-sm text-bad">
            <Icon icon={AlertCircle} size="sm" />
            <span>{query.error.message}</span>
          </div>
        )}

        {/* Evidence Loaded Card */}
        {query.data && (
          <div className="flex flex-col gap-6 p-6 bg-surface-1 rounded-m3-xl border border-outline-variant/60 shadow-sm">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div className="flex flex-col gap-1">
                <span className="text-xs font-semibold uppercase tracking-wider text-primary">
                  Paquete de Evidencia Digital
                </span>
                <h1 className="text-xl md:text-2xl font-bold leading-tight">{query.data.job.name}</h1>
                <span className="text-xs text-muted font-mono">
                  Grabación: {fmtDateTime(query.data.job.start_time)} ➔ {fmtDateTime(query.data.job.end_time)}
                </span>
              </div>

              <div className="flex items-center gap-2">
                <Button variant="filled" size="md" onClick={() => setPlayerOpen(true)}>
                  <Icon icon={Play} size="xs" />
                  <span>Reproducir en Grid</span>
                </Button>

                {query.data.job.download_url && (
                  <LinkButton variant="outlined" size="md" href={query.data.job.download_url}>
                    <Icon icon={Download} size="xs" />
                    <span>Descargar paquete</span>
                  </LinkButton>
                )}
              </div>
            </div>

            {/* Evidence metadata badge panel */}
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3 p-3 bg-surface-2 rounded-m3-md border border-outline-variant/40 text-xs">
              <div className="flex flex-col">
                <span className="text-muted">Cámaras</span>
                <span className="font-semibold text-on-surface">{query.data.job.camera_count} canales</span>
              </div>
              <div className="flex flex-col">
                <span className="text-muted">Tamaño total</span>
                <span className="font-semibold text-on-surface font-mono">
                  {fmtBytes(query.data.job.total_bytes)}
                </span>
              </div>
              <div className="flex flex-col">
                <span className="text-muted">Integridad</span>
                <span className="font-semibold text-ok flex items-center gap-1">
                  <Icon icon={CheckCircle2} size="xs" /> SHA-256 Verificado
                </span>
              </div>
              <div className="flex flex-col">
                <span className="text-muted">Caducidad del link</span>
                <span className="font-semibold text-on-surface font-mono">
                  {query.data.expires_at ? fmtDateTime(query.data.expires_at) : "Permanente"}
                </span>
              </div>
            </div>

            {/* Cameras Table */}
            <div className="flex flex-col gap-3">
              <h2 className="text-sm font-semibold text-on-surface-variant">
                Cámaras y firmas forenses individuales ({query.data.job.items?.length || 0})
              </h2>

              <div className="flex flex-col gap-2">
                {query.data.job.items?.map((it) => (
                  <div
                    key={it.id}
                    className="flex flex-wrap items-center justify-between gap-3 p-3 bg-surface-0 rounded-m3-md border border-outline-variant/50 text-xs"
                  >
                    <div className="flex items-center gap-2">
                      <Icon icon={Video} size="xs" className="text-muted" />
                      <span className="font-semibold text-on-surface">{it.camera_name}</span>
                      {it.server_name && (
                        <span className="text-[11px] text-muted">({it.server_name})</span>
                      )}
                    </div>

                    <div className="flex items-center gap-4">
                      {it.sha256_hash && (
                        <span
                          className="inline-flex items-center gap-1 text-[11px] font-mono text-ok"
                          title={`Hash completo: ${it.sha256_hash}`}
                        >
                          <Icon icon={CheckCircle2} size="xs" />
                          <span>SHA-256: {it.sha256_hash.slice(0, 16)}...</span>
                        </span>
                      )}
                      <span className="text-muted font-mono">{fmtBytes(it.total_bytes)}</span>
                      {it.video_url && (
                        <LinkButton variant="text" size="sm" href={it.video_url}>
                          <Icon icon={Download} size="xs" />
                          <span>Descargar clip</span>
                        </LinkButton>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* Evidence Player Modal */}
            {playerOpen && (
              <EvidencePlayerModal
                job={query.data.job}
                onClose={() => setPlayerOpen(false)}
                isPublic={true}
                shareToken={token}
              />
            )}
          </div>
        )}
      </div>
    </div>
  );
}
