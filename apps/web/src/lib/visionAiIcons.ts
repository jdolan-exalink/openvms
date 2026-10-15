import { createElement, type ReactElement } from "react";
import {
  Bell, Bike, Bus, Car, Eye, Fence, LayoutGrid, RectangleHorizontal, ScanFace, Siren, Truck, User, Users,
  type LucideIcon,
} from "lucide-react";

/** Detected object key -> icon. Unknown objects fall back to the generic eye. */
const OBJECT_ICONS: Record<string, LucideIcon> = {
  person: User,
  face: ScanFace,
  car: Car,
  motorcycle: Bike,
  bicycle: Bike,
  truck: Truck,
  bus: Bus,
  license_plate: RectangleHorizontal,
  alarm: Siren,
  alert: Siren,
};

/** Vision AI preset key -> icon. */
const PRESET_ICONS: Record<string, LucideIcon> = {
  todo: LayoutGrid,
  personas: Users,
  vehiculos: Car,
  lpr: RectangleHorizontal,
  perimetro: Fence,
  alarmas: Bell,
};

export function objectIcon(key: string | null | undefined): LucideIcon {
  return OBJECT_ICONS[key ?? ""] ?? Eye;
}

export function presetIcon(key: string): LucideIcon {
  return PRESET_ICONS[key] ?? Eye;
}

interface IconProps {
  className?: string;
}

/** Decorative object icon (hidden from assistive tech; the text label stays the accessible name). */
export function ObjectIcon({ object, className }: IconProps & { object?: string | null }): ReactElement {
  return createElement(objectIcon(object), { "aria-hidden": true, className });
}

/** Decorative preset icon. */
export function PresetIcon({ preset, className }: IconProps & { preset: string }): ReactElement {
  return createElement(presetIcon(preset), { "aria-hidden": true, className });
}
