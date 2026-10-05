import { fireEvent, render, screen } from "@testing-library/react";
import { Trash2 } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import { Checkbox, Empty, IconButton, LinkButton, Pill, RemovableChip, StatusBadge, Textarea } from "./ui";

describe("Pill", () => {
  it("renders the tone, an optional dot and its label", () => {
    const { container } = render(
      <Pill tone="ok" dot>
        Ready
      </Pill>,
    );
    expect(screen.getByText("Ready")).toHaveAttribute("data-tone", "ok");
    expect(container.querySelector("[aria-hidden]")).not.toBeNull();
  });

  it("defaults to neutral without a dot", () => {
    const { container } = render(<Pill>Plain</Pill>);
    expect(screen.getByText("Plain")).toHaveAttribute("data-tone", "neutral");
    expect(container.querySelector("[aria-hidden]")).toBeNull();
  });
});

describe("StatusBadge overrides", () => {
  it("uses the tone and label overrides for states beyond online/degraded/offline", () => {
    render(<StatusBadge status="ready" tone="ok" label="Lista" />);
    expect(screen.getByText("Lista")).toHaveAttribute("data-tone", "ok");
  });
});

describe("Checkbox", () => {
  it("is a role=checkbox named by its label and reports the next value", () => {
    const onChange = vi.fn();
    render(<Checkbox checked={false} onChange={onChange} label="Notify me" />);
    const box = screen.getByRole("checkbox", { name: "Notify me" });
    fireEvent.click(box);
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it("describes the control and honors disabled", () => {
    const onChange = vi.fn();
    render(<Checkbox checked onChange={onChange} label="Alerts" description="Sends an email" disabled />);
    const box = screen.getByRole("checkbox", { name: "Alerts" });
    expect(box).toBeDisabled();
    expect(box).toBeChecked();
    expect(box).toHaveAccessibleDescription("Sends an email");
  });
});

describe("Textarea", () => {
  it("forwards props and merges className", () => {
    const onChange = vi.fn();
    render(<Textarea aria-label="Notes" aria-invalid className="font-mono" onChange={onChange} />);
    const area = screen.getByRole("textbox", { name: "Notes" });
    expect(area).toHaveClass("font-mono");
    expect(area).toHaveAttribute("aria-invalid", "true");
    fireEvent.change(area, { target: { value: "x" } });
    expect(onChange).toHaveBeenCalledTimes(1);
  });
});

describe("RemovableChip", () => {
  it("exposes a labelled remove button", () => {
    const onRemove = vi.fn();
    render(<RemovableChip label="Plate ABC" removeLabel="Remove Plate ABC" onRemove={onRemove} />);
    expect(screen.getByText("Plate ABC")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove Plate ABC" }));
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  it("renders no remove button without onRemove", () => {
    render(<RemovableChip label="Fixed" removeLabel="Remove Fixed" />);
    expect(screen.queryByRole("button")).toBeNull();
  });
});

describe("LinkButton", () => {
  it("renders an anchor for href", () => {
    render(
      <LinkButton href="/download" variant="filled" size="sm">
        Download
      </LinkButton>,
    );
    const link = screen.getByRole("link", { name: "Download" });
    expect(link).toHaveAttribute("href", "/download");
    expect(link).toHaveClass("rounded-full");
  });
});

describe("LinkButton route typing", () => {
  it("rejects invalid routes, params and search at compile time (checked by pnpm typecheck)", () => {
    // Never rendered: a router link needs a RouterProvider, the assertions are the type errors.
    const compileOnly = () => (
      <>
        <LinkButton to="/cameras/$cameraId/frigate" params={{ cameraId: "c1" }}>
          Frigate
        </LinkButton>
        <LinkButton to="/permissions" search={{ subject: "user:1" }} variant="filled" size="sm" onClick={() => {}}>
          Ok
        </LinkButton>
        {/* @ts-expect-error unknown route */}
        <LinkButton to="/no-such-route">Nowhere</LinkButton>
        {/* @ts-expect-error missing required path param */}
        <LinkButton to="/cameras/$cameraId/frigate">NoParams</LinkButton>
        {/* @ts-expect-error params that do not belong to the route */}
        <LinkButton to="/cameras/$cameraId/frigate" params={{ wrong: "c1" }}>WrongParam</LinkButton>
      </>
    );
    expect(typeof compileOnly).toBe("function");
  });
});

describe("IconButton size sm", () => {
  it("keeps the aria-label and renders the compact size", () => {
    render(<IconButton icon={Trash2} size="sm" aria-label="Delete" />);
    expect(screen.getByRole("button", { name: "Delete" })).toHaveClass("size-9");
  });
});

describe("Empty", () => {
  it("renders its message with an optional icon", () => {
    const { container } = render(<Empty icon={Trash2}>Nothing here</Empty>);
    expect(screen.getByText("Nothing here")).toBeInTheDocument();
    expect(container.querySelector("svg")).not.toBeNull();
  });
});
