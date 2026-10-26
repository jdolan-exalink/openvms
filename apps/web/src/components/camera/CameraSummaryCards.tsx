import type { ReactNode } from "react";
import { NONE, type DeviceRow, type StreamRow } from "./summary";

function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex min-w-0 flex-col gap-1.5 rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-3 text-xs">
      <h2 className="font-semibold uppercase tracking-wider text-muted">{title}</h2>
      {children}
    </section>
  );
}

const Row = ({ label, value }: { label: string; value: string }) => (
  <div className="flex justify-between gap-2">
    <dt className="text-muted">{label}</dt>
    <dd className="truncate text-right font-mono text-ink" title={value}>{value}</dd>
  </div>
);

/** Origen / Video / Dispositivo / Capacidades; unknown data shows a dash. */
export function CameraSummaryCards({ origin, video, device, capabilities }: { origin: DeviceRow[]; video: StreamRow[]; device: DeviceRow[]; capabilities: string[] }) {
  return (
    <div className="grid min-w-0 flex-1 grid-cols-1 gap-2 sm:grid-cols-2">
      <Card title="Origen"><dl className="flex flex-col gap-1">{origin.map((r) => <Row key={r.label} {...r} />)}</dl></Card>
      <Card title="Video"><dl className="flex flex-col gap-1">{video.map((r) => <Row key={r.id} label={r.label} value={r.value} />)}</dl></Card>
      <Card title="Dispositivo"><dl className="flex flex-col gap-1">{device.map((r) => <Row key={r.label} {...r} />)}</dl></Card>
      <Card title="Capacidades"><p className="font-mono text-ink">{capabilities.length ? capabilities.join(" · ") : NONE}</p></Card>
    </div>
  );
}
