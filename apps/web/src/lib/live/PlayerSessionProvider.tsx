import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import type { PlayerSession, SessionQuality } from "./PlayerSession";
import { PlayerSessionManager, type PlayerSessionManagerOptions } from "./PlayerSessionManager";

const ManagerContext = createContext<PlayerSessionManager | null>(null);

/**
 * PlayerSessionProvider owns the app-wide PlayerSessionManager. Mounted once in the
 * authenticated shell it outlives routes, so live sessions stay WARM while the user visits
 * other screens. Sessions are dropped when the signed-in user changes or the shell unmounts
 * (logout), so no stream or frame leaks between users.
 */
export function PlayerSessionProvider({
  userId,
  options,
  children,
}: {
  userId?: string;
  options?: PlayerSessionManagerOptions;
  children: ReactNode;
}) {
  const [manager] = useState(() => new PlayerSessionManager(options));
  const lastUser = useRef(userId);
  useEffect(() => {
    // A user that was already known and is now someone else: nothing may carry over.
    if (lastUser.current && userId && lastUser.current !== userId) manager.clear();
    lastUser.current = userId ?? lastUser.current;
  }, [manager, userId]);
  useEffect(() => () => manager.clear(), [manager]);
  return <ManagerContext.Provider value={manager}>{children}</ManagerContext.Provider>;
}

/**
 * usePlayerSession acquires the persistent session for a camera+quality and releases it on
 * change or unmount (the session then stays WARM until its TTL). An empty camera ID disables
 * acquisition, allowing snapshot and prewarm stages to share this hook. Outside a provider it falls
 * back to a private manager, so the session simply lives as long as the component.
 */
export function usePlayerSession(cameraId: string, quality: SessionQuality, serverId?: string): PlayerSession | null {
  const shared = useContext(ManagerContext);
  const [fallback] = useState(() => (shared ? null : new PlayerSessionManager({ warmSessionTTL: 0 })));
  const manager = shared ?? fallback;
  const [session, setSession] = useState<PlayerSession | null>(null);
  // Only used when the session is created, so it must not re-acquire when it changes.
  const serverRef = useRef(serverId);
  useEffect(() => {
    serverRef.current = serverId;
  });
  useEffect(() => {
    if (!manager || !cameraId) return;
    // Acquiring an external resource and publishing it to state is the subscribe pattern the rule allows for.
    setSession(manager.acquire(cameraId, quality, serverRef.current));
    return () => {
      manager.release(cameraId, quality);
      if (manager === fallback) manager.clear();
    };
  }, [manager, fallback, cameraId, quality]);
  return cameraId && session?.cameraId === cameraId ? session : null;
}
