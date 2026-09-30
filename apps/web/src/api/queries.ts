import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api, unwrap } from "./client";

export const readinessQuery = queryOptions({
  queryKey: ["health", "ready"],
  // 503 still carries a body describing which dependency failed, so both codes are data.
  queryFn: async () => {
    const { data, error, response } = await api.GET("/health/ready");
    const body = data ?? error;
    if (!body) throw new Error(`readiness request failed with ${response.status}`);
    return body;
  },
  refetchInterval: 10_000,
});

export const systemInfoQuery = queryOptions({
  queryKey: ["system", "info"],
  queryFn: async () => {
    const { data, response } = await api.GET("/api/v1/system/info");
    if (!data) throw new Error(`system info request failed with ${response.status}`);
    return data;
  },
});

export const featuresQuery = queryOptions({
  queryKey: ["features"],
  queryFn: async () => unwrap(await api.GET("/api/v1/features")),
  staleTime: 5 * 60_000,
  retry: false,
});

export const meQuery = queryOptions({
  queryKey: ["me"],
  queryFn: async () => unwrap(await api.GET("/api/v1/me")),
  staleTime: 60_000,
});

export const tenantsQuery = queryOptions({
  queryKey: ["tenants"],
  queryFn: async () => unwrap(await api.GET("/api/v1/tenants")).items,
});

export const sitesQuery = queryOptions({
  queryKey: ["sites"],
  queryFn: async () => unwrap(await api.GET("/api/v1/sites")).items,
});

export const serversQuery = queryOptions({
  queryKey: ["servers"],
  queryFn: async () => unwrap(await api.GET("/api/v1/servers")).items,
  // The worker refreshes health every 30 s; keep the screen close to it.
  refetchInterval: 15_000,
});

export type CameraFilter = { site_id?: string; server_id?: string; q?: string };

export const camerasQuery = (filter: CameraFilter = {}) =>
  queryOptions({
    queryKey: ["cameras", filter],
    queryFn: async () => unwrap(await api.GET("/api/v1/cameras", { params: { query: filter } })).items,
    refetchInterval: 15_000,
  });

/** Shared explorer folders of the tenant plus the servers the caller may manage them on (LV-13). */
export const cameraFoldersQuery = queryOptions({
  queryKey: ["camera-folders"],
  queryFn: async () => unwrap(await api.GET("/api/v1/camera-folders")),
  refetchInterval: 30_000,
});

export const cameraFrigateConfigQuery = (cameraId: string) =>
  queryOptions({
    queryKey: ["cameras", cameraId, "frigate-config"],
    queryFn: async () =>
      unwrap(
        await api.GET("/api/v1/cameras/{cameraId}/config", {
          params: { path: { cameraId } },
        }),
      ),
  });

/** Full effective Frigate config of a camera (schema-driven editor). */
export const cameraFrigateDocQuery = (cameraId: string) =>
  queryOptions({
    queryKey: ["cameras", cameraId, "frigate-doc"],
    queryFn: async () => unwrap(await api.GET("/api/v1/cameras/{cameraId}/frigate-config", { params: { path: { cameraId } } })),
  });

export const frigateSchemaQuery = (serverId: string | undefined) =>
  queryOptions({
    queryKey: ["servers", serverId, "frigate-schema"],
    enabled: !!serverId,
    staleTime: 5 * 60_000,
    queryFn: async () =>
      unwrap(await api.GET("/api/v1/servers/{serverId}/frigate-config/schema", { params: { path: { serverId: serverId as string } } })),
  });

export const frigateRevisionsQuery = (serverId: string | undefined, cameraId: string, includeYaml: boolean) =>
  queryOptions({
    queryKey: ["servers", serverId, "frigate-revisions", cameraId, includeYaml],
    enabled: !!serverId,
    queryFn: async () =>
      unwrap(
        await api.GET("/api/v1/servers/{serverId}/frigate-config/revisions", {
          params: { path: { serverId: serverId as string }, query: { camera_id: cameraId, limit: 50, include_yaml: includeYaml } },
        }),
      ).items,
  });

export type EventFilter = {
  site_id?: string[];
  server_id?: string[];
  camera_id?: string[];
  camera_group_id?: string[];
  label?: string[];
  zone?: string[];
  sub_label?: string[];
  severity?: "alert" | "detection";
  plate?: string;
  from?: string;
  to?: string;
  reviewed?: boolean;
  has_snapshot?: boolean;
  has_preview?: boolean;
  limit?: number;
};

export const eventsQuery = (filter: EventFilter) =>
  infiniteQueryOptions({
    queryKey: ["events", filter],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) =>
      unwrap(await api.GET("/api/v1/events", { params: { query: { ...filter, cursor: pageParam } } })),
    getNextPageParam: (last) => last.next_cursor,
    refetchInterval: 15_000,
  });

export type PlateFilter = {
  plate?: string;
  exact?: boolean;
  site_id?: string[];
  camera_id?: string[];
  camera_group_id?: string[];
  from?: string;
  to?: string;
};

export const platesQuery = (filter: PlateFilter) =>
  infiniteQueryOptions({
    queryKey: ["plates", filter],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) =>
      unwrap(await api.GET("/api/v1/lpr/reads", { params: { query: { ...filter, cursor: pageParam } } })),
    getNextPageParam: (last) => last.next_cursor,
  });

export const syncStatusQuery = queryOptions({
  queryKey: ["events", "sync-status"],
  queryFn: async () => unwrap(await api.GET("/api/v1/events/sync-status")).items,
  refetchInterval: 15_000,
});

