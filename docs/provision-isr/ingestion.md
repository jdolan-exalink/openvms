# Provision-ISR: from the camera to the plate search (stage 2)

What happens to a plate reading after the camera pushes it to the edge agent, how to see whether it
arrived, and what to do when it did not. Plan: `odd/tasks/provision-isr-ingest.md`. Camera and agent
setup: `setup.md`.

## The path

```text
camera --POST--> agent receiver --> durable queue (provision-events.jsonl)
                                          ^                    |
              GET events/after/{seq}      |                    | stored in lpr_reads
central worker (every 15 s) --------------+--> POST events/ack  v
```

1. The receiver answers the camera 200 at once, as before.
2. A reading from a **registered camera** (identified by the address it connects from, see `setup.md`)
   is appended to the queue on the agent's disk before it reaches anything else. A reading from an
   address that is not registered is accepted by the receiver but **not queued**: central could not
   attribute it to a camera.
3. Central's worker asks each agent for the events after the last sequence it stored
   (`GET /v1/provision-lpr/events/after/{seq}`, agent token), stores them in `lpr_reads` and moves its
   cursor in the same database transaction, then tells the agent (`POST /v1/provision-lpr/events/ack`
   `{"through": N}`) to drop what is stored.
4. Readings appear in the plate search next to the ones Frigate produced.

## Guarantees

| Situation | What happens |
|---|---|
| Agent restarts | The queue is on disk (`<state dir>/provision-events.jsonl`, 0600). Sequences are never reused. A half-written last line is discarded. |
| Central down or unreachable | The agent keeps the readings, up to 10,000 or 32 MiB. Past that the **oldest** are dropped and counted (`lost`). |
| Ack lost | Central already stored the page. The next pull finds nothing new, sees the agent still holds events, and repeats the ack. |
| Same reading stored twice | Impossible: the key is `(server, "agent:" + the ID the agent gave the reading)`, inserted with `ON CONFLICT DO NOTHING`. |
| Central restores an old backup (cursor goes back) | Readings are read again and ignored by the same key. |
| Agent state wiped | The queue gets a new `epoch`. Central sees it change (or the sequence go backwards), restarts its cursor at 0 and stores what is there. |
| Reading for a camera central does not know, a camera of another server, or with no plate left after normalising | Counted as **discarded** and acknowledged; it never blocks the readings behind it. |
| Disk full / write error on the agent | The reading is logged as lost for central; the camera already got its 200 and the live fan-out is unchanged. The file is rewritten before the next append so the failure cannot corrupt it. |

Images are **not** queued or stored: a reading carries plate, time, confidence and camera. They are
stored with `crop_attempts` already used up so the Frigate crop copier does not look for them.

## See it

- **Camera configuration screen**: card "Lecturas de patentes de las cámaras": last pull, readings
  pending in the agent, stored, discarded, and a warning when readings were lost or the last pull failed.
- API: `GET /api/v1/servers/{serverId}/provision-lpr/ingestion` (needs `servers.manage`).
- Agent: `GET /v1/provision-lpr/status` includes `events` (`epoch`, `depth`, `last_seq`, `lost`).
- Database: `SELECT * FROM server_agent_event_cursors;` and
  `SELECT count(*) FROM lpr_reads WHERE remote_event_id LIKE 'agent:%';`

## When it does not arrive

| You see | Likely cause |
|---|---|
| "update the agent" in the card | The agent predates the queue (it answers 404). Update it. |
| `agent returned 401` | The agent token central holds is not the agent's. Reinstall or re-register the agent. |
| Pending in the agent keeps growing | Central cannot reach the agent, or the worker is not running. |
| Discarded keeps growing | The camera is not a camera of that server in OpenVMS, or the reading has no plate. Check the camera is registered with its OpenVMS ID. |
| Nothing pending, nothing stored, camera online | The reading is not being queued: the camera is not registered, or the parser does not recognise the payload (look at captures, `capture.md`). |
| "lost" above 0 | The agent was cut off from central longer than its queue holds. The readings are gone. |

## Not done here

- Alarms, rules and realtime announcements for these readings. Central's alarm path starts from Frigate
  review items; readings from the camera are stored and searchable but do not open alarms yet.
- Correlation with Frigate's own reading of the same vehicle (a camera with both would show it twice).
- Images, vehicle attributes and list status.
- Anything about the camera's real LPR payload: the parser is still the unverified heuristic until a
  real capture exists (`real-camera-run.md`).

## Undo it

Stop the worker job by rolling the worker back, or leave it: it only reads. To drop the data, delete the
rows `lpr_reads WHERE remote_event_id LIKE 'agent:%'` and `server_agent_event_cursors`. The migration
(`00073`) only adds a table; its down step drops it.
