import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("Playback", () => {
  it("preserves an explicit search time and refreshes the clock every 30 seconds", async () => {
    vi.resetModules();
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2025-06-15T23:59:50"));
    const setIntervalSpy = vi.spyOn(globalThis, "setInterval");
    const clearIntervalSpy = vi.spyOn(globalThis, "clearInterval");
    const { routeTree } = await import("../router");
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": () =>
            json({
              id: "u1",
              username: "operator",
              display_name: "Operator",
              mfa_enabled: false,
              must_change_password: false,
              auth_method: "session",
              tenant_id: "t1",
              grants: [],
            }),
          "/api/v1/cameras": () => json({ items: [] }),
          "/api/v1/recordings": () => json({ items: [] }),
          "/api/v1/events": () => json({ items: [] }),
        }),
      ),
    );
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({
      routeTree,
      context: { queryClient },
      history: createMemoryHistory({ initialEntries: ["/playback?t=1749945600"] }),
    });
    const { unmount } = render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    screen.getByText("Grabaciones");
    const dateInput = document.querySelector<HTMLInputElement>('input[type="date"]');
    const instantInput = document.querySelector<HTMLInputElement>('input[type="datetime-local"]');
    expect(dateInput?.value).toBe("2025-06-15");
    expect(instantInput?.value).toBe("2025-06-15T00:00");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    await act(async () => {
      screen.getByRole("button", { name: "Día siguiente" }).click();
    });
    expect(dateInput?.value).toBe("2025-06-16");

    unmount();
    const clockIntervalIndex = setIntervalSpy.mock.calls.findIndex((call) => call[1] === 30_000);
    const clockInterval = setIntervalSpy.mock.results[clockIntervalIndex]?.value;
    expect(clockInterval).toBeDefined();
    expect(clearIntervalSpy).toHaveBeenCalledWith(clockInterval);
  });

  it("initializes the timeline to the current day without a render-time clock read", async () => {
    vi.resetModules();
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2025-06-15T12:00:00Z"));
    const { routeTree } = await import("../router");
    const mountedAt = new Date("2025-07-16T12:00:00Z");
    vi.setSystemTime(mountedAt);
    const expectedDate = `${mountedAt.getFullYear()}-${String(mountedAt.getMonth() + 1).padStart(2, "0")}-${String(mountedAt.getDate()).padStart(2, "0")}`;
    const initialInstant = new Date(mountedAt.getTime() - 600_000);
    const expectedInstant = `${expectedDate}T${String(initialInstant.getHours()).padStart(2, "0")}:${String(initialInstant.getMinutes()).padStart(2, "0")}`;
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": () =>
            json({
              id: "u1",
              username: "operator",
              display_name: "Operator",
              mfa_enabled: false,
              must_change_password: false,
              auth_method: "session",
              tenant_id: "t1",
              grants: [],
            }),
          "/api/v1/cameras": () => json({ items: [] }),
          "/api/v1/recordings": () => json({ items: [] }),
          "/api/v1/events": () => json({ items: [] }),
        }),
      ),
    );
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({
      routeTree,
      context: { queryClient },
      history: createMemoryHistory({ initialEntries: ["/playback"] }),
    });

    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await screen.findByText("Grabaciones");
    await waitFor(() => {
      expect(document.querySelector<HTMLInputElement>('input[type="date"]')?.value).toBe(expectedDate);
      expect(document.querySelector<HTMLInputElement>('input[type="datetime-local"]')?.value).toBe(expectedInstant);
    });
  });
});
