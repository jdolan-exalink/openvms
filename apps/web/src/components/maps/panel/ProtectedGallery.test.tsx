import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ProtectedGallery } from "./ProtectedGallery";

vi.mock("@/lib/protectedImages", async () => {
  const actual = await vi.importActual<typeof import("@/lib/protectedImages")>("@/lib/protectedImages");
  return {
    ...actual,
    listProtectedImages: vi.fn(async () => [
      {
        id: "plate:r1",
        kind: "plate" as const,
        title: "AB123CD",
        detail: "Cementerio",
        comment: "conservar",
        savedAt: "2024-01-01T10:00:00Z",
        blob: new Blob(["img"], { type: "image/jpeg" }),
      },
    ]),
  };
});

beforeEach(() => {
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:saved");
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
});

describe("ProtectedGallery", () => {
  it("opens a large view with the saved photo, the full frame and the clip", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <ProtectedGallery />
      </QueryClientProvider>,
    );

    fireEvent.click(await screen.findByRole("button", { name: /AB123CD/ }));

    const dialog = await screen.findByRole("dialog", { name: "Imagen protegida AB123CD" });
    expect(screen.getByRole("img", { name: "Calidad completa de AB123CD" })).toHaveAttribute("src", "/media/v1/lpr/reads/r1/snapshot.jpg");

    fireEvent.click(screen.getByRole("button", { name: "Guardada" }));
    expect(dialog.querySelector("img[alt='AB123CD']")).toHaveAttribute("src", "blob:saved");

    fireEvent.click(screen.getByRole("button", { name: "Clip" }));
    expect(dialog.querySelector("video")).toHaveAttribute("src", "/media/v1/lpr/reads/r1/clip.mp4");
    expect(screen.getByText("conservar")).toBeInTheDocument();
  });
});
