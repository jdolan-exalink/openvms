import { Link, type LinkComponentProps, type RegisteredRouter } from "@tanstack/react-router";
import { Check, X, type LucideIcon } from "lucide-react";
import { useId, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";
import { Icon } from "@/components/Icon";
import { useT, type MessageKey } from "@/i18n";
import { cn } from "@/lib/cn";

export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: ReactNode }) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="text-[30px] leading-9 font-extrabold tracking-tight">{title}</h1>
        {description && <p className="text-sm text-on-surface-variant">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}

export type Tone = "ok" | "warn" | "bad" | "info" | "neutral";
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

/**
 * Pill with a role-colored dot and label. Known statuses map to a tone and label; anything else is neutral.
 * `tone` and `label` override the mapping so callers can express states beyond online/degraded/offline.
 */
export function StatusBadge({ status, tone: toneOverride, label }: { status: string; tone?: Tone; label?: string }) {
  const t = useT();
  const s = statusStyles[status] ?? unknownStatus;
  const toneName = toneOverride ?? s.tone;
  const tone = toneStyles[toneName];
  return (
    <span data-tone={toneName} className={cn("inline-flex h-6 items-center gap-1.5 rounded-full bg-surface-2 px-2.5 text-xs font-medium whitespace-nowrap", tone.text)}>
      <span className={cn("size-2 rounded-full", tone.dot)} aria-hidden />
      {label ?? t(s.label)}
    </span>
  );
}

export type PillTone = "neutral" | "primary" | "secondary" | "ok" | "warn" | "bad" | "info";
export type PillSize = "sm" | "md";
const pillTones: Record<PillTone, { box: string; dot: string }> = {
  neutral: { box: "bg-surface-2 text-on-surface-variant", dot: "bg-on-surface-variant" },
  primary: { box: "bg-primary-container text-on-primary-container", dot: "bg-on-primary-container" },
  secondary: { box: "bg-secondary-container text-on-secondary-container", dot: "bg-on-secondary-container" },
  ok: { box: "bg-ok/15 text-ok", dot: "bg-ok" },
  warn: { box: "bg-warn/15 text-warn", dot: "bg-warn" },
  bad: { box: "bg-bad/15 text-bad", dot: "bg-bad" },
  info: { box: "bg-info/15 text-info", dot: "bg-info" },
};

/** Small non-interactive label for states and categories. Use `StatusBadge` for online/degraded/offline. */
export function Pill({ tone = "neutral", size = "md", dot, className, children }: { tone?: PillTone; size?: PillSize; dot?: boolean; className?: string; children?: ReactNode }) {
  const style = pillTones[tone];
  return (
    <span data-tone={tone} className={cn("inline-flex items-center gap-1.5 rounded-full font-medium whitespace-nowrap", size === "sm" ? "h-5 px-2 text-[11px]" : "h-6 px-2.5 text-xs", style.box, className)}>
      {dot && <span className={cn("size-1.5 shrink-0 rounded-full", style.dot)} aria-hidden />}
      {children}
    </span>
  );
}

