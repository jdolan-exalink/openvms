/**
 * Error codes the media gateway sends in a `{"type":"error","code":...}` frame before it
 * closes a live websocket (LV-7), mapped to what the UI shows and whether retrying can help.
 */
export type StreamErrorCode =
  | "camera_offline"
  | "upstream_unreachable"
  | "unauthorized"
  | "forbidden"
  | "codec_unsupported"
  | "server_error"
  // Client-side conditions that behave like gateway errors.
  | "handshake_rejected"
  | "mse_unsupported";

export type SessionError = {
  code: StreamErrorCode;
  /** Short user-facing title, shown over the last frame. */
  label: string;
  /** True when automatic retries (with backoff) can recover; false stops until "Reintentar". */
  retryable: boolean;
};

const ERRORS: Readonly<Record<StreamErrorCode, SessionError>> = {
  camera_offline: { code: "camera_offline", label: "Cámara sin conexión", retryable: true },
  upstream_unreachable: { code: "upstream_unreachable", label: "Servidor no disponible", retryable: true },
  unauthorized: { code: "unauthorized", label: "Sesión expirada", retryable: false },
  forbidden: { code: "forbidden", label: "Sin permiso", retryable: false },
  codec_unsupported: { code: "codec_unsupported", label: "Códec no soportado", retryable: false },
  server_error: { code: "server_error", label: "Error de stream", retryable: true },
  handshake_rejected: { code: "handshake_rejected", label: "La cámara no tiene una transmisión de video disponible.", retryable: false },
  mse_unsupported: { code: "mse_unsupported", label: "El navegador no soporta Media Source Extensions.", retryable: false },
};

export function streamError(code: StreamErrorCode): SessionError {
  return ERRORS[code];
}

/**
 * describeStreamError maps a gateway error frame to a SessionError. Frames from older
 * gateways (free-text `value`, no code) and unknown codes are treated as a retryable
 * server_error so a new server-side code never stops a stream by accident.
 */
export function describeStreamError(frame: { code?: unknown; value?: unknown }): SessionError {
  const code = typeof frame.code === "string" ? frame.code : typeof frame.value === "string" ? frame.value : "";
  return Object.prototype.hasOwnProperty.call(ERRORS, code) ? ERRORS[code as StreamErrorCode] : ERRORS.server_error;
}
