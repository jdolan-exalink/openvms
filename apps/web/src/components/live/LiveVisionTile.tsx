import { useState } from "react";
import { Link } from "@tanstack/react-router";
import {
  Eye, Pin, PinOff, ExternalLink,
  Radio,
} from "lucide-react";
import { MsePlayer } from "@/components/MsePlayer";
import { IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { ObjectBadge } from "@/components/visionai/ObjectBadge";
import type { LiveVisionRef } from "@/lib/liveGrid";
import { useVisionSlots, visionSourceFromId, type VisionAISlot } from "@/lib/visionAi";
import { channelKindOf } from "@/lib/detectionChannels";
import { DetectionChannel } from "@/components/visionai/DetectionChannel";
import { DetectionListOverlay } from "@/components/live/DetectionListOverlay";

export interface LiveVisionTileProps {
  vision: LiveVisionRef;
  /** The tile is the focused one in the Live grid: use the main stream. */
  isFocused?: boolean;
  className?: string;
}

export function LiveVisionTile(props: LiveVisionTileProps) {
  const kind = channelKindOf(props.vision.id);
  if (kind) {
    return (
      <div className={cn("relative size-full overflow-hidden", props.className)} data-testid="live-vision-tile" data-vision-id={props.vision.id}>
        <DetectionChannel kind={kind} />
      </div>
    );
  }
  return <VisionSlotTile {...props} />;
}

function VisionSlotTile({ vision, isFocused = false, className }: LiveVisionTileProps) {
  // Pinning freezes the slot's current assignment (client side).
  // The pin belongs to the source it was taken from: changing source clears it, so returning to a
  // previous source starts unpinned (state is reset during render, before children see it).
  const [pin, setPin] = useState<{ sourceId: string; slot: VisionAISlot } | null>(null);
  if (pin && pin.sourceId !== vision.id) setPin(null);
  const pinnedSlot = pin && pin.sourceId === vision.id ? pin.slot : null;

  const slotsQuery = useVisionSlots(visionSourceFromId(vision.id), "1");
  const rankedSlot = slotsQuery.data?.slots[0];

  const activeSlot = pinnedSlot ?? (rankedSlot?.camera_id ? rankedSlot : null);
  const cameraId = activeSlot?.camera_id ?? null;
  const object = activeSlot?.object;

  const togglePin = () => {
    if (pinnedSlot) {
      setPin(null);
    } else if (activeSlot) {
      setPin({ sourceId: vision.id, slot: activeSlot });
    }
  };

  return (
    <div
      className={cn("relative size-full overflow-hidden bg-black select-none group", className)}
      data-testid="live-vision-tile"
      data-vision-id={vision.id}
    >
      {cameraId && activeSlot ? (
        <div className="size-full relative">
          <MsePlayer
            key={cameraId}
            cameraId={cameraId}
            quality={isFocused ? "main" : "sub"}
            persistent={true}
            surface={true}
            active={true}
            suspended={false}
            objectFit="fill"
            className="size-full"
          />

          <DetectionListOverlay cameraId={cameraId} lpr={Boolean(activeSlot.plate) || vision.layout === "lpr"} faces={activeSlot.object === "face"} />

          {/* Vision AI Top HUD Badge */}
          <div className="absolute top-2 left-2 z-10 flex flex-wrap items-center gap-1.5 pointer-events-none">
            {/* Header chip */}
            <div className="flex items-center gap-1.5 rounded-full bg-surface-1/90 backdrop-blur px-2.5 py-1 text-xs border border-primary/30 shadow-md">
              <span className="flex size-2 rounded-full bg-emerald-500 animate-pulse" />
              <Eye className="size-3 text-primary" />
              <span className="font-bold tracking-tight text-on-surface">{vision.name}</span>
            </div>

            {/* What was detected: icon only, the name and confidence live in its tooltip */}
            <ObjectBadge object={object} confidence={activeSlot.confidence} alarm={Boolean(activeSlot.alarm)} />

            {/* Camera display name */}
            <div className="rounded-full bg-surface-1/80 backdrop-blur px-2 py-0.5 text-[11px] text-on-surface-variant font-mono">
              {activeSlot.camera_name}
            </div>
          </div>

          {/* LPR Plate Bottom-Left Banner (only when the slot carries a plate) */}
          {activeSlot.plate && (
            <div className="absolute bottom-2 left-2 z-10 flex items-center gap-2 rounded-m3-md bg-surface-1/90 backdrop-blur px-3 py-1.5 border border-line shadow-lg pointer-events-none">
              <div className="rounded bg-primary/20 px-2 py-0.5 font-mono text-sm font-bold tracking-wider text-primary border border-primary/40">
                {activeSlot.plate}
              </div>
            </div>
          )}

          {/* Top-Right Action Buttons */}
          <div className="absolute top-2 right-12 z-10 flex items-center gap-1 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 transition-opacity">
            {/* Pin Camera Button */}
            <IconButton
              icon={pinnedSlot ? PinOff : Pin}
              size="sm"
              variant={pinnedSlot ? "tonal" : "standard"}
              title={pinnedSlot ? "Desfijar cámara" : "Fijar cámara"}
              aria-label={pinnedSlot ? "Desfijar cámara" : "Fijar cámara"}
              onClick={togglePin}
              className="bg-surface-1/80 backdrop-blur text-on-surface size-7 hover:bg-surface-1"
            />

            {/* Open in Vision AI Full Page */}
            <Link
              to="/vision-ai"
              title="Abrir página Vision AI"
              aria-label="Abrir página Vision AI"
              className="flex items-center justify-center size-7 rounded-full bg-surface-1/80 backdrop-blur text-on-surface hover:bg-surface-1 focus-visible:outline-2 focus-visible:outline-primary"
            >
              <ExternalLink className="size-3.5" />
            </Link>
          </div>
        </div>
      ) : (
        /* Radar / Scanner State when searching for events or cameras */
        <div className="size-full flex flex-col items-center justify-center bg-surface-dim p-4 text-center">
          <div className="relative flex size-20 items-center justify-center mb-3">
            <div className="absolute inset-0 rounded-full border border-primary/20 animate-ping" />
            <div className="absolute inset-2 rounded-full border border-primary/30" />
            <div className="absolute inset-4 rounded-full border border-dashed border-primary/40 animate-spin" />
            <Radio className="size-8 text-primary animate-pulse" />
          </div>
          <div className="flex items-center gap-1.5 font-bold text-xs uppercase tracking-wider text-ink mb-1">
            <span className="flex size-2 rounded-full bg-primary animate-pulse" />
            <span>{vision.name}</span>
          </div>
          <p className="text-[11px] text-muted max-w-xs">
            Motor de atención visual activo. Buscando actividad inteligente...
          </p>
          <Link
            to="/vision-ai"
            className="mt-3 inline-flex items-center gap-1 text-xs text-primary font-medium hover:underline"
          >
            <span>Configurar reglas en Vision AI</span>
            <ExternalLink className="size-3" />
          </Link>
        </div>
      )}
    </div>
  );
}
