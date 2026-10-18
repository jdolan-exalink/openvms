// A server whose saved configuration only takes effect after a Frigate restart is remembered
// for the browser session, so every screen that edits Frigate config can show the same notice.
const restartKey = (serverId: string) => `openvms.frigate-restart.${serverId}`;

export function loadRestart(serverId: string): boolean {
  try {
    return sessionStorage.getItem(restartKey(serverId)) === "1";
  } catch {
    return false;
  }
}

export function saveRestart(serverId: string, on: boolean) {
  try {
    if (on) sessionStorage.setItem(restartKey(serverId), "1");
    else sessionStorage.removeItem(restartKey(serverId));
  } catch {
    /* storage unavailable */
  }
}
