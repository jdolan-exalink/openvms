import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { BodyClassifySwitch } from "./BodyClassifySwitch";

function renderSwitch(props: Partial<React.ComponentProps<typeof BodyClassifySwitch>> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <BodyClassifySwitch scope="camera" id="cam-1" enabled {...props} />
    </QueryClientProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("BodyClassifySwitch", () => {
  it("is the shared button switch with its accessible name and checked state", () => {
    renderSwitch({ enabled: true });
    const control = screen.getByRole("switch", { name: "Clasificación fina" });
    expect(control.tagName).toBe("BUTTON");
    expect(control).toHaveAttribute("aria-checked", "true");
    expect(control).toBeChecked();
  });

  it("uses a custom label and can be disabled", () => {
    renderSwitch({ enabled: false, label: "Apagada en el servidor", disabled: true });
    const control = screen.getByRole("switch", { name: "Apagada en el servidor" });
    expect(control).not.toBeChecked();
    expect(control).toBeDisabled();
  });

  it("PUTs the toggled value for the camera", async () => {
    const fetchSpy = vi.fn(async () => new Response(JSON.stringify({ enabled: false }), { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetchSpy);
    renderSwitch({ enabled: true });
    fireEvent.click(screen.getByRole("switch", { name: "Clasificación fina" }));
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    const request = (fetchSpy.mock.calls[0] as unknown as [Request])[0];
    expect(request.method).toBe("PUT");
    expect(request.url).toContain("/api/v1/cameras/cam-1/body-classify");
    expect(await request.clone().json()).toEqual({ enabled: false });
  });
});
