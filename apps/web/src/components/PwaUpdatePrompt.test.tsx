import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PwaUpdatePrompt } from "./PwaUpdatePrompt";

const state = vi.hoisted(() => ({
  needRefresh: false,
  setNeedRefresh: vi.fn(),
  updateServiceWorker: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("virtual:pwa-register/react", () => ({
  useRegisterSW: () => ({
    needRefresh: [state.needRefresh, state.setNeedRefresh],
    offlineReady: [false, () => {}],
    updateServiceWorker: state.updateServiceWorker,
  }),
}));

beforeEach(() => {
  state.needRefresh = false;
  state.setNeedRefresh.mockClear();
  state.updateServiceWorker.mockClear();
});

describe("PwaUpdatePrompt", () => {
  it("stays hidden until a new version is waiting", () => {
    render(<PwaUpdatePrompt />);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("announces the update and applies it only when the operator asks", () => {
    state.needRefresh = true;
    render(<PwaUpdatePrompt />);
    expect(screen.getByRole("status")).toHaveTextContent("Hay una nueva versión disponible");
    expect(state.updateServiceWorker).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Actualizar" }));
    expect(state.updateServiceWorker).toHaveBeenCalledWith(true);
  });

  it("can be dismissed", () => {
    state.needRefresh = true;
    render(<PwaUpdatePrompt />);
    fireEvent.click(screen.getByRole("button", { name: "Descartar" }));
    expect(state.setNeedRefresh).toHaveBeenCalledWith(false);
  });
});
