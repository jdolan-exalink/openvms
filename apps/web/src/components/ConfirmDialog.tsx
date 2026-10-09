import { Modal } from "@/components/Modal";
import { Button, ErrorNote } from "@/components/ui";
import { useT } from "@/i18n";

/** ConfirmDialog replaces window.confirm with an accessible in-UI confirmation (built on Modal). */
export function ConfirmDialog({
  title,
  message,
  confirmLabel,
  cancelLabel,
  pending,
  error,
  onConfirm,
  onCancel,
}: {
  title: string;
  message: string;
  confirmLabel: string;
  /** Defaults to "Cancel"; use a task-specific label when cancel means "keep going". */
  cancelLabel?: string;
  pending?: boolean;
  error?: unknown;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  return (
    <Modal title={title} onClose={onCancel}>
      <p className="text-sm">{message}</p>
      <ErrorNote error={error} />
      <div className="flex justify-end gap-2">
        <Button variant="text" onClick={onCancel}>{cancelLabel ?? t("common.cancel")}</Button>
        <Button variant="danger" disabled={pending} onClick={onConfirm}>
          {confirmLabel}
        </Button>
      </div>
    </Modal>
  );
}
