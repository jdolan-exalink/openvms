import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRealtimeFeed } from "./realtime";

class FakeSocket {
  static instances: FakeSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onclose: ((e: { code: number; reason: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  constructor(public url: string) {
    FakeSocket.instances.push(this);
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
    sock(0).open();
    sock(0).message({ type: "notification.created", data: {} });
    expect(keys()).toContain(JSON.stringify(["notifications"]));
  });
});
