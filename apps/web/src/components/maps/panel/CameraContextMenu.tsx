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
      className="pointer-events-auto absolute z-40 min-w-48 rounded-m3-xl bg-surface-3 p-2 shadow-2xl text-xs animate-in fade-in zoom-in-95 duration-100"
    >
      <div className="px-3 py-1.5 text-on-surface-variant font-bold truncate">
        {camera.name}
      </div>

      <div className="py-1">
        <button
          type="button"
          onClick={() => {
            onOpenLive(camera.id);
            onClose();
          }}
          className="flex min-h-11 w-full items-center gap-3 rounded-full px-3 text-left text-on-surface hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary transition-colors"
        >
          <ExternalLink className="size-4 text-on-surface-variant" />
          <span>Maximizar</span>
        </button>

        {onAddToLive && (
          <button
            type="button"
            onClick={() => {
              onAddToLive(camera.id);
              onClose();
            }}
            className="flex min-h-11 w-full items-center gap-3 rounded-full px-3 text-left text-on-surface hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary transition-colors"
          >
            <Plus className="size-4 text-on-surface-variant" />
            <span>Add to Live View Grid</span>
          </button>
        )}

        <button
          type="button"
          onClick={() => {
            onPin(camera.id);
            onClose();
          }}
          className="flex min-h-11 w-full items-center gap-3 rounded-full px-3 text-left text-on-surface hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary transition-colors"
        >
          <Pin className="size-4 text-on-surface-variant" />
          <span>Pin Preview</span>
        </button>

        {camera.position.kind === "geo" && (
          <button
            type="button"
            onClick={handleCopyCoords}
            className="flex min-h-11 w-full items-center gap-3 rounded-full px-3 text-left text-on-surface hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary transition-colors"
          >
            <Copy className="size-4 text-on-surface-variant" />
            <span>Copy Coordinates</span>
          </button>
        )}
      </div>
    </div>
  );
}
