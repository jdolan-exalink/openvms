import { cn } from "@/lib/cn";
import { isH265 } from "@/lib/cameraCodec";

const EXPLANATION =
  "Códec H.265: muchos navegadores no lo reproducen en vivo. Si la cámara lo permite, cambialo a H.264 en su configuración.";

/**
 * CodecBadge flags a stream that is H.265, which many browsers cannot play over MSE, so a tile
 * that looks broken says why at a glance. Other codecs (and an unknown one) draw nothing: only
 * the exception needs to stand out. Like the quality badge it does not follow the overlay auto-hide.
 */
export function CodecBadge({ codec, className }: { codec: string | null | undefined; className?: string }) {
  if (!isH265(codec)) return null;
  return (
    <span
      data-testid="codec-badge"
      role="img"
      aria-label={EXPLANATION}
      title={EXPLANATION}
      className={cn(
        "inline-flex shrink-0 items-center rounded bg-warn/25 px-1.5 py-0.5 font-mono text-[10px] font-semibold leading-none tracking-wide text-warn ring-1 ring-warn/50 backdrop-blur",
        className,
      )}
    >
      H.265
    </span>
  );
}
