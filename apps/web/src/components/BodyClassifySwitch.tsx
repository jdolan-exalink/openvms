import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";

/** BodyClassifySwitch turns the crop classifier on or off for one server or camera. */
export function BodyClassifySwitch({
  scope,
  id,
  enabled,
  disabled,
  label = "Clasificación fina",
}: {
  scope: "server" | "camera";
  id: string;
  enabled: boolean;
  disabled?: boolean;
  label?: string;
}) {
  const qc = useQueryClient();
  const save = useMutation({
    mutationFn: async (next: boolean) => {
      if (scope === "server") {
        return unwrap(await api.PUT("/api/v1/servers/{serverId}/body-classify", {
          params: { path: { serverId: id } },
          body: { enabled: next },
        }));
      }
      return unwrap(await api.PUT("/api/v1/cameras/{cameraId}/body-classify", {
        params: { path: { cameraId: id } },
        body: { enabled: next },
      }));
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["classify", "policy"] });
      await qc.invalidateQueries({ queryKey: ["system", "capacity"] });
    },
  });
  return (
    <label className="inline-flex items-center gap-2 text-xs">
      <input
        type="checkbox"
        role="switch"
        aria-label={label}
        checked={enabled}
        disabled={disabled || save.isPending}
        onChange={(e) => save.mutate(e.target.checked)}
      />
      {label}
    </label>
  );
}
