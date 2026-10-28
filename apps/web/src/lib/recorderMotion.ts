/** MOTION_LABEL is the label of the detections made from a recorder's own motion detection. */
export const MOTION_LABEL = "motion";

/** A recorded stretch as the recordings endpoint returns it; `motion` counts motion detections in it. */
export type MotionSpan = { start_time: string; end_time: string; motion?: number };

/** A detection drawn on a timeline, in the shape the recording timelines take their events in. */
export type MotionEvent = {
  id: string;
  camera_id: string;
  start_time: string;
  end_time: string;
  severity: string;
  labels: string[];
};

/**
 * recorderMotionEvents turns the stretches a recorder recorded because it detected motion into detections
 * the timeline can draw. A Provision-ISR recorder has no object analytics of its own, but its record log says
 * what triggered each recording, so without analytics the timeline still shows where something moved.
 */
export function recorderMotionEvents(cameraId: string, spans: readonly MotionSpan[] | undefined): MotionEvent[] {
  return (spans ?? [])
    .filter((s) => (s.motion ?? 0) > 0)
    .map((s) => ({
      id: `motion:${cameraId}:${s.start_time}`,
      camera_id: cameraId,
      start_time: s.start_time,
      end_time: s.end_time,
      severity: "info",
      labels: [MOTION_LABEL],
    }));
}
