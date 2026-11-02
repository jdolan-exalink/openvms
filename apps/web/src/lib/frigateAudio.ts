import { setIn } from "@/lib/frigateSchema";

type Input = { roles?: unknown };

/** What Frigate says when audio detection is on and no stream carries the audio role. */
export const AUDIO_ROLE_MESSAGE = "La detección de audio está activada, pero ningún stream de entrada tiene el rol Audio. Marcá Audio en un stream (Video → Streams de entrada) o desactivá la detección de audio.";

function inputsOf(ffmpeg: unknown): Input[] {
  const list = (ffmpeg as { inputs?: unknown } | undefined)?.inputs;
  return Array.isArray(list) ? (list as Input[]) : [];
}

const hasAudio = (input: Input) => Array.isArray(input.roles) && input.roles.includes("audio");

/**
 * audioNeedsInput is true when audio detection is on but no ffmpeg input carries the audio role, the
 * combination Frigate refuses to save. A camera with no listed inputs is left alone: it inherits them.
 */
export function audioNeedsInput(audio: unknown, ffmpeg: unknown): boolean {
  if ((audio as { enabled?: unknown } | undefined)?.enabled !== true) return false;
  const inputs = inputsOf(ffmpeg);
  return inputs.length > 0 && !inputs.some(hasAudio);
}

/**
 * withAudioRole gives the audio role to the only input a camera has. With several inputs it changes
 * nothing, because which stream carries sound is the operator's call.
 */
export function withAudioRole(ffmpeg: unknown): unknown {
  const inputs = inputsOf(ffmpeg);
  const only = inputs[0];
  if (inputs.length !== 1 || !only || hasAudio(only)) return ffmpeg;
  const roles = Array.isArray(only.roles) ? only.roles : [];
  return setIn(ffmpeg, ["inputs", 0, "roles"], [...roles, "audio"]);
}
