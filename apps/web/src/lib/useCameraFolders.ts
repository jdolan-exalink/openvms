import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { api, unwrap } from "@/api/client";
import { cameraFoldersQuery } from "@/api/queries";
import { applyReorder, type Camera, type Folder, type Reorder, resolveTreeDrop } from "@/lib/explorer";

type Snapshot = { cameras: [readonly unknown[], Camera[] | undefined][]; folders: Folder[] | undefined };
type FolderData = { items: Folder[]; manageable_server_ids: string[] };

/**
 * useCameraFolders wraps the shared folder API for the Live explorer: the folder list, the servers
 * the caller may manage, CRUD mutations and an optimistic reorder that rolls back on API errors.
 */
export function useCameraFolders(cameras: Camera[] | undefined) {
  const qc = useQueryClient();
  const query = useQuery(cameraFoldersQuery);
  const folders = useMemo(() => query.data?.items ?? [], [query.data]);
  const manageable = useMemo(() => new Set(query.data?.manageable_server_ids ?? []), [query.data]);

  const refresh = async () => {
    await Promise.all([qc.invalidateQueries({ queryKey: ["camera-folders"] }), qc.invalidateQueries({ queryKey: ["cameras"] })]);
  };

  const create = useMutation({
    mutationFn: async (v: { serverId: string; name: string }) =>
      unwrap(await api.POST("/api/v1/camera-folders", { body: { server_id: v.serverId, name: v.name } })),
    onSuccess: refresh,
  });
  const rename = useMutation({
    mutationFn: async (v: { id: string; name: string }) =>
      unwrap(await api.PATCH("/api/v1/camera-folders/{folderId}", { params: { path: { folderId: v.id } }, body: { name: v.name } })),
    onSuccess: refresh,
  });
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/api/v1/camera-folders/{folderId}", { params: { path: { folderId: id } } })),
    onSuccess: refresh,
  });

  const reorder = useMutation({
    mutationFn: async (r: Reorder) => unwrap(await api.POST("/api/v1/camera-folders/reorder", { body: r })),
    onMutate: async (r): Promise<Snapshot> => {
      await Promise.all([qc.cancelQueries({ queryKey: ["cameras"] }), qc.cancelQueries({ queryKey: ["camera-folders"] })]);
      const snapshot: Snapshot = {
        cameras: qc.getQueriesData<Camera[]>({ queryKey: ["cameras"] }),
        folders: qc.getQueryData<FolderData>(["camera-folders"])?.items,
      };
      const currentFolders = snapshot.folders ?? [];
      qc.setQueriesData<Camera[]>({ queryKey: ["cameras"] }, (old) => (Array.isArray(old) ? applyReorder(old, currentFolders, r).cameras : old));
      qc.setQueryData<FolderData>(["camera-folders"], (old) => (old ? { ...old, items: applyReorder([], old.items, r).folders } : old));
      return snapshot;
    },
    onError: (_err, _vars, snapshot) => {
      if (!snapshot) return;
      for (const [key, data] of snapshot.cameras) qc.setQueryData(key, data);
      qc.setQueryData<FolderData>(["camera-folders"], (old) => (old && snapshot.folders ? { ...old, items: snapshot.folders } : old));
    },
    onSettled: refresh,
  });

  /** Handles a finished drag: returns "rejected" for a cross-server drop, "applied" when it changed the tree. */
  const drop = (activeId: string | number, overId: string | number | null): "rejected" | "applied" | "ignored" => {
    const res = resolveTreeDrop(activeId, overId, cameras ?? [], folders);
    if (!res) return "ignored";
    if ("rejected" in res) return "rejected";
    reorder.mutate(res.reorder);
    return "applied";
  };

  return { folders, manageable, loading: query.isPending, create, rename, remove, reorder, drop };
}
