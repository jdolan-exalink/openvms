import { Building, Server, Video, X } from "lucide-react";
import { Icon } from "@/components/Icon";
import { Button, IconButton } from "@/components/ui";
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
    <section aria-label="Sin ubicar" className="w-64 rounded-m3-xl bg-surface-1/95 p-4 shadow-lg backdrop-blur">
      <header className="mb-2 flex items-center justify-between">
        <h2 className="text-lg font-bold">Sin ubicar ({cameras.length})</h2>
        {onClose && (
          <IconButton icon={X} aria-label="Close unplaced tray" onClick={onClose} className="-mr-2" />
        )}
      </header>

      {siteName && <p className="mb-3 flex items-center gap-2 text-xs text-on-surface-variant"><Icon icon={Building} size="xs" className="shrink-0" />{siteName}</p>}
      {cameras.length === 0 ? (
        <p className="text-xs text-on-surface-variant">Todas las cámaras del sitio están ubicadas.</p>
      ) : (
        <>
          <ul className="max-h-48 space-y-2 overflow-auto">
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
                    draggingId === camera.id ? "m3-press flex min-h-11 w-full cursor-grabbing items-center gap-3 rounded-m3-lg bg-primary-container px-3 py-2.5 opacity-60" : armedId === camera.id
                      ? "m3-press flex min-h-11 w-full cursor-grab items-center gap-3 rounded-m3-lg bg-primary-container px-3 py-2.5 text-left text-sm text-on-primary-container"
                      : "m3-press flex min-h-11 w-full cursor-grab items-center gap-3 rounded-m3-lg bg-surface-2 px-3 py-2.5 text-left text-sm text-on-surface transition-colors hover:bg-surface-3 focus-visible:outline-2 focus-visible:outline-primary"
                  }
                >
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-m3-md bg-surface-3 text-primary"><Icon icon={Video} size="xs" /></span>
                  <span className="min-w-0 flex-1"><span className="block truncate">{camera.name}</span>
                    {camera.serverName && <span className="mt-0.5 flex items-center gap-1 font-mono text-[10px] text-on-surface-variant"><Icon icon={Server} size={12} className="shrink-0" />{camera.serverName}</span>}
                  </span>
                  {camera.status === "offline" ? <Icon icon={X} size="xs" className="text-bad" />
                    : <span className={camera.status === "online" ? "size-2 shrink-0 rounded-full bg-ok" : "size-2 shrink-0 rounded-full bg-muted"} aria-hidden />}
                  <GripVertical className="size-4 shrink-0 text-on-surface-variant" aria-hidden />
                </button>
              </li>
            ))}
          </ul>
          {draggingId && <p role="status" className="mt-2 text-xs font-bold text-primary">Suelta la cámara sobre el mapa</p>}
          {onPlaceAll && (
            <Button variant="tonal" size="sm" onClick={onPlaceAll} className="mt-3 w-full">
              Distribuir provisionalmente
            </Button>
          )}
          {onPlaceAll && <p className="mt-1 text-[10px] text-on-surface-variant">Posiciones aproximadas, no ubicaciones reales.</p>}
        </>
      )}
    </section>
  );
}
