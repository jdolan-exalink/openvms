import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, type Schemas } from "@/api/client";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button, Select } from "@/components/ui";
import { StreamCard } from "./StreamCard";

type Result = Schemas["CameraOnvifVideoEncodingResult"];
type Config = NonNullable<Result["configurations"]>[number];
type Pending = { config: Config; encoding: string };

const REFUSALS: Record<string, string> = {
  media2_unsupported: "La cámara no tiene el servicio ONVIF Media2, el único que ofrece H.265.",
  encoding_unsupported: "La cámara no lista ese códec para esta configuración.",
  encoding_unchanged: "La configuración ya usa ese códec.",
  configuration_not_found: "La cámara ya no tiene esa configuración de video. Vuelve a leer sus opciones.",
};

const label = (encoding: string) => encoding.replace(/^H(\d)/, "H.$1");
const summary = (c: Config) => `${label(c.encoding)} · ${c.width}×${c.height} · ${c.fps} fps · ${c.bitrate} kbps`;

/** Video codec over ONVIF: reads what the camera lists and switches one configuration to an encoding it offers. */
export function VideoCodecCard({ cameraId, readOnlyReason }: { cameraId: string; readOnlyReason?: string }) {
  const [result, setResult] = useState<Result | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const [chosen, setChosen] = useState<Record<string, string>>({});
  const [pending, setPending] = useState<Pending | null>(null);

  const call = (body: { action: "options" } | { action: "set"; config_token: string; encoding: "H264" | "H265" }) =>
    api.POST("/api/v1/cameras/{cameraId}/onvif/video-encoding", { params: { path: { cameraId } }, body }).then(unwrap);

  const read = useMutation({
    mutationFn: () => call({ action: "options" }),
    onSuccess: (res) => {
      setResult(res);
      setChosen({});
    },
  });
  const change = useMutation({
    mutationFn: (p: Pending) => call({ action: "set", config_token: p.config.token, encoding: p.encoding as "H264" | "H265" }),
    onSuccess: (res, p) => {
      setPending(null);
      setOutcome(
        res.success
          ? `${p.config.name}: cambiado de ${label(res.previous_encoding ?? p.config.encoding)} a ${label(res.encoding ?? p.encoding)}. El stream se reinicia unos segundos.`
          : (res.code && REFUSALS[res.code]) || res.detail || "La cámara rechazó el cambio.",
      );
      if (res.success) read.mutate();
    },
  });

  const configs = result?.configurations ?? [];
  const unsupported = result && !result.supported;

  return (
    <StreamCard title="Códec de video" purpose="Cambia el códec (H.264 o H.265) que la cámara entrega, solo entre los que ella misma lista.">
      {readOnlyReason && <p role="note" className="text-xs text-muted">Solo lectura. {readOnlyReason}</p>}
      <div>
        <Button variant="tonal" disabled={!!readOnlyReason || read.isPending} onClick={() => { setOutcome(null); read.mutate(); }}>
          {read.isPending ? "Leyendo…" : result ? "Volver a leer" : "Leer códecs de la cámara"}
        </Button>
      </div>

      {read.isError && (
        <p role="alert" className="rounded-m3-lg bg-bad/12 px-4 py-3 text-sm text-bad">
          No se pudieron leer las opciones: {read.error instanceof Error ? read.error.message : String(read.error)}
        </p>
      )}
      {unsupported && <p role="note" className="text-sm text-muted">{(result.code && REFUSALS[result.code]) || REFUSALS.media2_unsupported}</p>}
      {result?.supported && configs.length === 0 && <p role="note" className="text-sm text-muted">La cámara no informa configuraciones de video.</p>}

      {configs.map((c) => {
        const targets = c.supported_encodings.filter((e) => e !== c.encoding);
        const target = chosen[c.token] ?? targets[0] ?? "";
        return (
          <div key={c.token} className="flex flex-wrap items-end gap-3 border-t border-outline-variant/40 pt-3">
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">{c.name}</p>
              <p className="font-mono text-xs text-muted">{summary(c)}</p>
            </div>
            {targets.length === 0 ? (
              <p className="text-xs text-muted">No hay otro códec disponible.</p>
            ) : (
              <>
                <Select aria-label={`Nuevo códec de ${c.name}`} value={target} disabled={!!readOnlyReason} onChange={(e) => setChosen((s) => ({ ...s, [c.token]: e.target.value }))}>
                  {targets.map((e) => <option key={e} value={e}>{label(e)}</option>)}
                </Select>
                <Button variant="tonal" disabled={!!readOnlyReason || !target} onClick={() => setPending({ config: c, encoding: target })}>Cambiar códec</Button>
              </>
            )}
          </div>
        );
      })}

      {outcome && <p role="status" className="rounded-m3-lg bg-surface px-4 py-3 text-sm">{outcome}</p>}

      {pending && (
        <ConfirmDialog
          title="Cambiar el códec de video"
          message={`${pending.config.name} pasará de ${label(pending.config.encoding)} a ${label(pending.encoding)}. La cámara reinicia ese stream y se corta unos segundos.`}
          confirmLabel="Cambiar códec"
          pending={change.isPending}
          error={change.error}
          onConfirm={() => change.mutate(pending)}
          onCancel={() => { if (!change.isPending) { setPending(null); change.reset(); } }}
        />
      )}
    </StreamCard>
  );
}
