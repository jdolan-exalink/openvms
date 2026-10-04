import { Check, type LucideIcon } from "lucide-react";
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";
import { Icon } from "@/components/Icon";
import { useT, type MessageKey } from "@/i18n";
import { cn } from "@/lib/cn";

export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: ReactNode }) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="text-sm text-muted">{description}</p>}
      </div>
      {actions}
    </header>
  );
}

type Tone = "ok" | "warn" | "bad" | "info" | "neutral";
const toneStyles: Record<Tone, { dot: string; text: string }> = {
  ok: { dot: "bg-ok", text: "text-ok" },
  warn: { dot: "bg-warn", text: "text-warn" },
  bad: { dot: "bg-bad", text: "text-bad" },
  info: { dot: "bg-info", text: "text-info" },
  neutral: { dot: "bg-muted", text: "text-muted" },
};
type StatusStyle = { label: MessageKey; tone: Tone };
const unknownStatus: StatusStyle = { label: "status.unknown", tone: "neutral" };
const statusStyles: Record<string, StatusStyle> = {
  online: { label: "status.online", tone: "ok" },
  degraded: { label: "status.degraded", tone: "warn" },
  offline: { label: "status.offline", tone: "bad" },
};

/** Pill with a role-colored dot and label. Known statuses map to a tone; anything else is neutral. */
export function StatusBadge({ status }: { status: string }) {
  const t = useT();
  const s = statusStyles[status] ?? unknownStatus;
  const tone = toneStyles[s.tone];
  return (
    <span data-tone={s.tone} className={cn("inline-flex h-6 items-center gap-1.5 rounded-full bg-surface-2 px-2.5 text-xs font-medium whitespace-nowrap", tone.text)}>
      <span className={cn("size-2 rounded-full", tone.dot)} aria-hidden />
      {t(s.label)}
    </span>
  );
}

export function ErrorNote({ error }: { error: unknown }) {
  if (!error) return null;
  const message = error instanceof Error ? error.message : String(error);
  return (
    <p role="alert" className="rounded border border-bad/40 bg-bad/10 px-3 py-2 text-sm break-words text-bad">
      {message}
    </p>
  );
}

export type ButtonVariant = "filled" | "tonal" | "outlined" | "text" | "danger" | "primary" | "secondary";
/** "md" is 44px (use on touch layouts); "sm" is 36px, reserved for dense desktop tables. */
export type ButtonSize = "md" | "sm";

const buttonVariants: Record<ButtonVariant, string> = {
  filled: "bg-primary text-on-primary hover:brightness-110",
  tonal: "bg-secondary-container text-on-secondary-container hover:brightness-110",
  outlined: "border border-outline text-on-surface hover:bg-on-surface/8",
  text: "text-primary hover:bg-primary/10",
  danger: "bg-bad text-surface-dim light:text-white hover:brightness-110",
  primary: "bg-primary text-on-primary hover:brightness-110",
  secondary: "bg-secondary-container text-on-secondary-container hover:brightness-110",
};

const focusRing = "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary";

/** Pill button. Legacy "primary" renders as filled and "secondary" as tonal. */
export function Button({
  variant = "secondary",
  size = "md",
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: ButtonSize }) {
  return (
    <button
      type="button"
      {...props}
      className={cn(
        "m3-press inline-flex items-center justify-center gap-2 rounded-full text-sm font-bold",
        size === "sm" ? "h-9 px-4" : "h-11 px-5",
        focusRing,
        "disabled:cursor-not-allowed disabled:opacity-50",
        buttonVariants[variant],
        className,
      )}
    />
  );
}

export type IconButtonVariant = "standard" | "tonal" | "filled";
const iconButtonVariants: Record<IconButtonVariant, string> = {
  standard: "text-on-surface-variant hover:bg-on-surface/8",
  tonal: "bg-secondary-container text-on-secondary-container hover:brightness-110",
  filled: "bg-primary text-on-primary hover:brightness-110",
};

/** 44x44 round icon button. `aria-label` is required: there is no visible text. */
export function IconButton({
  icon,
  variant = "standard",
  className,
  ...props
}: Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children" | "aria-label"> & { icon: LucideIcon; "aria-label": string; variant?: IconButtonVariant }) {
  return (
    <button
      type="button"
      {...props}
      className={cn(
        "m3-press inline-flex size-11 shrink-0 items-center justify-center rounded-full",
        focusRing,
        "disabled:cursor-not-allowed disabled:opacity-50",
        iconButtonVariants[variant],
        className,
      )}
    >
      <Icon icon={icon} size="sm" />
    </button>
  );
}

