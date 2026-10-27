import { useState } from "react";
import { Field, Select, TextInput } from "@/components/ui";
import {
  applyViaHint, codecLabel, codecOptionsFor, effectiveValues, NONE, rangeError, reasonText, reconcileForCodec, resolutionLabel,
  type Range, type StreamOptions, type StreamValues, type VideoReason,
} from "./videoParams";

type Props = {
  options: StreamOptions | undefined;
  current: StreamValues;
  /** Pending sparse patch of this stream. */
  patch: StreamValues | undefined;
  /** Why the device cannot be read when there are no options for this stream. */
  reason?: VideoReason;
  onChange: (partial: StreamValues) => void;
};

const resKey = (r: { width: number; height: number }) => `${r.width}x${r.height}`;

function ReadOnly({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="flex flex-col gap-0.5 text-sm">
      <span className="font-medium">{label}</span>
      <span className="font-mono text-xs text-ink">{value}</span>
      {note && <span className="text-xs text-muted">{note}</span>}
    </div>
  );
}

/** Integer input that only reports values inside the range the device announced. */
function NumberField({ label, value, range, hint, onCommit }: { label: string; value: number | undefined; range: Range; hint?: string; onCommit: (n: number) => void }) {
  const [text, setText] = useState(value === undefined ? "" : String(value));
  const [error, setError] = useState<string>();
  // Follow external changes (discard, codec reconcile) without an effect.
  const [seen, setSeen] = useState(value);
  if (seen !== value) {
    setSeen(value);
    setText(value === undefined ? "" : String(value));
    setError(undefined);
  }
  const rangeHint = range ? `Entre ${range.min} y ${range.max}${hint ? ` · ${hint}` : ""}` : hint;
  return (
    <Field label={label} hint={rangeHint} error={error}>
      <TextInput
        type="number"
        inputMode="numeric"
        min={range?.min}
        max={range?.max}
        value={text}
        aria-invalid={!!error}
        onChange={(e) => {
          setText(e.target.value);
          const n = e.target.value === "" ? Number.NaN : Number(e.target.value);
          const err = rangeError(n, range);
          setError(err);
          if (!err) onCommit(n);
        }}
      />
    </Field>
  );
}

/** Parameters of one stream, built only from what the device reports; the rest is read-only text. */
export function VideoParametersForm({ options, current, patch, reason, onChange }: Props) {
  const eff = effectiveValues(current, patch);
  const unavailable = !options || !options.available || options.codecs.length === 0;
  const why = reasonText(options?.reason ?? reason);

  if (unavailable) {
    return (
      <div className="flex flex-col gap-2">
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
          <ReadOnly label="Resolución" value={resolutionLabel(eff.resolution) ?? NONE} />
          <ReadOnly label="FPS" value={eff.fps ? String(eff.fps) : NONE} />
          <ReadOnly label="Códec" value={codecLabel(eff.codec) ?? NONE} />
          <ReadOnly label="Bitrate" value={eff.bitrate_kbps ? `${eff.bitrate_kbps} kbps` : NONE} />
          <ReadOnly label="Keyframe (GOP)" value={eff.gop ? String(eff.gop) : NONE} />
        </div>
        <p className="text-xs text-muted">{why}.</p>
      </div>
    );
  }

  const via = options.apply_via;
  const codecOpts = codecOptionsFor(options, eff.codec);
  const locked = "La cámara no permite cambiar estos valores";
  const resolutions = codecOpts?.resolutions ?? [];
  const resEditable = via.resolution !== "none" && resolutions.length > 0;
  const fpsList = codecOpts?.frame_rates ?? [];

  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {via.codec !== "none" && options.codecs.length > 0 ? (
          <Field label="Códec" hint={applyViaHint(via.codec)}>
            <Select
              value={eff.codec ?? ""}
              onChange={(e) => {
                const next = codecOptionsFor(options, e.target.value);
                if (next) onChange(reconcileForCodec(next, eff));
              }}
            >
              {!codecOpts && <option value={eff.codec ?? ""} disabled>{codecLabel(eff.codec) ?? NONE}</option>}
              {options.codecs.map((c) => <option key={c.codec} value={c.codec}>{codecLabel(c.codec)}</option>)}
            </Select>
          </Field>
        ) : (
          <ReadOnly label="Códec" value={codecLabel(eff.codec) ?? NONE} note={locked} />
        )}

        {resEditable ? (
          <Field label="Resolución" hint={applyViaHint(via.resolution)}>
            <Select
              value={eff.resolution ? resKey(eff.resolution) : ""}
              onChange={(e) => {
                const r = resolutions.find((x) => resKey(x) === e.target.value);
                if (r) onChange({ resolution: { ...r } });
              }}
            >
              {eff.resolution && !resolutions.some((r) => resKey(r) === resKey(eff.resolution!)) && (
                <option value={resKey(eff.resolution)} disabled>{resolutionLabel(eff.resolution)}</option>
              )}
              {resolutions.map((r) => <option key={resKey(r)} value={resKey(r)}>{resolutionLabel(r)}</option>)}
            </Select>
          </Field>
        ) : (
          <ReadOnly label="Resolución" value={resolutionLabel(eff.resolution) ?? NONE} note={locked} />
        )}

        {via.fps === "none" ? (
          <ReadOnly label="FPS" value={eff.fps ? String(eff.fps) : NONE} note={locked} />
        ) : fpsList.length > 0 ? (
          <Field label="FPS" hint={applyViaHint(via.fps)}>
            <Select value={eff.fps === undefined ? "" : String(eff.fps)} onChange={(e) => onChange({ fps: Number(e.target.value) })}>
              {eff.fps !== undefined && !fpsList.includes(eff.fps) && <option value={String(eff.fps)} disabled>{eff.fps}</option>}
              {fpsList.map((f) => <option key={f} value={String(f)}>{f}</option>)}
            </Select>
          </Field>
        ) : codecOpts?.fps ? (
          <NumberField label="FPS" value={eff.fps} range={codecOpts.fps} hint={applyViaHint(via.fps)} onCommit={(n) => onChange({ fps: n })} />
        ) : (
          <ReadOnly label="FPS" value={eff.fps ? String(eff.fps) : NONE} note={locked} />
        )}

        {via.bitrate_kbps !== "none" && codecOpts?.bitrate_kbps ? (
          <NumberField label="Bitrate (kbps)" value={eff.bitrate_kbps} range={codecOpts.bitrate_kbps} hint={applyViaHint(via.bitrate_kbps)} onCommit={(n) => onChange({ bitrate_kbps: n })} />
        ) : (
          <ReadOnly label="Bitrate" value={eff.bitrate_kbps ? `${eff.bitrate_kbps} kbps` : NONE} note={locked} />
        )}

        {via.gop !== "none" && codecOpts?.gop ? (
          <NumberField label="Keyframe (GOP)" value={eff.gop} range={codecOpts.gop} hint={applyViaHint(via.gop)} onCommit={(n) => onChange({ gop: n })} />
        ) : (
          <ReadOnly label="Keyframe (GOP)" value={eff.gop ? String(eff.gop) : NONE} note={locked} />
        )}

        {options.current?.bitrate_mode && <ReadOnly label="Modo de bitrate" value={options.current.bitrate_mode} note="Solo lectura" />}
      </div>
    </div>
  );
}
