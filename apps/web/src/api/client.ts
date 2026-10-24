import createClient, { type Middleware } from "openapi-fetch";
import { clearToken, currentToken } from "./auth";
import type { components, paths } from "./schema";

export type Schemas = components["schemas"];

// Typed client generated from packages/api-contract/openapi.yaml (`pnpm generate`).
// Same-origin: Caddy (prod) or the Vite proxy (dev) forwards to the API, so the session
// cookie travels with every request. fetch is resolved per call so tests can stub it.
export const api = createClient<paths>({
  baseUrl: window.location.origin,
  fetch: (request) => globalThis.fetch(request),
});

/** Header the API requires on cookie-authenticated writes (CSRF protection). */
export const csrfHeader = { "X-OpenVMS-Request": "1" };

const auth: Middleware = {
  onRequest({ request }) {
    const token = currentToken();
    if (token) request.headers.set("Authorization", `Bearer ${token}`);
    request.headers.set("X-OpenVMS-Request", "1");
    return request;
  },
  onResponse({ request, response }) {
    // An expired session or token sends the user back to the login screen.
    const path = new URL(request.url).pathname;
    if (response.status === 401 && path.startsWith("/api/v1/") && path !== "/api/v1/auth/login") {
      clearToken();
      if (window.location.pathname !== "/login") window.location.assign("/login");
    }
    return response;
  },
};
api.use(auth);

/** ApiError carries the contract's error body so screens can show its message. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

type Result<T> = { data?: T; error?: unknown; response: Response };

function fallbackApiMessage(status: number, emptySuccess = false): string {
  if ([502, 503, 504].includes(status)) {
    return `No se pudo contactar con la API (HTTP ${status}). Revisá docker compose ps y, si el servicio api no está sano, docker compose logs --tail=100 api web.`;
  }
  if (emptySuccess) {
    return `El servidor respondió HTTP ${status}, pero sin los datos esperados. Puede haber una diferencia de versión entre web y API; actualizá con make deploy y recargá la página.`;
  }
  return `La API respondió HTTP ${status} sin un mensaje de error legible. Revisá docker compose logs --tail=100 api web.`;
}

/** unwrap returns data or throws ApiError with the server's message. */
export function unwrap<T>({ data, error, response }: Result<T>): T {
  if (response.ok) {
    if (data !== undefined || response.status === 204) return data as T;
    throw new ApiError(response.status, "empty_response", fallbackApiMessage(response.status, true));
  }
  const body = (error ?? {}) as Partial<Schemas["Error"]>;
  throw new ApiError(response.status, body.code ?? "error", body.message || fallbackApiMessage(response.status));
}

/**
 * postForm sends multipart form data, which the typed client cannot build, with the same
 * session/token and CSRF headers as every other request. It throws ApiError like unwrap.
 */
export async function postForm<T>(path: string, form: FormData): Promise<T> {
  const headers = new Headers(csrfHeader);
  const token = currentToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const response = await globalThis.fetch(path, { method: "POST", body: form, headers, credentials: "same-origin" });
  if (response.status === 401 && window.location.pathname !== "/login") {
    clearToken();
    window.location.assign("/login");
  }
  const body: unknown = await response.json().catch(() => undefined);
  if (!response.ok) {
    const err = (body ?? {}) as Partial<Schemas["Error"]>;
    throw new ApiError(response.status, err.code ?? "error", err.message || fallbackApiMessage(response.status));
  }
  return body as T;
}

/**
 * requestJson calls an endpoint that is not in the OpenAPI contract with the same session/token and
 * CSRF headers as every other request, which a bare fetch lacks (the API refuses cookie-authenticated
 * writes without them). A 204 answer gives undefined. It throws ApiError like unwrap.
 */
export async function requestJson<T>(method: "GET" | "POST" | "PUT" | "DELETE", path: string, body?: unknown): Promise<T> {
  const headers = new Headers(csrfHeader);
  headers.set("Accept", "application/json");
  const token = currentToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (body !== undefined) headers.set("Content-Type", "application/json");
  const response = await globalThis.fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "same-origin",
  });
  if (response.status === 401 && window.location.pathname !== "/login") {
    clearToken();
    window.location.assign("/login");
  }
  const parsed: unknown = await response.json().catch(() => undefined);
  if (!response.ok) {
    const err = (parsed ?? {}) as Partial<Schemas["Error"]>;
    throw new ApiError(response.status, err.code ?? "error", err.message || fallbackApiMessage(response.status));
  }
  return parsed as T;
}
