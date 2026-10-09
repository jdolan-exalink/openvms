import { act, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/client";
import { RoomPlannerModal } from "./RoomPlannerModal";

const SVG = '<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>';
const PNG = "data:image/png;base64,iVBORw0KGgo=";

function savePlan(pngDataUrl = PNG) {
  act(() => {
    window.dispatchEvent(
      new MessageEvent("message", { data: { type: "OPENVMS_SAVE_PLAN", svg: SVG, pngDataUrl, state: {} } }),
    );
  });
}

async function bytesOf(file: File): Promise<number[]> {
  return Array.from(new Uint8Array(await file.arrayBuffer()));
}

describe("RoomPlannerModal data URL decoding", () => {
  it("decodes a base64 data URL into the file bytes", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(<RoomPlannerModal mapName="Floor" onSave={onSave} onClose={() => {}} />);

    savePlan();

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    const file = onSave.mock.calls[0]?.[0] as File;
    expect(file.type).toBe("image/png");
    expect(await bytesOf(file)).toEqual([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  });

  it("decodes a percent-encoded data URL instead of treating it as base64", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(<RoomPlannerModal mapName="Floor" onSave={onSave} onClose={() => {}} />);

    savePlan("data:image/svg+xml;charset=utf-8,%3Csvg%3E%C3%B1%3C%2Fsvg%3E");

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    const file = onSave.mock.calls[0]?.[0] as File;
    expect(file.type).toBe("image/svg+xml");
    expect(await bytesOf(file)).toEqual(Array.from(new TextEncoder().encode("<svg>ñ</svg>")));
  });
});

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
