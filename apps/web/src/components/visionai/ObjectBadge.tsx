import { ShieldAlert } from "lucide-react";
import { cn } from "@/lib/cn";
import { ObjectIcon } from "@/lib/visionAiIcons";

/**
 * What was detected inside a video stream, as an icon only: words over the picture cover it. The
 * object, its confidence and whether it raised an alarm stay in the tooltip and in the accessible
 * name, so nothing is lost for a screen reader or a hover.
 */
export function ObjectBadge({
  object,
  confidence,
  alarm = false,
  className,
}: {
  object: string | null | undefined;
  /** 0..1 */
  confidence?: number;
  alarm?: boolean;
  className?: string;
}) {
  if (!object) return null;
  const label = [object, confidence !== undefined ? `${Math.round(confidence * 100)}%` : null, alarm ? "· alarma" : null].filter(Boolean).join(" ");
  return (
    <span
      data-testid="object-badge"
      data-alarm={alarm ? "true" : undefined}
      role="img"
      aria-label={label}
      title={label}
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full px-1.5 py-1 backdrop-blur shadow-sm",
        alarm ? "border border-bad/40 bg-bad/20 text-bad" : "border border-primary/30 bg-primary/20 text-primary",
        className,
      )}
    >
      <ObjectIcon object={object} className="size-3.5" />
      {alarm && <ShieldAlert aria-hidden="true" className="size-3.5" />}
    </span>
  );
}
