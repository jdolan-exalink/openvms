// Browsers authenticate with the HttpOnly session cookie set by POST /api/v1/auth/login;
// scripts can still paste an API token (issued with `vmsctl token`), kept only for this tab.
const KEY = "openvms.token";

let memory: string | null = null;

export function getToken(): string | null {
  try {
    return sessionStorage.getItem(KEY) ?? memory;
  } catch {
    return memory;
  }
}

export function setToken(token: string) {
  memory = token;
  try {
    sessionStorage.setItem(KEY, token);
  } catch {
    // storage unavailable: the token lives only in memory for this page
  }
}

export function clearToken() {
  memory = null;
  try {
    sessionStorage.removeItem(KEY);
  } catch {
    // nothing stored
  }
}

export function currentToken(): string | null {
  return getToken();
}
