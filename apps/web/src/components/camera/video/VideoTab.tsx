import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { cameraVideoOptionsQuery } from "@/api/queries";
import { Button } from "@/components/ui";
import { useCameraChange } from "../CameraChangeProvider";
import { AudioSection } from "./AudioSection";
import { StreamCard } from "./StreamCard";
import { VideoParametersForm } from "./VideoParametersForm";
import { currentValues, differsFromCurrent, RESTART_NOTICE, streamSummary, type Role, type StreamValues } from "./videoParams";

const ROLES: Array<{ role: Role; title: string; purpose: string }> = [
  { role: "primary", title: "Stream principal", purpose: "Live en alta calidad, grabación y exportación." },
  { role: "detection", title: "Stream de detección", purpose: "Detección de objetos, con menor carga." },
];

const isRec = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

/** Video tab: streams and their parameters in video terms; the Frigate sections stay under "Avanzado de video". */
export function VideoTab({ cameraId, legacy, readOnlyReason }: { cameraId: string; legacy?: ReactNode; readOnlyReason?: string }) {
  const change = useCameraChange();
  const options = useQuery(cameraVideoOptionsQuery(cameraId));
  const model = change.config.data;
  const streams = isRec(change.patch.streams) ? change.patch.streams : {};
  const patchOf = (role: Role) => (isRec(streams[role]) ? (streams[role] as StreamValues) : undefined);

  const rows = ROLES.map((r) => {
    const opts = options.data?.streams.find((s) => s.role === r.role);
    const modelStream = r.role === "primary" ? model?.streams?.primary : model?.streams?.detection;
    const current = currentValues(opts, modelStream);
    return { ...r, opts, modelStream, current, patch: patchOf(r.role) };
  });
  const restart = rows.some((r) => differsFromCurrent(r.current, r.patch));
  const recording = model?.streams?.recording;
  const hasAudio = model?.capabilities?.includes("audio") ?? false;

  return (
    <div className="flex flex-col gap-5">
      <p className="text-sm text-muted">Estos son los streams de la cámara y los valores que el dispositivo permite cambiar.</p>
      {readOnlyReason && <p role="note" className="text-xs text-muted">Solo lectura. {readOnlyReason}</p>}

      {options.isError && (
        <div role="alert" className="rounded-m3-lg bg-bad/12 px-4 py-3 text-sm text-bad">
          <p>No se pudieron leer las opciones de video de la cámara.</p>
          <details className="mt-1 text-xs">
            <summary className="cursor-pointer">Ver información técnica</summary>
            <p className="break-words font-mono">{options.error instanceof Error ? options.error.message : String(options.error)}</p>
          </details>
        </div>
      )}

      {rows.map((r) => (
        <StreamCard
          key={r.role}
          title={r.title}
          purpose={r.purpose}
          summary={streamSummary({ ...r.modelStream, codec: r.current.codec, width: r.current.resolution?.width, height: r.current.resolution?.height, fps: r.current.fps })}
        >
          <fieldset disabled={!!readOnlyReason} className="contents">
          <VideoParametersForm
            options={r.opts}
            current={r.current}
            patch={r.patch}
            reason={options.data?.reason}
            onChange={(partial) => change.setPatch({ streams: { [r.role]: partial } })}
          />
          </fieldset>
        </StreamCard>
      ))}

      {recording && (
        <StreamCard
          title="Stream de grabación"
          purpose="Stream distinto del principal que se usa solo para grabar."
          summary={streamSummary(recording)}
        />
      )}

      {restart && (
        <div role="status" className="flex flex-wrap items-center justify-between gap-3 rounded-m3-lg bg-warn/10 px-4 py-3 text-sm text-warn">
          <span>{RESTART_NOTICE}</span>
          <Button variant="tonal" onClick={change.openReview}>Revisar cambios</Button>
        </div>
      )}

      {hasAudio && <AudioSection detecting={model?.analytics?.audio} />}

      {legacy && (
        <details className="rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-3">
          <summary className="cursor-pointer text-sm font-semibold">Avanzado de video (Frigate)</summary>
          <div className="mt-4 flex flex-col gap-5">{legacy}</div>
        </details>
      )}
    </div>
  );
}
