import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { camerasQuery, serversQuery } from "@/api/queries";

const EMPTY: ReadonlySet<string> = new Set();

/**
 * useRecorderCameraIds is the set of cameras that belong to Provision-ISR recorders. It is empty until the
 * cameras and the servers are known, which is the same as having none: the screens that use it only add to
 * what they show.
 */
export function useRecorderCameraIds(): ReadonlySet<string> {
  const cameras = useQuery(camerasQuery());
  const servers = useQuery(serversQuery);
  return useMemo(() => {
    if (!cameras.data || !servers.data) return EMPTY;
    const recorders = new Set(servers.data.filter((s) => s.kind === "provision_nvr").map((s) => s.id));
    return new Set(cameras.data.filter((c) => recorders.has(c.server_id)).map((c) => c.id));
  }, [cameras.data, servers.data]);
}
