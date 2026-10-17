import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ZoneList } from "./ZoneList";
import type { EditorItem } from "./zoneDraft";

const item = (over: Partial<EditorItem>): EditorItem => ({
  uid: "u1",
  kind: "zone",
  scope: "",
  name: "entrada",
  points: [],
  enabled: true,
  colorIndex: 0,
  managedId: "z1",
  ...over,
});

function renderList(items: EditorItem[], props: Partial<Parameters<typeof ZoneList>[0]> = {}) {
  return render(
    <ZoneList
      tab="zone"
      onTab={() => {}}
      items={items}
      selectedUid={null}
      hidden={new Set()}
      issues={{}}
      disabled={false}
      onSelect={() => {}}
      onToggleHidden={() => {}}
      onDelete={() => {}}
      onNew={() => {}}
      {...props}
    />,
  );
}

describe("ZoneList sync state", () => {
  it("shows the sync state of persisted zones and nothing for foreign or unsaved ones", () => {
    renderList(
      [item({ uid: "a", name: "a", managedId: "z1" }), item({ uid: "b", name: "b", managedId: "z2" }), item({ uid: "c", name: "c", managedId: "z3" }), item({ uid: "d", name: "d", managedId: "z4" }), item({ uid: "e", name: "foranea", managedId: undefined })],
      {
        syncStates: {
          z1: { status: "pending" },
          z2: { status: "synced" },
          z3: { status: "failed", error: "Frigate no responde" },
          z4: { status: "conflict", error: "difiere" },
        },
      },
    );
    expect(screen.getByText("Pendiente")).toBeInTheDocument();
    expect(screen.getByText("Sincronizada")).toBeInTheDocument();
    expect(screen.getByText("Error de sincronización")).toBeInTheDocument();
    expect(screen.getByText("Conflicto")).toBeInTheDocument();
    expect(screen.getByTitle("Frigate no responde")).toBeInTheDocument();
    expect(screen.getAllByText(/Pendiente|Sincronizada|Error de sincronización|Conflicto/)).toHaveLength(4);
  });

  it("offers a retry only for failed or conflicted zones and only when the caller may retry", () => {
    const onRetrySync = vi.fn();
    const items = [item({ uid: "a", name: "a", managedId: "z1" }), item({ uid: "b", name: "b", managedId: "z2" }), item({ uid: "c", name: "c", managedId: "z3" })];
    const syncStates = { z1: { status: "pending" as const }, z2: { status: "failed" as const }, z3: { status: "conflict" as const } };
    const { unmount } = renderList(items, { syncStates, onRetrySync });
    expect(screen.queryByRole("button", { name: "Reintentar sincronización de a" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Reintentar sincronización de b" }));
    expect(onRetrySync).toHaveBeenCalledWith("z2");
    expect(screen.getByRole("button", { name: "Reintentar sincronización de c" })).toBeInTheDocument();
    unmount();

    renderList(items, { syncStates });
    expect(screen.queryByRole("button", { name: /Reintentar sincronización/ })).not.toBeInTheDocument();
  });
});
