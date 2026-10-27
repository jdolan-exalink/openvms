import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap } from "@/api/client";

export type PtzCommand = {
  action: "continuous_move" | "stop" | "goto_preset" | "set_preset" | "remove_preset" | "goto_home" | "set_home";
  pan?: number;
  tilt?: number;
  zoom?: number;
  presetToken?: string;
  presetName?: string;
};

const STATUS: Record<PtzCommand["action"], string> = {
  continuous_move: "Comando PTZ enviado.",
  stop: "Movimiento detenido.",
  goto_preset: "Preset ejecutado.",
  set_preset: "Preset guardado.",
  remove_preset: "Preset eliminado.",
  goto_home: "Cámara en la posición home.",
  set_home: "Posición home guardada.",
};

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
          body: { action: cmd.action, pan: cmd.pan, tilt: cmd.tilt, zoom: cmd.zoom, preset_token: cmd.presetToken, preset_name: cmd.presetName },
        }),
      ),
    onSuccess: (_res, cmd) => {
      setStatus(STATUS[cmd.action]);
      if (cmd.action === "goto_preset" || cmd.action === "set_preset" || cmd.action === "remove_preset") void qc.invalidateQueries({ queryKey: ["cameras", cameraId, "ptz-presets"] });
      // A manual movement cancels the running tour on the server.
      if (["continuous_move", "stop", "goto_preset", "goto_home"].includes(cmd.action)) void qc.invalidateQueries({ queryKey: ["cameras", cameraId, "ptz-tours"] });
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
    /** The name is optional; with an existing token this renames (and re-saves) that preset. */
    savePreset: (presetName: string, presetToken?: string) => mutation.mutate({ action: "set_preset", presetName: presetName.trim() || undefined, presetToken }),
    removePreset: (presetToken: string) => mutation.mutate({ action: "remove_preset", presetToken }),
    gotoHome: () => mutation.mutate({ action: "goto_home" }),
    setHome: () => mutation.mutate({ action: "set_home" }),
  };
}
