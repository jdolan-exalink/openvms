/** Audio of the camera. Rendered only when the camera has audio; shows what the model exposes. */
export function AudioSection({ detecting }: { detecting: boolean | undefined }) {
  return (
    <section aria-label="Audio" className="flex flex-col gap-2 rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-4">
      <h3 className="text-sm font-semibold">Audio</h3>
      <dl className="flex flex-col gap-1 text-sm">
        <div className="flex justify-between gap-2">
          <dt className="text-muted">Audio de la cámara</dt>
          <dd className="font-mono text-xs">Disponible</dd>
        </div>
        {detecting !== undefined && (
          <div className="flex justify-between gap-2">
            <dt className="text-muted">Detección de audio</dt>
            <dd className="font-mono text-xs">{detecting ? "Activada" : "Desactivada"}</dd>
          </div>
        )}
      </dl>
      <p className="text-xs text-muted">Habilitar audio, códec, grabar audio y audio en Live View todavía no se pueden editar desde aquí.</p>
    </section>
  );
}
