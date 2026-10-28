import { useQuery } from "@tanstack/react-query";
import { type ComponentProps, forwardRef } from "react";
import { cameraQuery, serversQuery } from "@/api/queries";
import { HlsPlayer, type HlsPlayerHandle } from "@/components/HlsPlayer";
import { ReplayPlayer } from "@/components/ReplayPlayer";

type Props = ComponentProps<typeof HlsPlayer> & { quality?: "sub" | "main" };

/**
 * useRecorderCamera says whether a camera belongs to a Provision-ISR recorder: true or false once known,
 * undefined while the camera and the servers are loading. Both answers are cached by the screens around.
 */
export function useRecorderCamera(cameraId: string): boolean | undefined {
  const camera = useQuery(cameraQuery(cameraId));
  const servers = useQuery(serversQuery);
  if (camera.isError || servers.isError) return false; // the recording player reports the real error
  if (!camera.data || !servers.data) return undefined;
  return servers.data.find((s) => s.id === camera.data.server_id)?.kind === "provision_nvr";
}

/**
 * RecordingPlayer plays the recording of a camera: through HLS for a Frigate camera, through the
 * recorder's replay for a Provision-ISR recorder camera. The two take the same props, so the screens
 * that mount it do not care which one they got.
 */
export const RecordingPlayer = forwardRef<HlsPlayerHandle, Props>(function RecordingPlayer({ quality, ...props }, ref) {
  const recorder = useRecorderCamera(props.cameraId);
  if (recorder === undefined) return <div className={props.className} aria-busy="true" aria-label={props.ariaLabel} />;
  if (recorder) return <ReplayPlayer ref={ref} quality={quality} {...props} />;
  return <HlsPlayer ref={ref} {...props} />;
});
