import { act, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/client";
import { RoomPlannerModal } from "./RoomPlannerModal";

const SVG = '<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>';
const PNG = "data:image/png;base64,iVBORw0KGgo=";

function savePlan() {
  act(() => {
    window.dispatchEvent(
      new MessageEvent("message", { data: { type: "OPENVMS_SAVE_PLAN", svg: SVG, pngDataUrl: PNG, state: {} } }),
    );
  });
}

describe("RoomPlannerModal save fallback", () => {
  it("shows the server error instead of a retry error after a conflict", async () => {
    const onSave = vi.fn().mockRejectedValue(new ApiError(412, "precondition_failed", "The plan changed on the server."));
    render(<RoomPlannerModal mapName="Floor" onSave={onSave} onClose={() => {}} />);

    savePlan();

    expect(await screen.findByText("The plan changed on the server.")).toBeInTheDocument();
  });

  it("retries with SVG when the PNG cannot be converted", async () => {
    const onSave = vi.fn().mockRejectedValueOnce(new Error("Invalid PNG header.")).mockResolvedValueOnce(undefined);
    render(<RoomPlannerModal mapName="Floor" onSave={onSave} onClose={() => {}} />);

    savePlan();

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(2));
    expect((onSave.mock.calls[0]?.[0] as File).type).toBe("image/png");
    expect((onSave.mock.calls[1]?.[0] as File).type).toBe("image/svg+xml");
  });

  it.each([
    ["a revision conflict", new ApiError(412, "precondition_failed", "The plan changed on the server.")],
    ["a permission error", new ApiError(403, "forbidden", "Not allowed.")],
    ["a network failure", new TypeError("Failed to fetch")],
    ["an aborted upload", new DOMException("Aborted", "AbortError")],
  ])("does not retry in another format after %s", async (_label, failure) => {
    const onSave = vi.fn().mockRejectedValue(failure);
    render(<RoomPlannerModal mapName="Floor" onSave={onSave} onClose={() => {}} />);

    savePlan();

    await waitFor(() => expect(screen.queryByText("Guardando plano…")).not.toBeInTheDocument());
    expect(onSave).toHaveBeenCalledTimes(1);
  });
});