export const viewsQuery = queryOptions({
  queryKey: ["views"],
  queryFn: async () => unwrap(await api.GET("/api/v1/views")).items,
});

export const exportsQuery = queryOptions({
  queryKey: ["exports"],
  queryFn: async () => unwrap(await api.GET("/api/v1/exports")).items,
  refetchInterval: (q) => (q.state.data?.some((e) => e.status === "running" || e.status === "pending") ? 3_000 : 30_000),
});

export const recordingsQuery = (cameraId: string, from: string, to: string) =>
  queryOptions({
    queryKey: ["recordings", cameraId, from, to],
    queryFn: async () =>
      unwrap(await api.GET("/api/v1/cameras/{cameraId}/recordings", { params: { path: { cameraId }, query: { from, to } } })).items,
    enabled: !!cameraId,
  });

export const usersQuery = queryOptions({
  queryKey: ["users"],
  queryFn: async () => unwrap(await api.GET("/api/v1/users")).items,
});

export const groupsQuery = queryOptions({
  queryKey: ["groups"],
  queryFn: async () => unwrap(await api.GET("/api/v1/groups")).items,
});

export const permissionCatalogQuery = queryOptions({
  queryKey: ["permissions"],
  queryFn: async () => unwrap(await api.GET("/api/v1/permissions")).items,
  staleTime: Infinity,
});

export const grantsQuery = (subject_type?: "user" | "group", subject_id?: string) =>
  queryOptions({
    queryKey: ["grants", subject_type, subject_id],
    queryFn: async () => unwrap(await api.GET("/api/v1/grants", { params: { query: { subject_type, subject_id } } })).items,
    enabled: !!subject_id,
  });

export const cameraGroupsQuery = queryOptions({
  queryKey: ["camera-groups"],
  queryFn: async () => unwrap(await api.GET("/api/v1/camera-groups")).items,
});

export const brandingQuery = (tenantId: string) =>
  queryOptions({
    queryKey: ["branding", tenantId],
    queryFn: async () => unwrap(await api.GET("/api/v1/tenants/{tenantId}/branding", { params: { path: { tenantId } } })),
    enabled: !!tenantId,
  });

export type AuditFilter = { action?: string; actor_id?: string; from?: string; to?: string };

export const auditQuery = (filter: AuditFilter) =>
  infiniteQueryOptions({
    queryKey: ["audit", filter],
    initialPageParam: undefined as number | undefined,
    queryFn: async ({ pageParam }) =>
      unwrap(await api.GET("/api/v1/audit", { params: { query: { ...filter, before_id: pageParam, limit: 100 } } })).items,
    getNextPageParam: (last) => (last.length === 100 ? last[last.length - 1]?.id : undefined),
  });

export type AlarmFilter = {
  status?: "open" | "acknowledged" | "resolved";
  site_id?: string;
  camera_id?: string;
  assigned_to?: string;
  limit?: number;
};

export const alarmsQuery = (filter: AlarmFilter = {}) =>
  queryOptions({
    queryKey: ["alarms", filter],
    queryFn: async () => unwrap(await api.GET("/api/v1/alarms", { params: { query: filter } })).items,
    refetchInterval: 15_000,
  });

export const alarmAssigneesQuery = (alarmId?: string) =>
  queryOptions({
    queryKey: ["alarms", alarmId, "assignees"],
    queryFn: async () => {
      if (!alarmId) return [];
      return unwrap(await api.GET("/api/v1/alarms/{alarmId}/assignees", { params: { path: { alarmId } } })).items;
    },
    enabled: !!alarmId,
  });

export const searchQuery = (q: string) =>
  queryOptions({
    queryKey: ["search", q],
    queryFn: async () => unwrap(await api.GET("/api/v1/search", { params: { query: { q } } })),
    enabled: q.trim().length >= 2,
    staleTime: 5_000,
  });


export const rulesQuery = queryOptions({
  queryKey: ["rules"],
  queryFn: async () => unwrap(await api.GET("/api/v1/rules")).items,
});

export const channelsQuery = queryOptions({
  queryKey: ["channels"],
  queryFn: async () => unwrap(await api.GET("/api/v1/notification-channels")).items,
});

export const deliveriesQuery = (limit = 20) =>
  queryOptions({
    queryKey: ["deliveries", limit],
    queryFn: async () => unwrap(await api.GET("/api/v1/notification-deliveries", { params: { query: { limit } } })).items,
    refetchInterval: 15_000,
  });

export const whatsappSessionQuery = (channelId: string) =>
  queryOptions({
    queryKey: ["whatsapp-session", channelId],
    queryFn: async () => unwrap(await api.GET("/api/v1/notification-channels/{channelId}/whatsapp/session", { params: { path: { channelId } } })),
    refetchInterval: 3_000,
  });

export const whatsappQrQuery = (channelId: string, enabled: boolean) =>
  queryOptions({
    queryKey: ["whatsapp-qr", channelId],
    queryFn: async () => unwrap(await api.GET("/api/v1/notification-channels/{channelId}/whatsapp/qr", { params: { path: { channelId } } })),
    enabled,
    refetchInterval: 15_000,
    retry: false,
  });

export type NotificationFilter = { unread_only?: boolean; limit?: number };

export const notificationsQuery = (filter: NotificationFilter = {}) =>
  queryOptions({
    queryKey: ["notifications", filter],
    queryFn: async () => unwrap(await api.GET("/api/v1/notifications", { params: { query: filter } })),
  });
