import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage, stubApi } from "@/test-utils";
import { ServerFrigateTab } from "./ServerFrigateTab";

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const server = { id: "a", tenant_id: "t", site_id: "s1", name: "frigate-h01" } as unknown as Schemas["Server"];
const yaml = "mqtt:\n  enabled: false\nrecord:\n  enabled: true\n  retain:\n    days: 21\n    mode: all\nsnapshots:\n  enabled: true\n  retain:\n    default: 9\n";

function stub() {
  const fetchMock = vi.fn(stubApi({ "/api/v1/servers/a/frigate-config/raw": () => json({ yaml }) }));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}
const rawCalls = (m: ReturnType<typeof stub>) => m.mock.calls.filter(([r]) => new URL((r as Request).url).pathname.endsWith("/frigate-config/raw"));

describe("ServerFrigateTab", () => {
  it("shows the recording summary read from the raw config when the user may see secrets", async () => {
    stub();
    renderPage(() => <ServerFrigateTab server={server} canReadConfig onOpenEditor={() => {}} />);
    expect(await screen.findByText("21 días")).toBeInTheDocument();
    expect(screen.getByText("all")).toBeInTheDocument();
    expect(screen.getByText("9 días")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Configuración de grabación" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Configuración de Frigate" })).toBeInTheDocument();
  });

  it("explains the missing permission and never requests the raw config", async () => {
    const m = stub();
    renderPage(() => <ServerFrigateTab server={server} canReadConfig={false} onOpenEditor={() => {}} />);
    expect(await screen.findByRole("note")).toHaveTextContent(/servers\.config\.secrets/);
    expect(screen.queryByText(/días/)).not.toBeInTheDocument();
    await waitFor(() => expect(rawCalls(m)).toHaveLength(0));
  });

  it("opens the editor from its button", async () => {
    stub();
    const onOpenEditor = vi.fn();
    renderPage(() => <ServerFrigateTab server={server} canReadConfig={false} onOpenEditor={onOpenEditor} />);
    fireEvent.click(await screen.findByRole("button", { name: "Abrir editor de configuración de Frigate" }));
    expect(onOpenEditor).toHaveBeenCalledTimes(1);
  });
});
