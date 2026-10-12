import { calculateClickToCenterAngles } from "./math";
import type { VirtualFisheyeView } from "./types";

export interface PtzState {
  yaw: number;
  pitch: number;
  roll: number;
  fov: number;
}

export type PtzChangeCallback = (state: PtzState) => void;

export class VirtualPtzController {
  private current: PtzState;
  private target: PtzState;
  private isAnimating = false;
  private animStartTime = 0;
  private animDuration = 250; // ms
  private animStart: PtzState | null = null;
  private onChange?: PtzChangeCallback;

  constructor(initial?: Partial<PtzState>, onChange?: PtzChangeCallback) {
    this.current = {
      yaw: initial?.yaw ?? 0,
      pitch: initial?.pitch ?? -30,
      roll: initial?.roll ?? 0,
      fov: initial?.fov ?? 90,
    };
    this.target = { ...this.current };
    this.onChange = onChange;
  }

  public getState(): PtzState {
    return { ...this.current };
  }

  public setState(state: Partial<PtzState>, animate = false): void {
    const nextTarget: PtzState = {
      yaw: this.normalizeYaw(state.yaw ?? this.target.yaw),
      pitch: this.clampPitch(state.pitch ?? this.target.pitch),
      roll: state.roll ?? this.target.roll,
      fov: this.clampFov(state.fov ?? this.target.fov),
    };

    if (!animate) {
      this.target = nextTarget;
      this.current = { ...nextTarget };
      this.isAnimating = false;
      this.onChange?.(this.getState());
      return;
    }

    this.target = nextTarget;
    this.animStart = { ...this.current };
    this.animStartTime = performance.now();
    this.isAnimating = true;
    this.stepAnimation();
  }

  public panBy(deltaX: number, deltaY: number, viewportWidth: number, viewportHeight: number): void {
    if (this.isAnimating) this.isAnimating = false;

    // Scale sensitivity by current field of view
    const fovRatio = this.current.fov / 90;
    const yawSens = (this.current.fov / viewportWidth) * 1.2;
    const pitchSens = ((this.current.fov * (viewportHeight / viewportWidth)) / viewportHeight) * 1.2;

    const newYaw = this.normalizeYaw(this.current.yaw - deltaX * yawSens * fovRatio);
    const newPitch = this.clampPitch(this.current.pitch + deltaY * pitchSens * fovRatio);

    this.current.yaw = newYaw;
    this.current.pitch = newPitch;
    this.target = { ...this.current };
    this.onChange?.(this.getState());
  }

  public zoomBy(deltaWheel: number): void {
    if (this.isAnimating) this.isAnimating = false;

    // deltaWheel < 0: zoom in (lower FOV), deltaWheel > 0: zoom out (higher FOV)
    const zoomFactor = deltaWheel > 0 ? 1.08 : 0.92;
    const newFov = this.clampFov(this.current.fov * zoomFactor);

    this.current.fov = Math.round(newFov * 10) / 10;
    this.target = { ...this.current };
    this.onChange?.(this.getState());
  }

  public clickToCenter(
    clientX: number,
    clientY: number,
    viewportWidth: number,
    viewportHeight: number,
  ): void {
    const u = (clientX / viewportWidth) * 2 - 1;
    const v = (clientY / viewportHeight) * 2 - 1;
    const aspect = viewportWidth / viewportHeight;

    const newAngles = calculateClickToCenterAngles(u, v, aspect, this.current);
    this.setState(newAngles, true);
  }

  public reset(defaultView?: Partial<VirtualFisheyeView>, animate = false): void {
    this.setState(
      {
        yaw: defaultView?.yaw ?? 0,
        pitch: defaultView?.pitch ?? -30,
        roll: defaultView?.roll ?? 0,
        fov: defaultView?.fov ?? 90,
      },
      animate,
    );
  }

  private stepAnimation = (): void => {
    if (!this.isAnimating || !this.animStart) return;

    const now = performance.now();
    const elapsed = now - this.animStartTime;
    const progress = Math.min(1, elapsed / this.animDuration);

    // Ease-out cubic
    const ease = 1 - Math.pow(1 - progress, 3);

    this.current.yaw = this.lerpAngle(this.animStart.yaw, this.target.yaw, ease);
    this.current.pitch = this.lerp(this.animStart.pitch, this.target.pitch, ease);
    this.current.fov = this.lerp(this.animStart.fov, this.target.fov, ease);
    this.current.roll = this.lerp(this.animStart.roll, this.target.roll, ease);

    this.onChange?.(this.getState());

    if (progress < 1) {
      requestAnimationFrame(this.stepAnimation);
    } else {
      this.isAnimating = false;
      this.animStart = null;
    }
  };

  private lerp(a: number, b: number, t: number): number {
    return a + (b - a) * t;
  }

  private lerpAngle(a: number, b: number, t: number): number {
    let diff = (b - a) % 360;
    if (diff > 180) diff -= 360;
    if (diff < -180) diff += 360;
    return this.normalizeYaw(a + diff * t);
  }

  private normalizeYaw(yaw: number): number {
    let y = yaw % 360;
    if (y > 180) y -= 360;
    if (y < -180) y += 360;
    return Math.round(y * 10) / 10;
  }

  private clampPitch(pitch: number): number {
    return Math.max(-85, Math.min(85, Math.round(pitch * 10) / 10));
  }

  private clampFov(fov: number): number {
    return Math.max(30, Math.min(130, Math.round(fov * 10) / 10));
  }
}
