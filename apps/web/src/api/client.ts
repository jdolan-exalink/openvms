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

/** unwrap returns data or throws ApiError with the server's message. */
export function unwrap<T>({ data, error, response }: Result<T>): T {
  if (response.ok && (data !== undefined || response.status === 204)) return data as T;
  const body = (error ?? {}) as Partial<Schemas["Error"]>;
  throw new ApiError(response.status, body.code ?? "error", body.message ?? `La API respondió ${response.status}`);
}
