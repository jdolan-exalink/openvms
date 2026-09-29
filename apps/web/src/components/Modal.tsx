import { X } from "lucide-react";
import { type ReactNode, useRef } from "react";
import { useFocusTrap } from "@/lib/useFocusTrap";

/**
 * Modal is a hand-rolled accessible dialog (role="dialog" aria-modal="true"), not the
 * native <dialog>/showModal(): jsdom (this project's vitest environment) does not implement
 * showModal, which would make every modal test fail outright. Esc closes it, Tab/Shift+Tab
 * are trapped inside, and focus returns to whatever triggered it on close (useFocusTrap).
 */
export function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const containerRef = useRef<HTMLDivElement>(null);
  useFocusTrap(containerRef, onClose);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div ref={containerRef} role="dialog" aria-modal="true" aria-label={title} className="flex max-h-[90vh] w-full max-w-3xl flex-col gap-4 overflow-auto rounded border border-line bg-surface p-4 shadow-lg">
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button type="button" aria-label="Cerrar" onClick={onClose} className="rounded p-1 hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent">
            <X className="size-5" aria-hidden />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