export function ErrorNote({ error }: { error: unknown }) {
  if (!error) return null;
  const message = error instanceof Error ? error.message : String(error);
  return (
    <p role="alert" className="rounded-m3-lg bg-bad/12 px-4 py-3 text-sm break-words text-bad">
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

type ButtonClassOptions = { variant?: ButtonVariant; size?: ButtonSize; className?: string };

/** Shared Button look so links and buttons stay identical. */
export function buttonClass({ variant = "secondary", size = "md", className }: ButtonClassOptions = {}) {
  return cn(
    "m3-press inline-flex items-center justify-center gap-2 rounded-full text-sm font-bold",
    size === "sm" ? "h-9 px-4" : "h-11 px-5",
    focusRing,
    "disabled:cursor-not-allowed disabled:opacity-50",
    buttonVariants[variant],
    className,
  );
}

/** Pill button. Legacy "primary" renders as filled and "secondary" as tonal. */
export function Button({
  variant = "secondary",
  size = "md",
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: ButtonSize }) {
  return <button type="button" {...props} className={buttonClass({ variant, size, className })} />;
}

type LinkButtonOwnProps = ButtonClassOptions & {
  children?: ReactNode;
  title?: string;
  "aria-label"?: string;
  "aria-current"?: "page" | "true" | "false";
  onClick?: () => void;
};

type RouteLinkProps<TFrom extends string, TTo extends string | undefined> = LinkComponentProps<"a", RegisteredRouter, TFrom, TTo>;
export type LinkButtonProps<TFrom extends string = string, TTo extends string | undefined = "."> =
  | (LinkButtonOwnProps & RouteLinkProps<TFrom, TTo> & { className?: string; href?: never })
  | (LinkButtonOwnProps & { href: string; className?: string; to?: never; search?: never; params?: never });

/**
 * Link styled as a Button: a TanStack Router link for `to` (route, params and search are checked
 * against the registered route tree), a plain anchor for `href`.
 */
export function LinkButton<const TFrom extends string = string, const TTo extends string | undefined = ".">(props: LinkButtonProps<TFrom, TTo>) {
  const { variant = "text", size = "md", className, children, ...rest } = props;
  const cls = buttonClass({ variant, size, className });
  if ("href" in rest && typeof rest.href === "string") {
    const { to: _to, search: _search, params: _params, ...anchor } = rest as typeof rest & { to?: never; search?: never; params?: never };
    return (
      <a className={cls} {...anchor}>
        {children}
      </a>
    );
  }
  // The union is already narrowed by the public signature; the router link re-derives it per route.
  return (
    <Link className={cls} {...(rest as unknown as { to: "." })}>
      {children}
    </Link>
  );
}

export type IconButtonVariant = "standard" | "tonal" | "filled";
const iconButtonVariants: Record<IconButtonVariant, string> = {
  standard: "text-on-surface-variant hover:bg-on-surface/8",
  tonal: "bg-secondary-container text-on-secondary-container hover:brightness-110",
  filled: "bg-primary text-on-primary hover:brightness-110",
};

/** Round icon button: "md" is 44px; "sm" is 36px visible with a 44px hit area. `aria-label` is required: there is no visible text. */
export function IconButton({
  icon,
  variant = "standard",
  size = "md",
  className,
  ...props
}: Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children" | "aria-label"> & { icon: LucideIcon; "aria-label": string; variant?: IconButtonVariant; size?: "md" | "sm" }) {
  return (
    <button
      type="button"
      {...props}
      className={cn(
        "m3-press inline-flex shrink-0 items-center justify-center rounded-full",
        size === "sm" ? "relative size-9 before:absolute before:-inset-1 before:content-['']" : "size-11",
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

/** Removable tag (applied filter, selected camera). The remove target is 28px visible with a 44px hit area. Omit `onRemove` for a fixed chip. */
export function RemovableChip({
  label,
  removeLabel,
  onRemove,
  tone = "secondary",
  className,
}: {
  label: ReactNode;
  removeLabel: string;
  onRemove?: () => void;
  tone?: "secondary" | "primary";
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex h-9 items-center gap-1 rounded-full pl-3 text-sm font-medium",
        onRemove ? "pr-1" : "pr-3",
        tone === "primary" ? "bg-primary-container text-on-primary-container" : "bg-secondary-container text-on-secondary-container",
        className,
      )}
    >
      <span className="min-w-0 truncate">{label}</span>
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          aria-label={removeLabel}
          className={cn("relative inline-flex size-7 shrink-0 items-center justify-center rounded-full before:absolute before:-inset-2 before:content-[''] hover:bg-on-surface/10", focusRing)}
        >
          <Icon icon={X} size="xs" />
        </button>
      )}
    </span>
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

export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      {...props}
      className={cn(
        "min-h-24 w-full rounded-m3-md border border-transparent bg-surface-2 px-3 py-3 text-sm placeholder:text-muted/70 focus-visible:outline-2 focus-visible:outline-primary aria-[invalid=true]:border-bad disabled:opacity-50",
        props.className,
      )}
    />
  );
}

/** Native checkbox (role=checkbox) in a 44px row. The label is the accessible name; `description` is announced as a description. */
export function Checkbox({
  checked,
  onChange,
  label,
  description,
  disabled,
  className,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  label: ReactNode;
  description?: string;
  disabled?: boolean;
  className?: string;
}) {
  const descId = useId();
  return (
    <div className={cn("flex flex-col text-sm", className)}>
      <label className={cn("flex min-h-11 items-center gap-3", disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer")}>
        <input
          type="checkbox"
          checked={checked}
          disabled={disabled}
          aria-describedby={description ? descId : undefined}
          onChange={(e) => onChange(e.target.checked)}
          className={cn("size-5 shrink-0 accent-primary", focusRing)}
        />
        <span>{label}</span>
      </label>
      {description && (
        <span id={descId} className="-mt-2 pb-2 pl-8 text-xs text-on-surface-variant">
          {description}
        </span>
      )}
    </div>
  );
}

/** Tonal empty state, optionally with an icon. */
export function Empty({ children, icon }: { children: ReactNode; icon?: LucideIcon }) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-m3-xl bg-surface-1 px-4 py-8 text-center text-sm text-on-surface-variant">
      {icon && <Icon icon={icon} size="md" />}
      <p>{children}</p>
    </div>
  );
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
    <div className="overflow-x-auto rounded-m3-xl bg-surface-1">
      <table
        aria-label={label}
        className="w-full text-left text-sm [&_td]:h-12 [&_td]:px-4 [&_th]:px-4 [&_th]:py-3 [&_tbody_tr]:border-b [&_tbody_tr]:border-outline-variant [&_tbody_tr:last-child]:border-b-0"
      >
        {children}
      </table>
    </div>
  );
}

export function Th({ children, className }: { children?: ReactNode; className?: string }) {
  return <th className={cn("bg-surface-2 text-xs font-bold text-on-surface-variant", className)}>{children}</th>;
}
