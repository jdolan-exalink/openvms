import { X } from "lucide-react";
import { type ReactNode, useRef } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/cn";
import { useFocusTrap } from "@/lib/useFocusTrap";

/**
 * Modal is a hand-rolled accessible dialog (role="dialog" aria-modal="true"), not the
 * native <dialog>/showModal(): jsdom (this project's vitest environment) does not implement
 * showModal, which would make every modal test fail outright. Esc closes it, Tab/Shift+Tab
 * are trapped inside, and focus returns to whatever triggered it on close (useFocusTrap).
 */
export function Modal({ title, onClose, children, className }: { title: string; onClose: () => void; children: ReactNode; className?: string }) {
  const containerRef = useRef<HTMLDivElement>(null);
  useFocusTrap(containerRef, onClose);

  const dialog = (
    <div
      className="fixed inset-0 z-[80] flex items-center justify-center bg-scrim p-4"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div ref={containerRef} role="dialog" aria-modal="true" aria-label={title} className={cn("flex max-h-[90vh] w-full max-w-3xl flex-col gap-4 overflow-auto rounded-m3-2xl bg-surface-1 p-6 shadow-lg", className)}>
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-[22px] font-bold">{title}</h2>
          <button type="button" aria-label="Cerrar" onClick={onClose} className="m3-press inline-flex size-11 items-center justify-center rounded-full hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary">
            <X className="size-5" aria-hidden />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
  return createPortal(dialog, document.body);
}
