import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Key, Link2, Trash2 } from "lucide-react";
import { useState } from "react";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, Checkbox, ErrorNote, Field, IconButton, TextInput } from "@/components/ui";
import { fmtDateTime } from "@/lib/format";

interface ExportShareItem {
  id: string;
  job_id: string;
  share_token: string;
  created_by: string;
  expires_at?: string;
  has_password: boolean;
  views_count: number;
  created_at: string;
}

interface ShareExportModalProps {
  job: { id: string; name: string };
  onClose: () => void;
}

export function ShareExportModal({ job, onClose }: ShareExportModalProps) {
  const qc = useQueryClient();
  const [expiryDays, setExpiryDays] = useState<number | null>(7);
  const [usePassword, setUsePassword] = useState(false);
  const [password, setPassword] = useState("");
  const [newShareUrl, setNewShareUrl] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // Fetch active shares for this job
  const sharesQuery = useQuery({
    queryKey: ["export-shares", job.id],
    queryFn: async (): Promise<ExportShareItem[]> => {
      const res = await fetch(`/api/v1/export-jobs/${job.id}/shares`, {
        headers: { "Content-Type": "application/json" },
      });
      if (!res.ok) throw new Error("Error al consultar enlaces compartidos");
      return res.json();
    },
  });

  // Create share mutation
  const createMutation = useMutation({
    mutationFn: async () => {
      let expiresAt: string | undefined;
      if (expiryDays !== null) {
        const d = new Date();
        d.setDate(d.getDate() + expiryDays);
        expiresAt = d.toISOString();
      }
      const res = await fetch(`/api/v1/export-jobs/${job.id}/shares`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          expires_at: expiresAt,
          password: usePassword ? password : "",
        }),
      });
      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw new Error(err.message || "Error al crear enlace compartido");
      }
      return res.json() as Promise<ExportShareItem>;
    },
    onSuccess: (data) => {
      const url = `${window.location.origin}/share/${data.share_token}`;
      setNewShareUrl(url);
      void qc.invalidateQueries({ queryKey: ["export-shares", job.id] });
    },
  });

  // Revoke share mutation
  const revokeMutation = useMutation({
    mutationFn: async (shareId: string) => {
      const res = await fetch(`/api/v1/export-jobs/shares/${shareId}`, {
        method: "DELETE",
      });
      if (!res.ok) throw new Error("Error al revocar enlace");
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["export-shares", job.id] });
    },
  });

  const copyUrl = (url: string) => {
    navigator.clipboard.writeText(url);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Modal
      title="Compartir evidencia de video"
      onClose={onClose}
      className="max-w-2xl bg-surface-1 border border-outline-variant/60 p-6 flex flex-col gap-5"
    >
      <div className="flex flex-col gap-1">
        <span className="text-xs text-muted">Exportación</span>
        <span className="text-sm font-semibold text-on-surface">{job.name}</span>
      </div>

      <ErrorNote error={createMutation.error || revokeMutation.error} />

      {/* Generated Link Alert */}
      {newShareUrl && (
        <div className="flex flex-col gap-2 p-3 bg-ok-container/40 border border-ok/40 rounded-m3-md">
          <span className="text-xs font-semibold text-ok">¡Enlace de evidencia generado con éxito!</span>
          <div className="flex items-center gap-2">
            <input
              type="text"
              readOnly
              value={newShareUrl}
              className="flex-1 px-3 py-1.5 text-xs font-mono bg-surface-1 rounded-m3-sm border border-outline-variant select-all"
            />
            <Button variant="filled" size="sm" onClick={() => copyUrl(newShareUrl)}>
              <Icon icon={copied ? Check : Copy} size="xs" />
              <span>{copied ? "Copiado" : "Copiar enlace"}</span>
            </Button>
          </div>
          <span className="text-[11px] text-muted">
            Este enlace permite a terceros acceder al reproductor forense y descargar la evidencia sin requerir cuenta en OpenVMS.
          </span>
        </div>
      )}

      {/* Creation form */}
      <div className="flex flex-col gap-4 p-4 bg-surface-2/60 rounded-m3-md border border-outline-variant/40">
        <span className="text-xs font-semibold text-on-surface">Configurar nuevo enlace</span>

        {/* Expiration options */}
        <div className="flex flex-col gap-1.5">
          <span className="text-xs text-on-surface-variant">Caducidad del enlace</span>
          <div className="flex flex-wrap gap-2 text-xs">
            {[
              { label: "24 horas", value: 1 },
              { label: "7 días", value: 7 },
              { label: "30 días", value: 30 },
              { label: "Sin expiración", value: null },
            ].map((opt) => (
              <button
                key={opt.label}
                type="button"
                onClick={() => setExpiryDays(opt.value)}
                className={`px-3 py-1.5 rounded-m3-sm font-medium transition-colors ${
                  expiryDays === opt.value
                    ? "bg-primary text-on-primary"
                    : "bg-surface-1 text-on-surface hover:bg-surface-3 border border-outline-variant/50"
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        {/* Password protection */}
        <div className="flex flex-col gap-2 pt-1 border-t border-outline-variant/30">
          <Checkbox
            label="Proteger acceso con contraseña"
            checked={usePassword}
            onChange={setUsePassword}
          />

          {usePassword && (
            <Field label="Contraseña requerida para ver la evidencia">
              <TextInput
                type="password"
                placeholder="Ingresá una clave de acceso..."
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </Field>
          )}
        </div>

        <div className="flex justify-end pt-2">
          <Button
            variant="filled"
            size="sm"
            onClick={() => createMutation.mutate()}
            disabled={createMutation.isPending || (usePassword && !password.trim())}
          >
            <Icon icon={Link2} size="xs" />
            <span>{createMutation.isPending ? "Generando..." : "Generar enlace compartido"}</span>
          </Button>
        </div>
      </div>

      {/* Active Shares List */}
      <div className="flex flex-col gap-2 pt-2 border-t border-outline-variant/40">
        <span className="text-xs font-semibold text-on-surface-variant">
          Enlaces activos ({sharesQuery.data?.length || 0})
        </span>

        {sharesQuery.isLoading && <span className="text-xs text-muted">Cargando enlaces...</span>}

        {sharesQuery.data && sharesQuery.data.length === 0 && (
          <span className="text-xs text-muted">No hay enlaces compartidos activos para esta evidencia.</span>
        )}

        {sharesQuery.data && sharesQuery.data.length > 0 && (
          <div className="flex flex-col gap-2">
            {sharesQuery.data.map((sh) => {
              const url = `${window.location.origin}/share/${sh.share_token}`;
              return (
                <div
                  key={sh.id}
                  className="flex flex-wrap items-center justify-between gap-2 p-2.5 bg-surface-2/70 rounded-m3-sm border border-outline-variant/40 text-xs"
                >
                  <div className="flex flex-col gap-0.5">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-primary font-medium">
                        /share/{sh.share_token.slice(0, 10)}...
                      </span>
                      {sh.has_password && (
                        <span className="inline-flex items-center gap-1 text-[11px] px-1.5 py-0.5 rounded-full bg-surface-3 text-muted">
                          <Icon icon={Key} size="xs" />
                          <span>Clave</span>
                        </span>
                      )}
                      <span className="text-[11px] text-muted">
                        • {sh.views_count} {sh.views_count === 1 ? "visita" : "visitas"}
                      </span>
                    </div>
                    <span className="text-[11px] text-muted font-mono">
                      Creado: {fmtDateTime(sh.created_at)}
                      {sh.expires_at ? ` • Vence: ${fmtDateTime(sh.expires_at)}` : " • Permanente"}
                    </span>
                  </div>

                  <div className="flex items-center gap-1.5">
                    <Button variant="outlined" size="sm" onClick={() => copyUrl(url)} title="Copiar enlace">
                      <Icon icon={Copy} size="xs" />
                      <span>Copiar</span>
                    </Button>
                    <IconButton
                      icon={Trash2}
                      onClick={() => {
                        if (confirm("¿Revocar este enlace compartido? Dejará de ser accesible inmediatamente.")) {
                          revokeMutation.mutate(sh.id);
                        }
                      }}
                      aria-label="Revocar enlace"
                      title="Revocar enlace"
                    />
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </Modal>
  );
}
