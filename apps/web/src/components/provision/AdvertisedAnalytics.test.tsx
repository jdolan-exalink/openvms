import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { AdvertisedAnalytics } from "@/components/provision/AdvertisedAnalytics";

const cap = (id: Schemas["CameraAnalyticsCapability"]["id"], topics: string[]): Schemas["CameraAnalyticsCapability"] => ({
  id, source: "onvif_events", status: "advertised", topics,
});

describe("AdvertisedAnalytics", () => {
  it("lists what the camera advertises, says it is not proven, and keeps the topics as evidence", () => {
    render(<AdvertisedAnalytics analytics={[cap("line_crossing", ["RuleEngine/LineDetector/Crossed"]), cap("motion", ["VideoSource/MotionAlarm", "RuleEngine/CellMotionDetector/Motion"])]} />);

    expect(screen.getByText("Cruce de línea")).toBeInTheDocument();
    expect(screen.getByText("Movimiento")).toBeInTheDocument();
    expect(screen.getByText(/anunciada.*no está probada|no está probada/i)).toBeInTheDocument();
    expect(screen.getByText("Cruce de línea").closest("li")).toHaveAttribute("title", "RuleEngine/LineDetector/Crossed");
    expect(screen.getByText("Movimiento").closest("li")).toHaveAttribute("title", "VideoSource/MotionAlarm\nRuleEngine/CellMotionDetector/Motion");
  });

  it("explains that nothing was advertised, and that LPR over HTTP POST is not visible over ONVIF", () => {
    render(<AdvertisedAnalytics analytics={[]} />);
    expect(screen.getByText(/no anunció/i)).toBeInTheDocument();
    expect(screen.getByText(/HTTP POST/)).toBeInTheDocument();
  });

  it("treats a camera that was never probed like one that advertised nothing", () => {
    render(<AdvertisedAnalytics analytics={undefined} />);
    expect(screen.getByText(/no anunció/i)).toBeInTheDocument();
  });

  it("shows an id it does not know by its own name instead of dropping it", () => {
    render(<AdvertisedAnalytics analytics={[{ id: "fire" as never, source: "onvif_events", status: "advertised", topics: ["Vendor/Fire"] }]} />);
    expect(screen.getByText("fire")).toBeInTheDocument();
  });
});
