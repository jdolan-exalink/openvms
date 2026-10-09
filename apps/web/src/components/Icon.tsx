import type { LucideIcon } from "lucide-react";

const SIZES = { xs: 16, sm: 20, md: 24, lg: 32 } as const;

export type IconSize = keyof typeof SIZES | number;

type Props = {
  icon: LucideIcon;
  /** 16 / 20 / 24 / 32 scale, or an explicit pixel size. Defaults to "sm" (20). */
  size?: IconSize;
  /** Defaults to 1.75 (1.5 for "lg"); pass 2 for active navigation. */
  strokeWidth?: number;
  className?: string;
  /** Accessible name. Omit for decorative icons, which are hidden from assistive tech. */
  label?: string;
};

/** The single entry point for icons: lucide, currentColor, one size scale, one stroke weight. */
export function Icon({ icon: Glyph, size = "sm", strokeWidth, className, label }: Props) {
  const px = typeof size === "number" ? size : SIZES[size];
  const stroke = strokeWidth ?? (size === "lg" ? 1.5 : 1.75);
  return (
    <Glyph
      size={px}
      strokeWidth={stroke}
      className={className}
      {...(label ? { role: "img", "aria-label": label } : { "aria-hidden": true })}
    />
  );
}
