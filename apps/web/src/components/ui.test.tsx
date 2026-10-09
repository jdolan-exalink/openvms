import { fireEvent, render, screen } from "@testing-library/react";
import { Check } from "lucide-react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { getLocale, translate } from "@/i18n";
import { Button, Card, Chip, Field, IconButton, Select, StatusBadge, Switch, TextInput } from "./ui";

describe("Button", () => {
  it.each(["primary", "secondary", "filled", "tonal", "outlined", "text", "danger"] as const)("renders variant %s and fires onClick", (variant) => {
    const onClick = vi.fn();
    render(
      <Button variant={variant} onClick={onClick}>
        Go
      </Button>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Go" }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("defaults to type=button, accepts type override and merges className", () => {
    const { rerender } = render(<Button className="extra">A</Button>);
    expect(screen.getByRole("button", { name: "A" })).toHaveAttribute("type", "button");
    expect(screen.getByRole("button", { name: "A" })).toHaveClass("extra");
    rerender(<Button type="submit">A</Button>);
    expect(screen.getByRole("button", { name: "A" })).toHaveAttribute("type", "submit");
  });

  it("blocks clicks when disabled and supports the sm size", () => {
    const onClick = vi.fn();
    render(
      <Button disabled size="sm" onClick={onClick}>
        Off
      </Button>,
    );
    const b = screen.getByRole("button", { name: "Off" });
    expect(b).toBeDisabled();
    fireEvent.click(b);
    expect(onClick).not.toHaveBeenCalled();
  });
});

describe("IconButton", () => {
  it("exposes its aria-label and fires onClick", () => {
    const onClick = vi.fn();
    render(<IconButton icon={Check} aria-label="Confirm" onClick={onClick} />);
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it.each(["standard", "tonal", "filled"] as const)("renders variant %s", (variant) => {
    render(<IconButton icon={Check} aria-label="X" variant={variant} />);
    expect(screen.getByRole("button", { name: "X" })).toBeInTheDocument();
  });

  it("requires aria-label at the type level", () => {
    // @ts-expect-error aria-label is mandatory
    const el = <IconButton icon={Check} />;
    expect(el).toBeTruthy();
  });
});

describe("Chip", () => {
  it("reflects selected via aria-pressed and calls onChange with the next value", () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <Chip selected={false} onChange={onChange}>
        Cams
      </Chip>,
    );
    const chip = screen.getByRole("button", { name: "Cams" });
    expect(chip).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(chip);
    expect(onChange).toHaveBeenCalledWith(true);
    rerender(
      <Chip selected onChange={onChange}>
        Cams
      </Chip>,
    );
    expect(screen.getByRole("button", { name: "Cams" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "Cams" }));
    expect(onChange).toHaveBeenLastCalledWith(false);
  });

  it("blocks clicks when disabled", () => {
    const onChange = vi.fn();
    render(
      <Chip selected={false} disabled onChange={onChange}>
        Cams
      </Chip>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Cams" }));
    expect(onChange).not.toHaveBeenCalled();
  });
});

describe("Switch", () => {
  function Harness({ disabled }: { disabled?: boolean }) {
    const [on, setOn] = useState(false);
    return <Switch checked={on} onChange={setOn} label="Notify" disabled={disabled} />;
  }

  it("is a role=switch with a label that toggles aria-checked on click", () => {
    render(<Harness />);
    const sw = screen.getByRole("switch", { name: "Notify" });
    expect(sw).toHaveAttribute("aria-checked", "false");
    fireEvent.click(sw);
    expect(sw).toHaveAttribute("aria-checked", "true");
    fireEvent.click(sw);
    expect(sw).toHaveAttribute("aria-checked", "false");
  });

  it("is a native button so Space/Enter activate it (click is emitted by the browser)", () => {
    render(<Harness />);
    const sw = screen.getByRole("switch", { name: "Notify" });
    expect(sw.tagName).toBe("BUTTON");
    expect(sw).toHaveAttribute("type", "button");
  });

  it("blocks toggling when disabled", () => {
    render(<Harness disabled />);
    const sw = screen.getByRole("switch", { name: "Notify" });
    expect(sw).toBeDisabled();
    fireEvent.click(sw);
    expect(sw).toHaveAttribute("aria-checked", "false");
  });
});

describe("StatusBadge", () => {
  it("renders the translated label with a tone for known and unknown statuses", () => {
    const loc = getLocale();
    const { rerender } = render(<StatusBadge status="online" />);
    expect(screen.getByText(translate(loc, "status.online"))).toHaveAttribute("data-tone", "ok");
    rerender(<StatusBadge status="degraded" />);
    expect(screen.getByText(translate(loc, "status.degraded"))).toHaveAttribute("data-tone", "warn");
    rerender(<StatusBadge status="offline" />);
    expect(screen.getByText(translate(loc, "status.offline"))).toHaveAttribute("data-tone", "bad");
    rerender(<StatusBadge status="whatever" />);
    expect(screen.getByText(translate(loc, "status.unknown"))).toHaveAttribute("data-tone", "neutral");
  });
});

describe("Card", () => {
  it("renders children, and a heading when a title is given", () => {
    render(
      <Card title="Cameras" variant="outlined">
        <p>body</p>
      </Card>,
    );
    expect(screen.getByRole("heading", { name: "Cameras" })).toBeInTheDocument();
    expect(screen.getByText("body")).toBeInTheDocument();
  });

  it("renders without a heading when no title", () => {
    render(<Card>only</Card>);
    expect(screen.queryByRole("heading")).toBeNull();
    expect(screen.getByText("only")).toBeInTheDocument();
  });
});

describe("inputs", () => {
  it("TextInput and Select pass props through", () => {
    render(
      <>
        <TextInput aria-label="name" defaultValue="a" />
        <Select aria-label="kind" defaultValue="x">
          <option value="x">X</option>
        </Select>
      </>,
    );
    expect(screen.getByRole("textbox", { name: "name" })).toHaveValue("a");
    expect(screen.getByRole("combobox", { name: "kind" })).toHaveValue("x");
  });
});

describe("Field", () => {
  it("shows the hint, or the error in an alert instead of it", () => {
    const { rerender } = render(
      <Field label="Name" hint="help">
        <TextInput />
      </Field>,
    );
    expect(screen.getByText("help")).toBeInTheDocument();
    rerender(
      <Field label="Name" hint="help" error="Required">
        <TextInput />
      </Field>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Required");
    expect(screen.queryByText("help")).toBeNull();
  });
});
