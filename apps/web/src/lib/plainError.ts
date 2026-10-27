/** A failure in plain words plus the sanitized technical text kept behind "Ver información técnica". */
export type PlainError = { message: string; technical: string };

type WithError = { plain_error?: PlainError; state?: { last_error?: string } };

export const GENERIC_APPLY_ERROR = "No se pudo aplicar la configuración.";

/**
 * The failure to show for an apply/rollback result: the API's `plain_error` when present, else a
 * generic message with `state.last_error` as the technical text. The raw text is never the message.
 */
export function plainErrorOf(result: WithError | null | undefined, fallback = GENERIC_APPLY_ERROR): PlainError | null {
  if (!result) return null;
  if (result.plain_error?.message) return { message: result.plain_error.message, technical: result.plain_error.technical || result.state?.last_error || "" };
  const raw = result.state?.last_error;
  return raw ? { message: fallback, technical: raw } : null;
}
