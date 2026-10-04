import { readMapPalette } from "@/components/maps/canvas/palette";
import type { FeatureCollection, Point } from "geojson";

export type RippleType = "detection" | "lpr" | "alarm";

export interface RippleItem {
  id: string;
  lng: number;
  lat: number;
  type: RippleType;
  color?: string;
  startTime?: number;
  duration?: number;
}

export interface PulseItem {
  cameraId: string;
  lng: number;
  lat: number;
  acknowledged?: boolean;
  color?: string;
  addedAt?: number;
}

interface InternalRipple {
  id: string;
  lng: number;
  lat: number;
  type: RippleType;
  priority: number;
  color: string;
  startTime: number;
  duration: number;
}

interface InternalPulse {
  cameraId: string;
  lng: number;
  lat: number;
  acknowledged: boolean;
  color: string;
  addedAt: number;
}

export const MAX_CONCURRENT_RIPPLES = 30;
export const MAX_CONCURRENT_PULSES = 50;
export const RIPPLE_DURATION_MS = 2500;

const RIPPLE_PRIORITY: Record<RippleType, number> = {
  detection: 1,
  lpr: 2,
  alarm: 3,
};

/** Default ripple colors follow the active theme through the canvas palette. */
function rippleDefaultColor(type: RippleType): string {
  const palette = readMapPalette();
  return type === "lpr" ? palette.primary : type === "alarm" ? palette.bad : palette.ok;
}

export class AnimationBudget {
  private ripples: InternalRipple[] = [];
  private pulses = new Map<string, InternalPulse>();

  addRipple(item: RippleItem): boolean {
    const now = item.startTime ?? Date.now();
    const duration = item.duration ?? RIPPLE_DURATION_MS;
    const priority = RIPPLE_PRIORITY[item.type] ?? 1;
    const color = item.color ?? rippleDefaultColor(item.type);

    const internal: InternalRipple = {
      id: item.id,
      lng: item.lng,
      lat: item.lat,
      type: item.type,
      priority,
      color,
      startTime: now,
      duration,
    };

    if (this.ripples.length >= MAX_CONCURRENT_RIPPLES) {
      // Find lowest priority
      let lowestIdx = 0;
      let lowestPriority = this.ripples[0]!.priority;
      for (let i = 1; i < this.ripples.length; i++) {
        if (this.ripples[i]!.priority < lowestPriority) {
          lowestPriority = this.ripples[i]!.priority;
          lowestIdx = i;
        }
      }

      if (priority < lowestPriority) {
        // New ripple has lower priority than all current ripples; drop it
        return false;
      }

      // Drop lowest priority ripple to make room
      this.ripples.splice(lowestIdx, 1);
    }

    this.ripples.push(internal);
    return true;
  }

  setAlarmPulse(pulse: PulseItem): void {
    if (this.pulses.size >= MAX_CONCURRENT_PULSES && !this.pulses.has(pulse.cameraId)) {
      // Drop oldest pulse
      let oldestId: string | null = null;
      let oldestTime = Infinity;
      for (const [id, p] of this.pulses.entries()) {
        if (p.addedAt < oldestTime) {
          oldestTime = p.addedAt;
          oldestId = id;
        }
      }
      if (oldestId) {
        this.pulses.delete(oldestId);
      }
    }

    this.pulses.set(pulse.cameraId, {
      cameraId: pulse.cameraId,
      lng: pulse.lng,
      lat: pulse.lat,
      acknowledged: pulse.acknowledged ?? false,
      color: pulse.color ?? readMapPalette().bad,
      addedAt: pulse.addedAt ?? Date.now(),
    });
  }

  removeAlarmPulse(cameraId: string): void {
    this.pulses.delete(cameraId);
  }

  clear(): void {
    this.ripples = [];
    this.pulses.clear();
  }

  hasActiveFx(): boolean {
    return this.ripples.length > 0 || this.pulses.size > 0;
  }

  getActiveCounts(): { ripples: number; pulses: number } {
    return {
      ripples: this.ripples.length,
      pulses: this.pulses.size,
    };
  }

  /**
   * Evaluates all active ripples and pulses at timestamp `now` and returns
   * a GeoJSON FeatureCollection ready to pass into the MapLibre `fx` source.
   */
  tick(now = Date.now(), isReducedMotion = false): FeatureCollection<Point> {
    // 1. Prune and evaluate ripples
    const activeRipples: InternalRipple[] = [];
    const features: FeatureCollection<Point>["features"] = [];

    for (const ripple of this.ripples) {
      const elapsed = now - ripple.startTime;
      if (elapsed >= ripple.duration) {
        continue; // Expired
      }
      activeRipples.push(ripple);

      if (!isReducedMotion) {
        const progress = Math.min(1, Math.max(0, elapsed / ripple.duration));
        const radius = 4 + progress * 32;
        const opacity = Math.max(0, (1 - progress) * 0.8);

        features.push({
          type: "Feature",
          id: ripple.id,
          geometry: {
            type: "Point",
            coordinates: [ripple.lng, ripple.lat],
          },
          properties: {
            id: ripple.id,
            fxType: "ripple",
            radius,
            opacity,
            color: ripple.color,
            strokeWidth: 2,
          },
        });
      }
    }
    this.ripples = activeRipples;

    // 2. Shared clock for pulses (1200ms cycle)
    const cycle = 1200;
    const t = (now % cycle) / cycle;
    const sine = Math.sin(t * Math.PI * 2);

    for (const pulse of this.pulses.values()) {
      let radius: number;
      let opacity: number;

      if (pulse.acknowledged || isReducedMotion) {
        // Static ring for acknowledged alarms or reduced motion
        radius = 16;
        opacity = 0.5;
      } else {
        // Pulsing ring
        radius = 14 + (sine + 1) * 5; // 14 to 24px
        opacity = 0.2 + (sine + 1) * 0.3; // 0.2 to 0.8
      }

      features.push({
        type: "Feature",
        id: `pulse-${pulse.cameraId}`,
        geometry: {
          type: "Point",
          coordinates: [pulse.lng, pulse.lat],
        },
        properties: {
          id: `pulse-${pulse.cameraId}`,
          fxType: "pulse",
          radius,
          opacity,
          color: pulse.color,
          strokeWidth: 2,
        },
      });
    }

    return {
      type: "FeatureCollection",
      features,
    };
  }
}

export function isPrefersReducedMotion(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}
