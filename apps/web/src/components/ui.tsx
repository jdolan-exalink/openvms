import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from "react";
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

type StatusStyle = { label: string; dot: string; text: string };
const unknownStatus: StatusStyle = { label: "Sin datos", dot: "bg-muted", text: "text-muted" };
const statusStyles: Record<string, StatusStyle> = {
  online: { label: "En línea", dot: "bg-ok", text: "text-ok" },
  degraded: { label: "Degradado", dot: "bg-warn", text: "text-warn" },
  offline: { label: "Fuera de línea", dot: "bg-bad", text: "text-bad" },
};

export function StatusBadge({ status }: { status: string }) {
  const s = statusStyles[status] ?? unknownStatus;
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs whitespace-nowrap", s.text)}>
      <span className={cn("size-2 rounded-full", s.dot)} aria-hidden />
      {s.label}
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

export function Button({ variant = "secondary", className, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" }) {
  return (
    <button
      type="button"
      {...props}
      className={cn(
        "inline-flex items-center justify-center gap-2 rounded px-3 py-1.5 text-sm font-medium",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-50",
        variant === "primary" ? "bg-accent text-bg hover:bg-accent/90" : "border border-line bg-surface hover:bg-raised",
        className,
      )}
    />
  );
}

const fieldClass =
  "w-full rounded border border-line bg-bg px-3 py-1.5 text-sm placeholder:text-muted/70 focus-visible:outline-2 focus-visible:outline-accent";

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span className="font-medium">{label}</span>
      {children}
      {hint && <span className="text-xs text-muted">{hint}</span>}
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
