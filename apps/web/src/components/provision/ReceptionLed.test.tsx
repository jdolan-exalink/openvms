import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ReceptionLed } from "./ReceptionLed";

function show(reception: Parameters<typeof ReceptionLed>[0]["reception"]) {
  return render(<ReceptionLed reception={reception} />);
}

describe("ReceptionLed", () => {
  it("says in text that analytics are arriving, with the time of the last one", () => {
    show({ state: "receiving", last_event_at: "2026-11-05T10:00:00Z" });
    const led = screen.getByRole("status");
    expect(led).toHaveTextContent("Analíticas: recibiendo");
    expect(led).toHaveAttribute("data-state", "receiving");
    expect(led.getAttribute("title")).toMatch(/Último evento/);
  });

  it("says nothing arrived, and when nothing ever did", () => {
    show({ state: "idle", last_event_at: null });
    const led = screen.getByRole("status");
    expect(led).toHaveTextContent("Analíticas: sin actividad");
    expect(led.getAttribute("title")).toMatch(/Sin eventos recibidos/);
  });

  it("says the link to the agent is down", () => {
    show({ state: "down" });
    expect(screen.getByRole("status")).toHaveTextContent("Analíticas: sin conexión");
  });

  it("renders nothing for a camera Provision has nothing to do with", () => {
    const { container } = show(undefined);
    expect(container).toBeEmptyDOMElement();
  });
});
