import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap } from "@/api/client";

export type PtzCommand = { action: "continuous_move" | "stop" | "goto_preset"; pan?: number; tilt?: number; zoom?: number; presetToken?: string };

export type PtzDirection = "up" | "down" | "left" | "right" | "zoom_in" | "zoom_out";

/** Speed is a 0.1..1 fraction of the device's maximum velocity (ONVIF generic velocity space). */
export const MIN_SPEED = 0.1;

/** Velocity vector of one pad direction at the given speed. */
export function velocityOf(direction: PtzDirection, speed: number): Pick<PtzCommand, "pan" | "tilt" | "zoom"> {
  const s = Math.min(1, Math.max(MIN_SPEED, speed));
  switch (direction) {
    case "up": return { tilt: s };
    case "down": return { tilt: -s };
    case "left": return { pan: -s };
    case "right": return { pan: s };
    case "zoom_in": return { zoom: s };
    case "zoom_out": return { zoom: -s };
  }
}

/**
 * Direct PTZ control through the ONVIF endpoint the editor already uses. The server resolves the
 * camera's stored credentials, so none are sent from here.
 */
export function usePtzControl(cameraId: string) {
  const qc = useQueryClient();
  const [speed, setSpeed] = useState(0.5);
  const [status, setStatus] = useState<string | null>(null);
  const mutation = useMutation({
    mutationFn: async (cmd: PtzCommand) =>
      unwrap(
        await api.POST("/api/v1/cameras/{cameraId}/onvif/ptz", {
          params: { path: { cameraId } },
          body: { action: cmd.action, pan: cmd.pan, tilt: cmd.tilt, zoom: cmd.zoom, preset_token: cmd.presetToken },
        }),
      ),
    onSuccess: (_res, cmd) => {
      setStatus(cmd.action === "stop" ? "Movimiento detenido." : cmd.action === "goto_preset" ? "Preset ejecutado." : "Comando PTZ enviado.");
      if (cmd.action === "goto_preset") void qc.invalidateQueries({ queryKey: ["cameras", cameraId, "ptz-presets"] });
    },
    onError: () => setStatus(null),
  });
  return {
    speed,
    setSpeed,
    status,
    error: mutation.error,
    pending: mutation.isPending,
    move: (direction: PtzDirection) => mutation.mutate({ action: "continuous_move", ...velocityOf(direction, speed) }),
    stop: () => mutation.mutate({ action: "stop" }),
    gotoPreset: (presetToken: string) => mutation.mutate({ action: "goto_preset", presetToken }),
  };
}
