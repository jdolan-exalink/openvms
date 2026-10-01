export const DRAG_MIME = "application/x-openvms-map-camera";

export interface UnplacedTrayCamera {
  id: string;
  name: string;
  status: string;
}

export interface UnplacedTrayProps {
  cameras: readonly UnplacedTrayCamera[];
  armedId?: string;
  onArm: (cameraId: string) => void;
  /** Offered only when the site has coordinates to place the cameras at. */
  onPlaceAll?: () => void;
  /** Offered only to holders of maps.edit_device (the backend's import permission). */
  onImport?: () => void;
  onClose?: () => void;
}

/**
 * UnplacedTray lists the authorized cameras of the site that have no placement yet: each
 * one can be armed for a click-to-place, or dragged straight onto the map. The bulk action
 * answers PO decision 3 ("ubicar todas en el centro del sitio") for sites that already
 * know where they are.
 */
export function UnplacedTray({ cameras, armedId, onArm, onPlaceAll, onImport, onClose }: UnplacedTrayProps) {
  return (
    <section aria-label="Sin ubicar" className="rounded border border-line bg-surface p-3 shadow-sm">
      <header className="mb-2 flex items-center justify-between">
        <h2 className="text-sm font-semibold">Sin ubicar ({cameras.length})</h2>
        {onClose && (
          <button
            type="button"
            aria-label="Close unplaced tray"
            onClick={onClose}
            className="rounded p-1 text-muted hover:bg-raised hover:text-ink"
          >
            ×
          </button>
        )}
      </header>

      {cameras.length === 0 ? (
        <p className="text-xs text-muted">Todas las cámaras del sitio están ubicadas.</p>
      ) : (
        <>
          <p className="mb-2 text-xs text-muted">Arrastrá una cámara al mapa o tocá una y hacé click donde va.</p>
          <ul className="max-h-48 space-y-1 overflow-auto">
            {cameras.map((camera) => (
              <li key={camera.id}>
                <button
                  type="button"
                  draggable
                  aria-pressed={armedId === camera.id}
                  onDragStart={(event) => event.dataTransfer.setData(DRAG_MIME, camera.id)}
                  onClick={() => onArm(camera.id)}
                  className={
                    armedId === camera.id
                      ? "w-full rounded border border-accent bg-accent/15 px-2 py-1 text-left text-sm text-ink"
                      : "w-full rounded border border-line px-2 py-1 text-left text-sm text-ink hover:bg-raised"
                  }
                >
                  {camera.name}
                </button>
              </li>
            ))}
          </ul>
          {onPlaceAll && (
            <button
              type="button"
              onClick={onPlaceAll}
              className="mt-2 w-full rounded bg-accent px-2 py-1 text-xs font-medium text-white"
            >
              Ubicar todas en el centro del sitio
            </button>
          )}
        </>
      )}
      {onImport && (
        <button
          type="button"
          onClick={onImport}
          className="mt-2 w-full rounded border border-line px-2 py-1 text-xs text-ink hover:bg-raised"
        >
          Importar CSV
        </button>
      )}
    </section>
  );
}
