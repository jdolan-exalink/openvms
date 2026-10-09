import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PublicEvidenceShare } from "./PublicEvidenceShare";

vi.mock("@tanstack/react-router", () => ({
  useParams: () => ({ token: "tok123" }),
}));
vi.mock("@/components/EvidencePlayerModal", () => ({
  EvidencePlayerModal: () => null,
}));

const info = {
  share_token: "tok123",
  has_password: true,
  job: {
    id: "j1",
    name: "Case",
    start_time: "2026-01-01T00:00:00Z",
    end_time: "2026-01-01T00:01:00Z",
    camera_count: 1,
    total_bytes: 10,
    items: [],
    download_url: "/media/v1/public/shares/tok123/download",
  },
};

afterEach(() => vi.restoreAllMocks());

describe("PublicEvidenceShare", () => {
  it("submits the password in a POST body, never in a URL", async () => {
    const fetchMock = vi
      .fn<(url: string, init?: RequestInit & { headers?: Record<string, string> }) => Promise<Response>>()
      .mockResolvedValueOnce(new Response("{}", { status: 401 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(info), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    render(
      <QueryClientProvider client={new QueryClient()}>
        <PublicEvidenceShare />
      </QueryClientProvider>,
    );

    const input = await screen.findByDisplayValue("");
    fireEvent.change(input, { target: { value: "s3cret" } });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    const [firstUrl, firstInit] = fetchMock.mock.calls[0] ?? [];
    expect(firstUrl).toBe("/media/v1/public/shares/tok123");
    expect(firstInit?.method).toBeUndefined();
    const [secondUrl, secondInit] = fetchMock.mock.calls[1] ?? [];
    expect(secondUrl).toBe("/media/v1/public/shares/tok123");
    expect(String(secondUrl)).not.toContain("password");
    expect(secondInit).toMatchObject({
      method: "POST",
      credentials: "same-origin",
      body: JSON.stringify({ password: "s3cret" }),
    });
    expect(secondInit?.headers?.["X-OpenVMS-Request"]).toBe("1");
  });
});
