import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { alarmAssigneesQuery } from "@/api/queries";

type Action = "acknowledge" | "assign" | "investigate" | "resolve" | "close" | "comments";
const labels: Record<Action, string> = {
  acknowledge: "Acknowledge", assign: "Assign", investigate: "Investigate", resolve: "Resolve", close: "Close", comments: "Add comment",
};

export function AlarmPanel({ alarms, canManage }: { alarms: Schemas["Alarm"][]; canManage: boolean }) {
  const [selected, setSelected] = useState<string>();
  const alarm = alarms.find(item => item.id === selected);
  return <section aria-label="Alarms" className="rounded border border-line bg-surface p-3">
    <h2>Alarms</h2>
    {!alarms.length && <p>No alarms in this site</p>}
    <ul>{alarms.map(item => <li key={item.id}><button onClick={() => setSelected(item.id)}>
      {item.camera_name} — {item.status}
    </button></li>)}</ul>
    {alarm && <AlarmDetail key={alarm.id} alarm={alarm} canManage={canManage} />}
  </section>;
}

function AlarmDetail({ alarm, canManage }: { alarm: Schemas["Alarm"]; canManage: boolean }) {
  const client = useQueryClient();
  const [comment, setComment] = useState("");
  const [assignee, setAssignee] = useState("");
  const history = useQuery({ queryKey: ["alarms", alarm.id, "transitions"],
    queryFn: async () => unwrap(await api.GET("/api/v1/alarms/{alarmId}/transitions", { params: { path: { alarmId: alarm.id } } })),
  });
  const assignees = useQuery({ ...alarmAssigneesQuery(alarm.id), enabled: canManage });
  const action = useMutation({
    mutationFn: async (next: Action) => {
      if (!canManage) throw new Error("Alarm management permission required");
      const params = { path: { alarmId: alarm.id } };
      switch (next) {
        case "acknowledge": return unwrap(await api.POST("/api/v1/alarms/{alarmId}/acknowledge", { params }));
        case "resolve": return unwrap(await api.POST("/api/v1/alarms/{alarmId}/resolve", { params }));
        case "assign": return unwrap(await api.POST("/api/v1/alarms/{alarmId}/assign", { params, body: { user_id: assignee } }));
        case "investigate": return unwrap(await api.POST("/api/v1/alarms/{alarmId}/investigate", { params, body: { comment } }));
        case "close": return unwrap(await api.POST("/api/v1/alarms/{alarmId}/close", { params, body: { comment } }));
        case "comments": return unwrap(await api.POST("/api/v1/alarms/{alarmId}/comments", { params, body: { comment } }));
      }
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["alarms"] });
      void client.invalidateQueries({ queryKey: ["maps"] });
    },
  });
  const legal = (next: Action) => {
    if (next === "comments") return !!comment.trim();
    if (alarm.status === "closed") return false;
    if (alarm.status === "resolved" && !["close", "investigate"].includes(next)) return false;
    return next !== "assign" || !!assignee;
  };
  return <div>
    <h3>History</h3>
    {history.isError && <p role="alert">{history.error.message}</p>}
    <ul>{history.data?.map(item => <li key={item.id}>{item.at} {item.actor_name} {item.to_status} {item.comment}</li>)}</ul>
    {canManage && <div>
      <label>Comment<textarea aria-label="Comment" value={comment} onChange={event => setComment(event.target.value)} /></label>
      <label>Assignee<select aria-label="Assignee" value={assignee} onChange={event => setAssignee(event.target.value)}>
        <option value="">Select assignee</option>
        {assignees.data?.map(item => <option key={item.id} value={item.id}>{item.display_name || item.username}</option>)}
      </select></label>
      {assignees.isError && <p role="alert">{assignees.error.message}</p>}
      {(Object.keys(labels) as Action[]).map(next => <button key={next} disabled={action.isPending || !legal(next)}
        onClick={() => action.mutate(next)}>{labels[next]}</button>)}
      {action.isError && <p role="alert">{action.error.message}</p>}
      {action.isSuccess && <p role="status">Action completed</p>}
    </div>}
  </div>;
}
