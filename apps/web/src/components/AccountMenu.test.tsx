import { act, fireEvent, render, screen } from "@testing-library/react";
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AccountMenu } from "./AccountMenu";

function stubEnv({ ios = false, standalone = false }: { ios?: boolean; standalone?: boolean }) {
  vi.stubGlobal("navigator", { userAgent: ios ? "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)" : "Mozilla/5.0 (X11; Linux) Chrome/120", platform: "", maxTouchPoints: 0 });
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: query === "(display-mode: standalone)" && standalone, media: query, addEventListener: () => {}, removeEventListener: () => {} }));
}

async function openMenu(onInstall?: () => void) {
  const root = createRootRoute({ component: () => <AccountMenu name="Ana" username="ana" onLogout={() => {}} onInstall={onInstall} /> });
  const router = createRouter({ routeTree: root, history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(<RouterProvider router={router} />);
  fireEvent.click(await screen.findByRole("button", { name: "Cuenta" }));
}

const installable = () => {
  const event = new Event("beforeinstallprompt", { cancelable: true }) as Event & { prompt: () => Promise<void>; userChoice: Promise<{ outcome: string }> };
  event.prompt = async () => {};
  event.userChoice = Promise.resolve({ outcome: "dismissed" });
  act(() => {
    window.dispatchEvent(event);
  });
};

afterEach(() => {
  act(() => {
    window.dispatchEvent(new Event("appinstalled"));
  });
  vi.unstubAllGlobals();
});

describe("AccountMenu install entry", () => {
  it("is hidden when the app cannot be installed", async () => {
    stubEnv({});
    await openMenu(() => {});
    expect(screen.queryByRole("menuitem", { name: "Instalar app" })).not.toBeInTheDocument();
  });

  it("shows when the browser offers installation and opens the sheet", async () => {
    stubEnv({});
    installable();
    const onInstall = vi.fn();
    await openMenu(onInstall);
    fireEvent.click(screen.getByRole("menuitem", { name: "Instalar app" }));
    expect(onInstall).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("shows on iOS Safari until installed, never in standalone", async () => {
    stubEnv({ ios: true });
    await openMenu(() => {});
    expect(screen.getByRole("menuitem", { name: "Instalar app" })).toBeInTheDocument();
  });

  it("is hidden in standalone iOS", async () => {
    stubEnv({ ios: true, standalone: true });
    await openMenu(() => {});
    expect(screen.queryByRole("menuitem", { name: "Instalar app" })).not.toBeInTheDocument();
  });
});
