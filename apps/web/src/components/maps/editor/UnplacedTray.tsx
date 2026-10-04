import { Building, Server, Video, X } from "lucide-react";
import { Icon } from "@/components/Icon";
import { GripVertical } from "lucide-react";
import { useRef, useState } from "react";

export const DRAG_MIME = "application/x-openvms-map-camera";

export interface UnplacedTrayCamera {
  id: string;
  name: string;
  status: string;
  serverName?: string;
}

export interface UnplacedTrayProps {
  cameras: readonly UnplacedTrayCamera[];
  siteName?: string;
  armedId?: string;
  onArm: (cameraId: string) => void;
  /** Touch/pen direct drag; the receiving canvas validates the actual drop bounds. */
  onPointerDrop?: (cameraId:string,clientX:number,clientY:number)=>void;
  /** Offered only when the site has coordinates to place the cameras at. */
  onPlaceAll?: () => void;
  onClose?: () => void;
}

/**
 * UnplacedTray lists the authorized cameras of the site that have no placement yet: each
 * one can be armed for a click-to-place, or dragged straight onto the map. The bulk action
 * answers PO decision 3 ("ubicar todas en el centro del sitio") for sites that already
 * know where they are.
 */
export function UnplacedTray({ siteName, cameras, armedId, onArm, onPlaceAll, onClose, onPointerDrop }: UnplacedTrayProps) {
  const pointerDrag=useRef<{id:string;x:number;y:number;pointer:number;moved:boolean}|undefined>(undefined);
  const [draggingId, setDraggingId] = useState<string>();
  return (
    <section aria-label="Sin ubicar" className="w-64 rounded-xl border border-line bg-surface p-3 shadow-lg">
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

      {siteName && <p className="mb-3 flex items-center gap-2 text-xs text-muted"><Icon icon={Building} size="xs" className="shrink-0" />{siteName}</p>}
      {cameras.length === 0 ? (
        <p className="text-xs text-muted">Todas las cámaras del sitio están ubicadas.</p>
      ) : (
        <>
          <ul className="max-h-48 space-y-1 overflow-auto">
            {cameras.map((camera) => (
              <li key={camera.id}>
                <button
                  type="button"
                  draggable
                  style={{touchAction:onPointerDrop?"none":undefined}}
                  onPointerDown={event=>{
                    if(!onPointerDrop||event.pointerType==="mouse"||!event.isPrimary)return;
                    pointerDrag.current={id:camera.id,x:event.clientX,y:event.clientY,pointer:event.pointerId,moved:false};
                    event.currentTarget.setPointerCapture(event.pointerId);
                  }}
                  onPointerMove={event=>{
                    const drag=pointerDrag.current;if(!drag||drag.pointer!==event.pointerId)return;
                    if(Math.hypot(event.clientX-drag.x,event.clientY-drag.y)>5){drag.moved=true;setDraggingId(drag.id);}
                  }}
                  onPointerUp={event=>{
                    const drag=pointerDrag.current;if(!drag||drag.pointer!==event.pointerId)return;
                    pointerDrag.current=undefined;setDraggingId(undefined);
                    if(drag.moved){event.preventDefault();onPointerDrop?.(drag.id,event.clientX,event.clientY);}
                  }}
                  onPointerCancel={()=>{pointerDrag.current=undefined;setDraggingId(undefined);}}
                  onLostPointerCapture={()=>{pointerDrag.current=undefined;setDraggingId(undefined);}}
                  aria-pressed={armedId === camera.id}
                  onDragStart={(event) => {
                    event.dataTransfer.setData(DRAG_MIME, camera.id);
                    event.dataTransfer.effectAllowed = "copy";
                    setDraggingId(camera.id);
                  }}
                  onDragEnd={() => setDraggingId(undefined)}
                  aria-label={camera.name}
                  onClick={() => onArm(camera.id)}
                  className={
                    draggingId === camera.id ? "flex w-full cursor-grabbing items-center gap-3 rounded-lg border border-accent bg-accent/10 px-3 py-2.5 opacity-60" : armedId === camera.id
                      ? "flex w-full cursor-grab items-center gap-3 rounded-lg border border-accent bg-accent/15 px-3 py-2.5 text-left text-sm text-ink"
                      : "flex w-full cursor-grab items-center gap-3 rounded-lg border border-line px-3 py-2.5 text-left text-sm text-ink transition-colors hover:border-accent/50 hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent"
                  }
                >
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-raised text-accent"><Icon icon={Video} size="xs" /></span>
                  <span className="min-w-0 flex-1"><span className="block truncate">{camera.name}</span>
                    {camera.serverName && <span className="mt-0.5 flex items-center gap-1 text-[10px] text-muted"><Icon icon={Server} size={12} className="shrink-0" />{camera.serverName}</span>}
                  </span>
                  {camera.status === "offline" ? <Icon icon={X} size="xs" className="text-bad" />
                    : <span className={camera.status === "online" ? "size-1.5 shrink-0 rounded-full bg-ok" : "size-1.5 shrink-0 rounded-full bg-muted"} aria-hidden />}
                  <GripVertical className="size-4 shrink-0 text-muted" aria-hidden />
                </button>
              </li>
            ))}
          </ul>
          {draggingId && <p role="status" className="mt-2 text-xs font-medium text-accent">Suelta la cámara sobre el mapa</p>}
          {onPlaceAll && (
            <button
              type="button"
              onClick={onPlaceAll}
              className="mt-3 w-full rounded border border-line px-2 py-1 text-xs text-muted hover:bg-raised"
            >
              Distribuir provisionalmente
            </button>
          )}
          {onPlaceAll && <p className="mt-1 text-[10px] text-muted">Posiciones aproximadas, no ubicaciones reales.</p>}
        </>
      )}
    </section>
  );
}
