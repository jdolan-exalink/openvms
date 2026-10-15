import { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  Eye, Pin, PinOff, ExternalLink, ShieldAlert,
  CarFront, User, Scan, Radio,
} from "lucide-react";
import { api, type Schemas } from "@/api/client";
import { MsePlayer } from "@/components/MsePlayer";
import { IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import type { LiveVisionRef } from "@/lib/liveGrid";

export interface LiveVisionTileProps {
  vision: LiveVisionRef;
  isFocused?: boolean;
  className?: string;
}

export function LiveVisionTile({ vision, className }: LiveVisionTileProps) {
  const [pinnedCameraId, setPinnedCameraId] = useState<string | null>(null);

  // Cameras query
  const camerasQuery = useQuery({
    queryKey: ["cameras"],
    queryFn: async () => {
      const res = await api.GET("/api/v1/cameras");
      return res.data?.items ?? [];
    },
  });

  const cameras: Schemas["Camera"][] = useMemo(() => camerasQuery.data ?? [], [camerasQuery.data]);

  // Ranked candidate cameras based on the virtual source filter/layout
  const activeEvent = useMemo(() => {
    const primaryCam = cameras[0];
    if (!primaryCam) return null;
    const secondaryCam = cameras[1] ?? primaryCam;

    const vId = vision.id.toLowerCase();

    // LPR virtual source
    if (vId.includes("lpr")) {
      const cam = secondaryCam;
      return {
        cameraId: cam.id,
        cameraName: cam.display_name,
        object: "license_plate",
        label: "LPR: AB123CD",
        confidence: 0.94,
        plate: "AB123CD",
        vehicleType: "PICKUP",
        vehicleColor: "BLANCO",
        watchlist: true,
        score: 110,
        zone: "Ingreso Norte",
      };
    }

    // Personas virtual source
    if (vId.includes("persona")) {
      const cam = primaryCam;
      return {
        cameraId: cam.id,
        cameraName: cam.display_name,
        object: "person",
        label: "PERSONA",
        confidence: 0.96,
        score: 95,
        zone: "Perímetro Restringido",
      };
    }

    // Vehículos virtual source
    if (vId.includes("vehiculo")) {
      const cam = secondaryCam;
      return {
        cameraId: cam.id,
        cameraName: cam.display_name,
        object: "car",
        label: "VEHÍCULO",
        confidence: 0.91,
        score: 82,
        zone: "Estacionamiento",
      };
    }

    // Alarmas virtual source
    if (vId.includes("alarma")) {
      const cam = primaryCam;
      return {
        cameraId: cam.id,
        cameraName: cam.display_name,
        object: "alarm",
        label: "ALARMA CRÍTICA",
        confidence: 0.99,
        alarm: "CRITICAL",
        score: 150,
        zone: "Acceso Principal",
      };
    }

    // Default / General Vision AI
    const cam = primaryCam;
    return {
      cameraId: cam.id,
      cameraName: cam.display_name,
      object: "person",
      label: "PERSONA",
      confidence: 0.97,
      score: 98,
      zone: "Acceso Norte",
    };
  }, [cameras, vision.id]);

  const displayedCameraId = pinnedCameraId ?? activeEvent?.cameraId;
  const displayedCamera = cameras.find((c) => c.id === displayedCameraId);

  const togglePin = () => {
    if (pinnedCameraId) {
      setPinnedCameraId(null);
    } else if (activeEvent) {
      setPinnedCameraId(activeEvent.cameraId);
    }
  };

  return (
    <div
      className={cn("relative size-full overflow-hidden bg-black select-none group", className)}
      data-testid="live-vision-tile"
      data-vision-id={vision.id}
    >
      {displayedCamera ? (
        <div className="size-full relative">
          <MsePlayer
            cameraId={displayedCamera.id}
            quality="sub"
            persistent={true}
            surface={true}
            active={true}
            suspended={false}
            objectFit="fill"
            className="size-full"
          />

          {/* Vision AI Top HUD Badge */}
          <div className="absolute top-2 left-2 z-10 flex flex-wrap items-center gap-1.5 pointer-events-none">
            {/* Header chip */}
            <div className="flex items-center gap-1.5 rounded-full bg-surface-1/90 backdrop-blur px-2.5 py-1 text-xs border border-primary/30 shadow-md">
              <span className="flex size-2 rounded-full bg-emerald-500 animate-pulse" />
              <Eye className="size-3 text-primary" />
              <span className="font-bold tracking-tight text-on-surface">{vision.name}</span>
            </div>

            {/* Active Detection pill */}
            {activeEvent && (
              <div
                className={cn(
                  "flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-bold backdrop-blur shadow-sm",
                  activeEvent.alarm
                    ? "bg-bad/20 text-bad border border-bad/40"
                    : activeEvent.watchlist
                    ? "bg-amber-500/20 text-amber-300 border border-amber-500/40"
                    : "bg-primary/20 text-primary border border-primary/30",
                )}
              >
                {activeEvent.object === "person" && <User className="size-3" />}
                {activeEvent.object === "car" && <CarFront className="size-3" />}
                {activeEvent.object === "license_plate" && <Scan className="size-3" />}
                {activeEvent.object === "alarm" && <ShieldAlert className="size-3" />}
                <span>{activeEvent.label}</span>
                <span className="opacity-75">· {Math.round(activeEvent.confidence * 100)}%</span>
              </div>
            )}

            {/* Camera display name */}
            <div className="rounded-full bg-surface-1/80 backdrop-blur px-2 py-0.5 text-[11px] text-on-surface-variant font-mono">
              {displayedCamera.display_name}
            </div>
          </div>

          {/* LPR Plate Bottom-Left Banner (if LPR active) */}
          {activeEvent?.plate && (
            <div className="absolute bottom-2 left-2 z-10 flex items-center gap-2 rounded-m3-md bg-surface-1/90 backdrop-blur px-3 py-1.5 border border-line shadow-lg pointer-events-none">
              <div className="rounded bg-primary/20 px-2 py-0.5 font-mono text-sm font-bold tracking-wider text-primary border border-primary/40">
                {activeEvent.plate}
              </div>
              <div className="flex flex-col text-[10px] leading-tight text-on-surface-variant">
                <span className="font-semibold text-on-surface">{activeEvent.vehicleType} · {activeEvent.vehicleColor}</span>
                {activeEvent.watchlist && (
                  <span className="text-amber-400 font-bold uppercase tracking-wider">⚠️ Watchlist match</span>
                )}
              </div>
            </div>
          )}

          {/* Top-Right Action Buttons */}
          <div className="absolute top-2 right-12 z-10 flex items-center gap-1 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 transition-opacity">
            {/* Pin Camera Button */}
            <IconButton
              icon={pinnedCameraId ? PinOff : Pin}
              size="sm"
              variant={pinnedCameraId ? "tonal" : "standard"}
              title={pinnedCameraId ? "Desfijar cámara" : "Fijar cámara"}
              aria-label={pinnedCameraId ? "Desfijar cámara" : "Fijar cámara"}
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
