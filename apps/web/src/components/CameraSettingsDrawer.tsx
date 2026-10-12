import { Activity, Car, Eye, Radio, SlidersHorizontal, Sparkles, Video } from "lucide-react";
import { Icon } from "./Icon";
import { cn } from "@/lib/cn";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { cameraFrigateConfigQuery } from "@/api/queries";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, Field, LinkButton, Select, StatusBadge, Switch, TextInput } from "@/components/ui";

type Camera = Schemas["Camera"];

function FrigateSummary({ config }: { config: Schemas["CameraFrigateConfig"] }) {
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
      <dt className="text-muted">Zonas</dt>
      <dd>{config.zones.length > 0 ? config.zones.join(", ") : "Sin zonas"}</dd>
      <dt className="text-muted">Detección</dt>
      <dd>{config.detect_enabled ? "Habilitada" : "Deshabilitada"}</dd>
      <dt className="text-muted">LPR</dt>
      <dd>{config.lpr_enabled ? "Habilitado" : "Deshabilitado"}</dd>
      <dt className="text-muted">Objetos</dt>
      <dd>{config.tracked_objects.join(", ") || "Ninguno"}</dd>
    </dl>
  );
}

/**
 * CameraSettingsDrawer shows a camera's read-only inventory context and, for users
 * who can manage cameras or configure servers, editing forms for VMS-side settings
 * and a link to the full Frigate config editor (FrigateCameraConfig).
 */
