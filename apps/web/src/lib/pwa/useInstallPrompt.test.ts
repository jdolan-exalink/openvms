import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useInstallPrompt } from "./useInstallPrompt";

function installEvent(outcome: "accepted" | "dismissed" = "accepted") {
  const event = new Event("beforeinstallprompt", { cancelable: true }) as Event & { prompt: ReturnType<typeof vi.fn>; userChoice: Promise<{ outcome: string }> };
  event.prompt = vi.fn().mockResolvedValue(undefined);
  event.userChoice = Promise.resolve({ outcome });
  return event;
}

function stubDisplayMode(standalone: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: standalone && query === "(display-mode: standalone)", media: query, addEventListener: () => {}, removeEventListener: () => {} }));
}

afterEach(() => {
  vi.unstubAllGlobals();
  Object.defineProperty(navigator, "standalone", { value: undefined, configurable: true });
});

describe("useInstallPrompt", () => {
  it("captures beforeinstallprompt, suppresses the mini-infobar and prompts on demand", async () => {
    stubDisplayMode(false);
    const { result } = renderHook(() => useInstallPrompt());
    expect(result.current.canInstall).toBe(false);

    const event = installEvent();
    act(() => {
      window.dispatchEvent(event);
    });
    expect(event.defaultPrevented).toBe(true);
    expect(result.current.canInstall).toBe(true);

    await act(async () => {
      await result.current.promptInstall();
    });
    expect(event.prompt).toHaveBeenCalledTimes(1);
    expect(result.current.canInstall).toBe(false);
  });

  it("stops offering install after appinstalled", () => {
    stubDisplayMode(false);
    const { result } = renderHook(() => useInstallPrompt());
    act(() => {
      window.dispatchEvent(installEvent());
    });
    act(() => {
      window.dispatchEvent(new Event("appinstalled"));
    });
    expect(result.current.canInstall).toBe(false);
  });

  it("detects standalone through display-mode and navigator.standalone", () => {
    stubDisplayMode(true);
    expect(renderHook(() => useInstallPrompt()).result.current.isStandalone).toBe(true);

    stubDisplayMode(false);
    expect(renderHook(() => useInstallPrompt()).result.current.isStandalone).toBe(false);
    Object.defineProperty(navigator, "standalone", { value: true, configurable: true });
    expect(renderHook(() => useInstallPrompt()).result.current.isStandalone).toBe(true);
  });

  it("detects iPhone, iPad and iPadOS desktop-class Safari", () => {
    stubDisplayMode(false);
    const ua = (value: string, touchPoints = 0) => {
      vi.stubGlobal("navigator", { userAgent: value, platform: value.includes("Macintosh") ? "MacIntel" : "", maxTouchPoints: touchPoints });
      return renderHook(() => useInstallPrompt()).result.current.isIOS;
    };
    expect(ua("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")).toBe(true);
    expect(ua("Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X)")).toBe(true);
    expect(ua("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", 5)).toBe(true);
    expect(ua("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", 0)).toBe(false);
    expect(ua("Mozilla/5.0 (X11; Linux x86_64) Chrome/120")).toBe(false);
  });
});