/** Filter/choice chip. Selected shows a check and `aria-pressed`; onChange receives the next value. */
export function Chip({
  selected,
  onChange,
  children,
  className,
  ...props
}: Omit<ButtonHTMLAttributes<HTMLButtonElement>, "onChange" | "aria-pressed"> & { selected: boolean; onChange: (next: boolean) => void }) {
  return (
    <button
      type="button"
      {...props}
      aria-pressed={selected}
      onClick={(e) => {
        props.onClick?.(e);
        onChange(!selected);
      }}
      className={cn(
        "inline-flex h-9 items-center gap-1.5 rounded-m3-md border px-3 text-sm font-medium transition-colors duration-150",
        focusRing,
        "disabled:cursor-not-allowed disabled:opacity-50",
        selected ? "border-transparent bg-primary-container text-on-primary-container" : "border-outline-variant text-on-surface-variant hover:bg-on-surface/8",
        className,
      )}
    >
      {selected && <Icon icon={Check} size="xs" />}
      {children}
    </button>
  );
}

export type CardVariant = "filled" | "outlined" | "elevated";
const cardVariants: Record<CardVariant, string> = {
  filled: "bg-surface-1",
  outlined: "bg-surface-1 border border-outline-variant",
  elevated: "bg-surface-1 shadow-md",
};

/** Surface container. A `title` renders an h2 above the children. */
export function Card({ title, variant = "filled", className, children }: { title?: string; variant?: CardVariant; className?: string; children?: ReactNode }) {
  return (
    <section className={cn("flex flex-col gap-3 rounded-m3-xl p-5", cardVariants[variant], className)}>
      {title && <h2 className="text-base font-bold">{title}</h2>}
      {children}
    </section>
  );
}

/** Real <button role="switch">; Space/Enter toggle natively. The label is the accessible name. */
export function Switch({ checked, onChange, label, disabled, className }: { checked: boolean; onChange: (next: boolean) => void; label: string; disabled?: boolean; className?: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn("inline-flex min-h-11 items-center gap-3 rounded-full text-sm", focusRing, "disabled:cursor-not-allowed disabled:opacity-50", className)}
    >
      <span
        aria-hidden
        className={cn(
          "relative inline-flex h-8 w-[52px] shrink-0 items-center rounded-full border-2 transition-colors duration-150",
          checked ? "border-primary bg-primary" : "border-outline bg-surface-3",
        )}
      >
        <span
          className={cn(
            "absolute top-1/2 -translate-y-1/2 rounded-full transition-all duration-300",
            checked ? "left-[22px] size-6 bg-on-primary" : "left-1.5 size-4 bg-outline",
          )}
        />
      </span>
      <span>{label}</span>
    </button>
  );
}

const fieldClass =
  "h-12 w-full rounded-m3-md border border-transparent bg-surface-2 px-3 text-sm placeholder:text-muted/70 focus-visible:outline-2 focus-visible:outline-primary aria-[invalid=true]:border-bad";

export function Field({ label, hint, error, children }: { label: string; hint?: string; error?: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span className="font-medium">{label}</span>
      {children}
      {error ? (
        <span role="alert" className="text-xs text-bad">
          {error}
        </span>
      ) : (
        hint && <span className="text-xs text-muted">{hint}</span>
      )}
    </label>
  );
}

export function TextInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={cn(fieldClass, props.className)} />;
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select {...props} className={cn(fieldClass, props.className)} />;
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="rounded border border-dashed border-line px-4 py-8 text-center text-sm text-muted">{children}</p>;
}

/** Live-region line with inventory counts shown above a table. */
export function Summary({ children }: { children: ReactNode }) {
  return (
    <p role="status" className="text-xs text-muted tabular-nums">
      {children}
    </p>
  );
}

/** Table wrapper that scrolls sideways on narrow screens instead of the page. */
export function Table({ children, label }: { children: ReactNode; label: string }) {
  return (
    <div className="overflow-x-auto rounded border border-line bg-surface">
      <table aria-label={label} className="w-full text-left text-sm [&_td]:px-3 [&_td]:py-2 [&_th]:px-3 [&_th]:py-2">
        {children}
      </table>
    </div>
  );
}

export function Th({ children, className }: { children?: ReactNode; className?: string }) {
  return <th className={cn("border-b border-line font-mono text-[11px] font-normal tracking-wider text-muted uppercase", className)}>{children}</th>;
}
