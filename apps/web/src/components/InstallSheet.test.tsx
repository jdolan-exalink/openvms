import { act, fireEvent, render, renderHook, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { InstallSheet } from "./InstallSheet";
import { INSTALL_DISMISSED_KEY, useInstallSheet } from "@/lib/pwa/useInstallSheet";

function stubEnv({ ios = false, standalone = false, small = true }: { ios?: boolean; standalone?: boolean; small?: boolean }) {
  vi.stubGlobal("navigator", { userAgent: ios ? "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)" : "Mozilla/5.0 (X11; Linux) Chrome/120", platform: "", maxTouchPoints: 0 });
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: query === "(display-mode: standalone)" ? standalone : query === "(max-width: 767px)" ? small : false,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
}

function fireInstallable(): Event & { prompt: ReturnType<typeof vi.fn> } {
  const event = new Event("beforeinstallprompt", { cancelable: true }) as Event & { prompt: ReturnType<typeof vi.fn>; userChoice: Promise<{ outcome: string }> };
  event.prompt = vi.fn().mockResolvedValue(undefined);
  event.userChoice = Promise.resolve({ outcome: "accepted" });
  act(() => {
    window.dispatchEvent(event);
  });
  return event;
}

beforeEach(() => localStorage.clear());
afterEach(() => {
  act(() => {
    window.dispatchEvent(new Event("appinstalled"));
  });
  vi.unstubAllGlobals();
});

describe("InstallSheet", () => {
  it("is an accessible modal dialog with the three benefits and focus inside", () => {
    stubEnv({});
    fireInstallable();
    render(<InstallSheet open onClose={() => {}} />);
    const dialog = screen.getByRole("dialog", { name: "Instalar OpenVMS" });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveTextContent("Pantalla completa, sin la barra del navegador");
    expect(dialog).toHaveTextContent("Un ícono en tu pantalla de inicio");
    expect(dialog).toHaveTextContent("La barra del sistema sigue tu tema");
    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(screen.queryByText(/Agregar a inicio/)).not.toBeInTheDocument();
  });

  it("renders nothing when closed", () => {
    stubEnv({});
    render(<InstallSheet open={false} onClose={() => {}} />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("closes on Escape and remembers the dismissal", () => {
    stubEnv({});
    const onClose = vi.fn();
    render(<InstallSheet open onClose={onClose} />);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem(INSTALL_DISMISSED_KEY)).toBe("1");
  });

  it("'Ahora no' closes and persists the dismissal", () => {
    stubEnv({});
    fireInstallable();
    const onClose = vi.fn();
    render(<InstallSheet open onClose={onClose} />);
    fireEvent.click(screen.getByRole("button", { name: "Ahora no" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem(INSTALL_DISMISSED_KEY)).toBe("1");
  });

  it("'Instalar' triggers the browser prompt", async () => {
    stubEnv({});
    const event = fireInstallable();
    const onClose = vi.fn();
    render(<InstallSheet open onClose={onClose} />);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Instalar" }));
    });
    expect(event.prompt).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalled();
  });

  it("on iOS shows the Share hint and hides the Instalar button", () => {
    stubEnv({ ios: true });
    render(<InstallSheet open onClose={() => {}} />);
    expect(screen.getByText(/En Safari, toca Compartir y luego «Agregar a inicio»/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Instalar" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Ahora no" })).toBeInTheDocument();
  });

  it("does not show the iOS hint when already installed", () => {
    stubEnv({ ios: true, standalone: true });
    render(<InstallSheet open onClose={() => {}} />);
    expect(screen.queryByText(/Agregar a inicio/)).not.toBeInTheDocument();
  });
});

describe("useInstallSheet auto-show", () => {
  it("opens once for an installable device on a small screen, then stays quiet after dismissal", () => {
    stubEnv({});
    const first = renderHook(() => useInstallSheet());
    expect(first.result.current.open).toBe(false);
    fireInstallable();
    expect(first.result.current.open).toBe(true);

    act(() => first.result.current.dismiss());
    expect(first.result.current.open).toBe(false);
    expect(localStorage.getItem(INSTALL_DISMISSED_KEY)).toBe("1");

    const second = renderHook(() => useInstallSheet());
    expect(second.result.current.open).toBe(false);
  });

  it("auto-opens on iOS Safari when not standalone, but not on large screens or standalone", () => {
    stubEnv({ ios: true });
    expect(renderHook(() => useInstallSheet()).result.current.open).toBe(true);
    localStorage.clear();
    stubEnv({ ios: true, small: false });
    expect(renderHook(() => useInstallSheet()).result.current.open).toBe(false);
    stubEnv({ ios: true, standalone: true });
    expect(renderHook(() => useInstallSheet()).result.current.open).toBe(false);
  });

  it("can be opened manually after a dismissal", () => {
    stubEnv({ ios: true });
    localStorage.setItem(INSTALL_DISMISSED_KEY, "1");
    const { result } = renderHook(() => useInstallSheet());
    expect(result.current.open).toBe(false);
    act(() => result.current.show());
    expect(result.current.open).toBe(true);
  });
});
