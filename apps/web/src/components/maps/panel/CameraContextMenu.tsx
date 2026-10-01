import { useEffect, useRef } from "react";
import type { CameraEntity } from "@/lib/maps/types";
import { Copy, ExternalLink, Pin, Plus } from "lucide-react";

export interface CameraContextMenuProps {
  camera: CameraEntity;
  position: { x: number; y: number };
  onClose: () => void;
  onOpenLive: (cameraId: string) => void;
  onAddToLive?: (cameraId: string) => void;
  onPin: (cameraId: string) => void;
}

export function CameraContextMenu({
  camera,
  position,
  onClose,
  onOpenLive,
  onAddToLive,
  onPin,
}: CameraContextMenuProps) {
  const menuRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };

    window.addEventListener("mousedown", handleClickOutside);
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("mousedown", handleClickOutside);
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [onClose]);

  const handleCopyCoords = () => {
    if (camera.position.kind === "geo") {
      const text = `${camera.position.lat}, ${camera.position.lng}`;
      void navigator.clipboard?.writeText(text);
    }
    onClose();
  };

  return (
    <div
      ref={menuRef}
      style={{ left: `${position.x}px`, top: `${position.y}px` }}
      className="pointer-events-auto absolute z-40 min-w-48 rounded-lg border border-line bg-surface p-1 shadow-2xl text-xs animate-in fade-in zoom-in-95 duration-100"
    >
      <div className="px-2.5 py-1.5 border-b border-line text-muted font-medium truncate">
        {camera.name}
      </div>

      <div className="py-1">
        <button
          type="button"
          onClick={() => {
            onOpenLive(camera.id);
            onClose();
          }}
          className="flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-ink hover:bg-raised transition-colors"
        >
          <ExternalLink className="size-3.5 text-muted" />
          <span>Open in Live View</span>
        </button>

        {onAddToLive && (
          <button
            type="button"
            onClick={() => {
              onAddToLive(camera.id);
              onClose();
            }}
            className="flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-ink hover:bg-raised transition-colors"
          >
            <Plus className="size-3.5 text-muted" />
            <span>Add to Live View Grid</span>
          </button>
        )}

        <button
          type="button"
          onClick={() => {
            onPin(camera.id);
            onClose();
          }}
          className="flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-ink hover:bg-raised transition-colors"
        >
          <Pin className="size-3.5 text-muted" />
          <span>Pin Preview</span>
        </button>

        {camera.position.kind === "geo" && (
          <button
            type="button"
            onClick={handleCopyCoords}
            className="flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-ink hover:bg-raised transition-colors"
          >
            <Copy className="size-3.5 text-muted" />
            <span>Copy Coordinates</span>
          </button>
        )}
      </div>
    </div>
  );
}