export function CameraSettingsDrawer({
  camera,
  siteName,
  serverName,
  canManage,
  canConfigServer = false,
  onClose,
}: {
  camera: Camera;
  siteName?: string;
  serverName?: string;
  canManage: boolean;
  canConfigServer?: boolean;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const [name, setName] = useState(camera.display_name);
  const [enabled, setEnabled] = useState(camera.enabled);
  const [quality, setQuality] = useState<Camera["default_live_quality"]>(camera.default_live_quality);
  const [description, setDescription] = useState(camera.description);
  const [location, setLocation] = useState(camera.location);
  const [tags, setTags] = useState(camera.tags.join(", "));
  const [invalid, setInvalid] = useState(false);

  const [isLpr, setIsLpr] = useState(camera.lpr ?? camera.tags.includes("lpr"));
  const [isPtz, setIsPtz] = useState(camera.onvif_capabilities?.has_ptz ?? camera.tags.includes("ptz"));
  const [isFisheye, setIsFisheye] = useState(camera.tags.includes("fisheye") || camera.tags.includes("ojo_de_pez"));
  const [hasAudio, setHasAudio] = useState(camera.tags.includes("audio") || camera.tags.includes("mic"));
  const [isThermal, setIsThermal] = useState(camera.tags.includes("thermal") || camera.tags.includes("termica"));

  // Read-only Frigate summary; the full editor lives on its own page (FrigateCameraConfig).
  const frigateConfig = useQuery(cameraFrigateConfigQuery(camera.id));

  const save = useMutation({
    mutationFn: async () => {
      if (canManage) {
        const parsedTags = new Set(tags.split(",").map((t) => t.trim().toLowerCase()).filter(Boolean));
        if (isLpr) parsedTags.add("lpr"); else parsedTags.delete("lpr");
        if (isPtz) parsedTags.add("ptz"); else parsedTags.delete("ptz");
        if (isFisheye) parsedTags.add("fisheye"); else parsedTags.delete("fisheye");
        if (hasAudio) parsedTags.add("audio"); else parsedTags.delete("audio");
        if (isThermal) parsedTags.add("thermal"); else parsedTags.delete("thermal");

        await unwrap(
          await api.PATCH("/api/v1/cameras/{cameraId}", {
            params: { path: { cameraId: camera.id } },
            body: {
              display_name: name.trim(),
              enabled,
              default_live_quality: quality,
              description: description.trim(),
              location: location.trim(),
              tags: Array.from(parsedTags),
            },
          }),
        );
      }
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras"] });
      onClose();
    },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    if (canManage && !name.trim()) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    save.mutate();
  }

  const canEdit = canManage || canConfigServer;

  const [imgError, setImgError] = useState(false);
  const snapshotUrl = `/media/v1/cameras/${camera.id}/snapshot.jpg?h=240`;
  const isOnline = camera.enabled && camera.status === "online";

  return (
    <Modal title="Ajustes de cámara" onClose={onClose}>
      <div className="flex flex-col gap-4">
        {/* Avigilon Live Viewfinder Thumbnail */}
        <div className="relative aspect-video w-full overflow-hidden rounded-m3-lg border border-outline-variant/60 bg-black/80 shadow-inner">
          {!imgError ? (
            <img
              src={snapshotUrl}
              alt={`Vista previa de ${camera.display_name}`}
              className="size-full object-cover"
              onError={() => setImgError(true)}
            />
          ) : (
            <div className="flex size-full flex-col items-center justify-center gap-2 text-muted">
              <Icon icon={Video} size="md" className="opacity-40" />
              <span className="font-mono text-xs">Vista previa no disponible</span>
            </div>
          )}
          <div className="pointer-events-none absolute inset-0 flex flex-col justify-between p-3 bg-gradient-to-t from-black/80 via-transparent to-black/60">
            <div className="flex items-center justify-between gap-2">
              <span className="flex items-center gap-1.5 rounded-m3-xs bg-black/60 px-2 py-0.5 font-mono text-[10px] font-bold tracking-wider text-white backdrop-blur-md">
                <span className={cn("size-2 rounded-full", isOnline ? "bg-ok animate-pulse" : "bg-muted")} />
                {isOnline ? "EN VIVO" : camera.enabled ? "ONLINE" : "OFFLINE"}
              </span>
              {camera.onvif_profile && (
                <span className="rounded-m3-xs bg-primary/20 border border-primary/30 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-primary backdrop-blur-md">
                  {camera.onvif_profile}
                </span>
              )}
            </div>
            <div className="flex items-end justify-between gap-2">
              <div>
                <p className="font-bold text-sm text-white drop-shadow-sm">{camera.display_name}</p>
                <p className="font-mono text-[11px] text-white/80">{camera.remote_name}</p>
              </div>
              <span className="rounded-m3-xs bg-black/60 px-1.5 py-0.5 font-mono text-[10px] text-white/90 backdrop-blur-md">
                {quality === "main" ? "MAIN" : "SUB"}
              </span>
            </div>
          </div>
        </div>

        {/* Telemetry card */}
        <div className="rounded-m3-lg bg-surface-2 p-3.5 border border-outline-variant/40">
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
            <dt className="text-muted">Origen en Frigate</dt>
            <dd className="font-mono text-xs text-ink">{camera.remote_name}</dd>
            <dt className="text-muted">Sitio</dt>
            <dd className="text-ink">{siteName ?? "—"}</dd>
            <dt className="text-muted">Servidor</dt>
            <dd className="text-ink">{serverName ?? "—"}</dd>
            <dt className="text-muted">Estado</dt>
            <dd>{camera.enabled ? <StatusBadge status={camera.status} /> : "Deshabilitada"}</dd>
          </dl>
        </div>

        {canEdit ? (
          <form onSubmit={submit} className="flex flex-col gap-3">
            {canManage ? (
              <>
                <Field label="Nombre">
                  <TextInput value={name} onChange={(e) => setName(e.target.value)} aria-invalid={invalid} />
                </Field>
                {invalid && <p role="alert" className="text-xs text-bad">El nombre no puede estar vacío.</p>}
                <Switch label="Habilitada" checked={enabled} onChange={setEnabled} />
                <Field label="Calidad en vivo por defecto" hint="Flujo que usa la cámara al añadirla a la grilla de Vivo.">
                  <Select value={quality} onChange={(e) => setQuality(e.target.value as Camera["default_live_quality"])}>
                    <option value="sub">Sub (menor calidad)</option>
                    <option value="main">Main (alta calidad)</option>
                  </Select>
                </Field>
                <Field label="Descripción">
                  <TextInput value={description} maxLength={1000} onChange={(e) => setDescription(e.target.value)} />
                </Field>
                <Field label="Ubicación">
                  <TextInput value={location} maxLength={200} onChange={(e) => setLocation(e.target.value)} />
                </Field>
                <Field label="Etiquetas" hint="Separadas por comas (máximo 20).">
                  <TextInput value={tags} onChange={(e) => setTags(e.target.value)} />
                </Field>

                <div className="flex flex-col gap-2 rounded-m3-lg border border-outline-variant/60 bg-surface-2 p-3">
                  <span className="text-xs font-bold text-muted uppercase">Tipo y Funciones de Cámara</span>
                  <div className="grid grid-cols-2 gap-2">
                    <label className={cn("flex items-center gap-2 rounded-m3-md p-2 text-xs border cursor-pointer transition-colors", isLpr ? "border-primary bg-primary/10 text-ink font-semibold" : "border-outline-variant/60 bg-surface-1 text-muted")}>
                      <input type="checkbox" checked={isLpr} onChange={(e) => setIsLpr(e.target.checked)} className="size-3.5 rounded accent-primary" />
                      <span className="flex items-center gap-1"><Icon icon={Car} size="xs" /> LPR</span>
                    </label>
                    <label className={cn("flex items-center gap-2 rounded-m3-md p-2 text-xs border cursor-pointer transition-colors", isPtz ? "border-primary bg-primary/10 text-ink font-semibold" : "border-outline-variant/60 bg-surface-1 text-muted")}>
                      <input type="checkbox" checked={isPtz} onChange={(e) => setIsPtz(e.target.checked)} className="size-3.5 rounded accent-primary" />
                      <span className="flex items-center gap-1"><Icon icon={Radio} size="xs" /> PTZ</span>
                    </label>
                    <label className={cn("flex items-center gap-2 rounded-m3-md p-2 text-xs border cursor-pointer transition-colors", isFisheye ? "border-primary bg-primary/10 text-ink font-semibold" : "border-outline-variant/60 bg-surface-1 text-muted")}>
                      <input type="checkbox" checked={isFisheye} onChange={(e) => setIsFisheye(e.target.checked)} className="size-3.5 rounded accent-primary" />
                      <span className="flex items-center gap-1"><Icon icon={Eye} size="xs" /> 360° / Fisheye</span>
                    </label>
                    <label className={cn("flex items-center gap-2 rounded-m3-md p-2 text-xs border cursor-pointer transition-colors", hasAudio ? "border-primary bg-primary/10 text-ink font-semibold" : "border-outline-variant/60 bg-surface-1 text-muted")}>
                      <input type="checkbox" checked={hasAudio} onChange={(e) => setHasAudio(e.target.checked)} className="size-3.5 rounded accent-primary" />
                      <span className="flex items-center gap-1"><Icon icon={Activity} size="xs" /> Audio</span>
                    </label>
                    <label className={cn("flex items-center gap-2 rounded-m3-md p-2 text-xs border cursor-pointer transition-colors col-span-2", isThermal ? "border-primary bg-primary/10 text-ink font-semibold" : "border-outline-variant/60 bg-surface-1 text-muted")}>
                      <input type="checkbox" checked={isThermal} onChange={(e) => setIsThermal(e.target.checked)} className="size-3.5 rounded accent-primary" />
                      <span className="flex items-center gap-1"><Icon icon={Sparkles} size="xs" /> Cámara Térmica</span>
                    </label>
                  </div>
                </div>
              </>
            ) : (
              <p className="text-xs text-muted">Necesitas cameras.manage para editar los ajustes de VMS.</p>
            )}

            <div className="mt-2 flex flex-col gap-1 rounded-m3-lg bg-surface-2 p-4 border border-outline-variant/40">
              <h3 className="mb-2 text-base font-bold text-ink">Configuración en Frigate</h3>
              {frigateConfig.isLoading && <p className="text-xs text-muted">Cargando configuración de Frigate…</p>}
              {frigateConfig.error && <p className="text-xs text-muted">Configuración de Frigate no disponible.</p>}
              {frigateConfig.data && <FrigateSummary config={frigateConfig.data} />}
              {canConfigServer && (
                <LinkButton
                  variant="tonal"
                  to="/cameras/$cameraId/frigate"
                  params={{ cameraId: camera.id }}
                  onClick={onClose}
                  className="mt-3 self-start"
                >
                  <Icon icon={SlidersHorizontal} size="xs" /> Configuración avanzada (Estilo Avigilon)
                </LinkButton>
              )}
            </div>

            <ErrorNote error={save.error} />
            <div className="flex justify-end gap-2 mt-2">
              <Button variant="text" onClick={onClose}>Cancelar</Button>
              <Button type="submit" variant="primary" disabled={save.isPending}>
                {save.isPending ? "Guardando…" : "Guardar"}
              </Button>
            </div>
          </form>
        ) : (
          <div className="flex flex-col gap-3">
            <p className="text-xs text-muted">Necesitas el permiso cameras.manage o servers.config para editar esta cámara.</p>
            {frigateConfig.data && (
              <div className="rounded-m3-lg bg-surface-2 p-4 border border-outline-variant/40">
                <h3 className="mb-2 text-base font-bold text-ink">Configuración en Frigate</h3>
                <FrigateSummary config={frigateConfig.data} />
              </div>
            )}
          </div>
        )}
      </div>
    </Modal>
  );
}
