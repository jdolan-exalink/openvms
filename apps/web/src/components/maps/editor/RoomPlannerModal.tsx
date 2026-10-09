import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Maximize2, Minimize2, X } from "lucide-react";
import { Button } from "@/components/ui";

interface RoomPlannerModalProps {
  initialState?: unknown;
  mapName?: string;
  onSave: (file: File, state: unknown) => Promise<void> | void;
  onClose: () => void;
}

export function RoomPlannerModal({
  initialState,
  mapName,
  onSave,
  onClose,
}: RoomPlannerModalProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();
  const [isMaximized, setIsMaximized] = useState(true);

  // Send initial state to the planner when ready
  const sendInitialState = () => {
    if (initialState && iframeRef.current?.contentWindow) {
      iframeRef.current.contentWindow.postMessage(
        {
          type: "OPENVMS_LOAD_PLAN",
          state: initialState,
        },
        "*"
      );
    }
  };

  useEffect(() => {
    const handleMessage = async (event: MessageEvent) => {
      if (!event.data || typeof event.data !== "object") return;

      if (event.data.type === "OPENVMS_PLANNER_READY") {
        sendInitialState();
      } else if (event.data.type === "OPENVMS_CLOSE_PLANNER") {
        onClose();
      } else if (event.data.type === "OPENVMS_SAVE_PLAN") {
        const { svg, pngDataUrl, state } = event.data;
        setSaving(true);
        setError(undefined);

        try {
          let file: File;
          if (svg && typeof svg === "string" && svg.includes("<svg")) {
            const blob = new Blob([svg], { type: "image/svg+xml" });
            file = new File([blob], `${mapName || "room-plan"}.svg`, {
              type: "image/svg+xml",
            });
          } else if (pngDataUrl && typeof pngDataUrl === "string") {
            const res = await fetch(pngDataUrl);
            const blob = await res.blob();
            file = new File([blob], `${mapName || "room-plan"}.png`, {
              type: "image/png",
            });
          } else {
            throw new Error("No se pudo obtener el gráfico del plano.");
          }

          await onSave(file, state);
        } catch (cause) {
          setError(
            cause instanceof Error
              ? cause.message
              : "No se pudo guardar el plano diseñado."
          );
          setSaving(false);
        }
      }
    };

    window.addEventListener("message", handleMessage);
    return () => {
      window.removeEventListener("message", handleMessage);
    };
  }, [initialState, mapName, onSave, onClose]);

  const modalContent = (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Diseñador de planos"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-2 sm:p-4 backdrop-blur-xs"
    >
      <div
        className={`flex flex-col bg-surface-1 border border-outline-variant/40 shadow-2xl overflow-hidden transition-all duration-200 ${
          isMaximized
            ? "w-full h-full rounded-m3-lg sm:rounded-m3-xl"
            : "w-[94vw] h-[88vh] max-w-6xl rounded-m3-xl"
        }`}
      >
        {/* Header */}
        <header className="flex items-center justify-between px-4 py-2.5 bg-surface-2 border-b border-outline-variant/30 shrink-0">
          <div className="flex items-center gap-2 min-w-0">
            <span className="flex size-7 items-center justify-center rounded-full bg-primary/15 text-primary text-xs font-bold">
              📐
            </span>
            <div className="min-w-0">
              <h2 className="text-sm font-bold text-on-surface truncate">
                Diseñador de planos {mapName ? `· ${mapName}` : ""}
              </h2>
              <p className="text-[11px] text-on-surface-variant truncate">
                Dibujá paredes, puertas, ventanas y ambientes. Al finalizar, hacé clic en «Guardar en OpenVMS».
              </p>
            </div>
          </div>

          <div className="flex items-center gap-1.5 shrink-0">
            {saving && (
              <span className="text-xs text-primary font-medium animate-pulse mr-2">
                Guardando plano…
              </span>
            )}
            {error && (
              <span className="text-xs text-bad font-medium mr-2 max-w-xs truncate" title={error}>
                {error}
              </span>
            )}
            <Button
              variant="text"
              size="sm"
              onClick={() => setIsMaximized(!isMaximized)}
              title={isMaximized ? "Restaurar ventana" : "Pantalla completa"}
              aria-label={isMaximized ? "Restaurar ventana" : "Pantalla completa"}
            >
              {isMaximized ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
            </Button>
            <Button
              variant="text"
              size="sm"
              onClick={onClose}
              title="Cerrar editor"
              aria-label="Cerrar editor"
            >
              <X className="size-4 text-bad" />
            </Button>
          </div>
        </header>

        {/* Embedded Planner Iframe */}
        <div className="relative flex-1 min-h-0 w-full bg-surface-3">
          <iframe
            ref={iframeRef}
            src="/room-planner/index.html"
            title="Room Planner"
            className="w-full h-full border-0"
            onLoad={sendInitialState}
          />
        </div>
      </div>
    </div>
  );

  return createPortal(modalContent, document.body);
}
