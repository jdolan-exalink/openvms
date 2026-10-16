import { QueryClient, QueryClientProvider, useInfiniteQuery } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRealtimeFeed, subscribeFrames } from "./realtime";
import { alarmsInfiniteQuery } from "@/api/queries";

class FakeSocket {
  static instances: FakeSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onclose: ((e: { code: number; reason: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  sent: string[] = [];
  constructor(public url: string) {
    FakeSocket.instances.push(this);
  }
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.closed = true;
  }
  open() {
    this.onopen?.();
  }
  message(frame: unknown) {
    this.onmessage?.({ data: typeof frame === "string" ? frame : JSON.stringify(frame) });
  }
  drop(code: number, reason = "") {
    this.onclose?.({ code, reason });
  }
}

function sock(i: number) {
  const ws = FakeSocket.instances.at(i);
  if (!ws) throw new Error(`no socket at ${i}`);
  return ws;
}

function setup(enabled = true) {
  const client = new QueryClient();
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  const hook = renderHook(() => useRealtimeFeed({ enabled, random: () => 1 }), { wrapper });
  const keys = () => invalidate.mock.calls.map((c) => JSON.stringify((c[0] as { queryKey: unknown }).queryKey));
  return { ...hook, invalidate, keys };
}

beforeEach(() => {
  vi.useFakeTimers();
  FakeSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeSocket);
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("useRealtimeFeed", () => {

  it("refreshes priority alarm pages from fresh cursors on updates and reconnect", async () => {
    vi.useRealTimers();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const requests: URL[] = [];
    vi.stubGlobal("fetch", vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      requests.push(url);
      const cursor = url.searchParams.get("cursor");
      const nextCursor = cursor
        ? undefined
        : requests.length === 1
          ? "cursor-initial"
          : requests.length === 3
            ? "cursor-after-open"
            : requests.length === 5
              ? "cursor-after-create"
              : requests.length === 7
                ? "cursor-after-update"
              : "cursor-after-reconnect";
      const items = cursor
        ? [{ id: "page-two" }]
        : requests.length >= 5
          ? [{ id: "new-alarm", status: "open" }, { id: "page-one", status: requests.length >= 7 ? "acknowledged" : "open" }]
          : [{ id: "page-one", status: "open" }];
      return new Response(JSON.stringify({ items, next_cursor: nextCursor }), {
        headers: { "Content-Type": "application/json" },
      });
    }));

    const filter = { status_group: "active" as const, sort: "priority" as const, camera_id: "cam-1" };
    const { result } = renderHook(() => {
      useRealtimeFeed({ random: () => 1 });
      return useInfiniteQuery(alarmsInfiniteQuery(filter));
    }, { wrapper });

    await waitFor(() => expect(result.current.data?.pages).toHaveLength(1));
    await act(async () => { await result.current.fetchNextPage(); });
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(2));
    expect(requests.map((url) => url.searchParams.get("sort"))).toEqual(["priority", "priority"]);
    expect(requests.map((url) => url.searchParams.get("camera_id"))).toEqual(["cam-1", "cam-1"]);

    act(() => sock(0).open());
    await waitFor(() => expect(requests).toHaveLength(4));
    expect(requests[3]?.searchParams.get("cursor")).toBe("cursor-after-open");

    act(() => sock(0).message({ type: "alarm.created", data: { id: "new-alarm" } }));
    await waitFor(() => expect(requests).toHaveLength(6));
    expect(requests[4]?.searchParams.get("cursor")).toBeNull();
    expect(requests[5]?.searchParams.get("cursor")).toBe("cursor-after-create");
    await waitFor(() => expect(result.current.data?.pages.flatMap((page) => page.items.map((item) => item.id))).toEqual(["new-alarm", "page-one", "page-two"]));

    act(() => sock(0).message({ type: "alarm.updated", data: { id: "page-one", status: "acknowledged" } }));
    await waitFor(() => expect(requests).toHaveLength(8));
    expect(requests[6]?.searchParams.get("cursor")).toBeNull();
    expect(requests[7]?.searchParams.get("cursor")).toBe("cursor-after-update");
    await waitFor(() => expect(result.current.data?.pages[0]?.items[1]?.status).toBe("acknowledged"));
    const visibleIds = result.current.data?.pages.flatMap((page) => page.items.map((item) => item.id)) ?? [];
    expect(new Set(visibleIds).size).toBe(visibleIds.length);

    act(() => sock(0).drop(1006));
    await waitFor(() => expect(FakeSocket.instances).toHaveLength(2), { timeout: 3_000 });
    act(() => sock(1).open());
    await waitFor(() => expect(requests).toHaveLength(10));
    expect(requests[9]?.searchParams.get("cursor")).toBe("cursor-after-reconnect");
    expect(requests.every((url) => url.searchParams.get("sort") === "priority")).toBe(true);
    expect(requests.every((url) => url.searchParams.get("camera_id") === "cam-1")).toBe(true);
  });

  it("invalidates maps on reconnect and resync", () => {
    const { keys, invalidate } = setup();
    act(() => sock(0).open());
    expect(keys()).toContain(JSON.stringify(["maps"]));
    invalidate.mockClear();
    act(() => sock(0).message({ op: "resync" }));
    expect(keys()).toContain(JSON.stringify(["maps"]));
  });
  it("connects to same-origin /ws and reports the connection state", () => {
    const { result } = setup();
    expect(FakeSocket.instances).toHaveLength(1);
    expect(sock(0).url).toBe(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws`);
    expect(result.current.connected).toBe(false);
    act(() => sock(0).open());
    expect(result.current.connected).toBe(true);
  });

  it("does not connect when disabled", () => {
    setup(false);
    expect(FakeSocket.instances).toHaveLength(0);
  });

  it("invalidates event queries on event.created and server queries on server.status", () => {
    const { keys } = setup();
    const ws = sock(0);
    act(() => ws.open());
    const afterOpen = keys().length;
    act(() => ws.message({ type: "event.created", tenant_id: "t", data: {} }));
    expect(keys().slice(afterOpen)).toContain(JSON.stringify(["events"]));
    expect(keys().slice(afterOpen)).toContain(JSON.stringify(["vision-ai"]));
    const afterEvent = keys().length;
    act(() => ws.message({ type: "server.status", tenant_id: "t", data: {} }));
    expect(keys().slice(afterEvent)).toEqual(expect.arrayContaining([JSON.stringify(["servers"]), JSON.stringify(["health"])]));
  });

  it("ignores unknown types and malformed frames", () => {
    const { invalidate } = setup();
    const ws = sock(0);
    act(() => ws.open());
    invalidate.mockClear();
    act(() => {
      ws.message({ type: "something.else", data: {} });
      ws.message("not json");
    });
    expect(invalidate).not.toHaveBeenCalled();
  });

  it("reconnects with exponential backoff capped at 30s and catches up after each open", () => {
    const { result, invalidate } = setup();
    act(() => sock(0).drop(1006));
    expect(result.current.connected).toBe(false);
    act(() => vi.advanceTimersByTime(999));
    expect(FakeSocket.instances).toHaveLength(1);
    act(() => vi.advanceTimersByTime(1));
    expect(FakeSocket.instances).toHaveLength(2);
    act(() => sock(1).drop(1006)); // never opened: attempt grows
    act(() => vi.advanceTimersByTime(1999));
    expect(FakeSocket.instances).toHaveLength(2);
    act(() => vi.advanceTimersByTime(1));
    expect(FakeSocket.instances).toHaveLength(3);
    for (let i = 0; i < 8; i++) {
      act(() => sock(-1).drop(1013));
      act(() => vi.advanceTimersByTime(30_000));
    }
    const before = FakeSocket.instances.length;
    act(() => sock(-1).drop(1013));
    act(() => vi.advanceTimersByTime(29_999));
    expect(FakeSocket.instances).toHaveLength(before);
    act(() => vi.advanceTimersByTime(1));
    expect(FakeSocket.instances).toHaveLength(before + 1);
    invalidate.mockClear();
    act(() => sock(-1).open());
    expect(invalidate).toHaveBeenCalled();
  });

  it("resets the backoff after a stable connection", () => {
    setup();
    act(() => sock(0).drop(1006));
    act(() => vi.advanceTimersByTime(1000));
    act(() => sock(1).drop(1006));
    act(() => vi.advanceTimersByTime(2000));
    const ws = sock(2);
    act(() => ws.open());
    act(() => vi.advanceTimersByTime(10_000));
    act(() => ws.drop(1006));
    act(() => vi.advanceTimersByTime(1000));
    expect(FakeSocket.instances).toHaveLength(4);
  });

  it("does not reconnect after 1008 session ended and refreshes the session query instead", () => {
    const { invalidate, keys } = setup();
    const ws = sock(0);
    act(() => ws.open());
    invalidate.mockClear();
    act(() => ws.drop(1008, "session ended"));
    act(() => vi.advanceTimersByTime(120_000));
    expect(FakeSocket.instances).toHaveLength(1);
    expect(keys()).toContain(JSON.stringify(["me"]));
  });

  it("reconnects after 1008 slow consumer", () => {
    setup();
    act(() => sock(0).drop(1008, "slow consumer"));
    act(() => vi.advanceTimersByTime(1000));
    expect(FakeSocket.instances).toHaveLength(2);
  });

  it("closes the socket and cancels pending reconnects on unmount", () => {
    const { unmount } = setup();
    const ws = sock(0);
    act(() => ws.open());
    unmount();
    expect(ws.closed).toBe(true);
    act(() => ws.drop(1006));
    act(() => vi.advanceTimersByTime(60_000));
    expect(FakeSocket.instances).toHaveLength(1);
  });
});

describe("notification frames", () => {
  it("invalidates the notifications cache on notification.created", () => {
    const { keys } = setup();
    act(() => sock(0).open());
    act(() => sock(0).message({ type: "notification.created", data: {} }));
    expect(keys()).toContain(JSON.stringify(["notifications"]));
  });
});

describe("realtime protocol v2 & listener API", () => {
  it("sends hello frame on socket open", () => {
    setup();
    const ws = sock(0);
    act(() => ws.open());
    expect(ws.sent).toHaveLength(1);
    const hello = JSON.parse(ws.sent[0]!) as { op: string; v: number; topics: string[] };
    expect(hello.op).toBe("hello");
    expect(hello.v).toBe(2);
    expect(hello.topics).toEqual(expect.arrayContaining(["events", "cameras", "alarms"]));
  });

  it("dispatches raw frames to subscribeFrames listeners", () => {
    setup();
    const ws = sock(0);
    act(() => ws.open());

    const received: unknown[] = [];
    const unsub = subscribeFrames((frame) => {
      received.push(frame);
    });

    const testFrame = {
      v: 2,
      type: "camera.status_changed",
      camera_id: "c1",
      data: { from: "online", to: "offline" },
    };
    act(() => ws.message(testFrame));

    expect(received).toHaveLength(1);
    expect(received[0]).toEqual(testFrame);

    unsub();
    act(() => ws.message({ type: "other" }));
    expect(received).toHaveLength(1);
  });

  it("handles batch frames and dispatches each sub-frame", () => {
    const { keys } = setup();
    const ws = sock(0);
    act(() => ws.open());

    const received: unknown[] = [];
    const unsub = subscribeFrames((frame) => {
      received.push(frame);
    });

    const batch = {
      op: "batch",
      frames: [
        { type: "event.created", data: {} },
        { type: "camera.status_changed", camera_id: "c2", data: {} },
      ],
    };

    act(() => ws.message(batch));

    expect(received).toHaveLength(2);
    expect(keys()).toContain(JSON.stringify(["events"]));
    expect(keys()).toContain(JSON.stringify(["cameras"]));

    unsub();
  });

  it("handles resync frames and triggers catch-up invalidations", () => {
    const { keys, invalidate } = setup();
    const ws = sock(0);
    act(() => ws.open());
    invalidate.mockClear();

    const received: unknown[] = [];
    const unsub = subscribeFrames((frame) => {
      received.push(frame);
    });

    act(() => ws.message({ op: "resync", streams: ["events", "cameras"] }));

    expect(received).toHaveLength(1);
    expect(keys()).toContain(JSON.stringify(["events"]));
    expect(keys()).toContain(JSON.stringify(["cameras"]));

    unsub();
  });
});
