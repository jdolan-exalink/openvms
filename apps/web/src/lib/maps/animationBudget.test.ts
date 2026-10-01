import { describe, expect, it } from "vitest";
import {
  AnimationBudget,
  MAX_CONCURRENT_PULSES,
  MAX_CONCURRENT_RIPPLES,
  RIPPLE_DURATION_MS,
} from "./animationBudget";

describe("AnimationBudget", () => {
  it("caps concurrent ripples at 30, dropping lowest priority first", () => {
    const budget = new AnimationBudget();

    // Add 30 detection ripples (priority 1)
    for (let i = 0; i < MAX_CONCURRENT_RIPPLES; i++) {
      const ok = budget.addRipple({
        id: `det-${i}`,
        lng: 0,
        lat: 0,
        type: "detection",
        startTime: 1000,
      });
      expect(ok).toBe(true);
    }
    expect(budget.getActiveCounts().ripples).toBe(30);

    // Adding another detection ripple (priority 1) drops an existing lowest priority ripple
    const okDet = budget.addRipple({
      id: "det-new",
      lng: 0,
      lat: 0,
      type: "detection",
      startTime: 1000,
    });
    expect(okDet).toBe(true);
    expect(budget.getActiveCounts().ripples).toBe(30);

    // Adding an alarm ripple (priority 3) easily evicts a detection ripple
    const okAlarm = budget.addRipple({
      id: "alarm-1",
      lng: 0,
      lat: 0,
      type: "alarm",
      startTime: 1000,
    });
    expect(okAlarm).toBe(true);
    expect(budget.getActiveCounts().ripples).toBe(30);

    const geo = budget.tick(1500);
    const alarmFeature = geo.features.find((f) => f.id === "alarm-1");
    expect(alarmFeature).toBeDefined();
    expect(alarmFeature?.properties?.color).toBe("#ef4444");
  });

  it("expires ripples after duration (2500ms)", () => {
    const budget = new AnimationBudget();
    budget.addRipple({
      id: "r1",
      lng: -58.38,
      lat: -34.6,
      type: "detection",
      startTime: 1000,
      duration: RIPPLE_DURATION_MS,
    });

    // At 2000ms (1000ms elapsed): still active
    const geo1 = budget.tick(2000);
    expect(geo1.features).toHaveLength(1);
    expect(geo1.features[0]?.properties?.radius).toBeGreaterThan(4);
    expect(geo1.features[0]?.properties?.opacity).toBeLessThan(0.8);

    // At 3501ms (2501ms elapsed): expired
    const geo2 = budget.tick(3501);
    expect(geo2.features).toHaveLength(0);
    expect(budget.getActiveCounts().ripples).toBe(0);
  });

  it("caps alarm pulses at 50, dropping oldest", () => {
    const budget = new AnimationBudget();

    for (let i = 0; i < MAX_CONCURRENT_PULSES; i++) {
      budget.setAlarmPulse({
        cameraId: `cam-${i}`,
        lng: 0,
        lat: 0,
        addedAt: 1000 + i,
      });
    }
    expect(budget.getActiveCounts().pulses).toBe(50);

    // Add 51st pulse
    budget.setAlarmPulse({
      cameraId: "cam-51",
      lng: 0,
      lat: 0,
      addedAt: 2000,
    });
    expect(budget.getActiveCounts().pulses).toBe(50);

    // cam-0 was the oldest and should have been dropped
    const geo = budget.tick(2500);
    expect(geo.features.find((f) => f.id === "pulse-cam-0")).toBeUndefined();
    expect(geo.features.find((f) => f.id === "pulse-cam-51")).toBeDefined();
  });

  it("renders static ring when pulse is acknowledged", () => {
    const budget = new AnimationBudget();
    budget.setAlarmPulse({
      cameraId: "cam-ack",
      lng: -58.38,
      lat: -34.6,
      acknowledged: true,
    });

    const geo1 = budget.tick(1000);
    const geo2 = budget.tick(1600);

    // Radius & opacity remain static
    expect(geo1.features[0]?.properties?.radius).toBe(16);
    expect(geo1.features[0]?.properties?.opacity).toBe(0.5);
    expect(geo2.features[0]?.properties?.radius).toBe(16);
    expect(geo2.features[0]?.properties?.opacity).toBe(0.5);
  });

  it("suppresses ripple animation and keeps pulses static when isReducedMotion is true", () => {
    const budget = new AnimationBudget();
    budget.addRipple({
      id: "r-motion",
      lng: 0,
      lat: 0,
      type: "detection",
      startTime: 1000,
    });
    budget.setAlarmPulse({
      cameraId: "cam-motion",
      lng: 0,
      lat: 0,
      acknowledged: false,
    });

    const geo = budget.tick(1500, true);
    // Ripples omitted
    expect(geo.features.find((f) => f.properties?.fxType === "ripple")).toBeUndefined();
    // Pulse rendered as static ring
    const pulseFeat = geo.features.find((f) => f.id === "pulse-cam-motion");
    expect(pulseFeat).toBeDefined();
    expect(pulseFeat?.properties?.radius).toBe(16);
    expect(pulseFeat?.properties?.opacity).toBe(0.5);
  });
});
