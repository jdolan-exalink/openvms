import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Clock, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, Checkbox, ErrorNote, Field, TextInput } from "@/components/ui";

interface EditExportModalProps {
  job: {
    id: string;
    name: string;
    protected: boolean;
    expires_at?: string | null;
  };
  onClose: () => void;
  onSaved?: () => void;
}

export function EditExportModal({ job, onClose, onSaved }: EditExportModalProps) {
  const qc = useQueryClient();
  const [name, setName] = useState(job.name);
  const [isProtected, setIsProtected] = useState(job.protected);

  const mutation = useMutation({
    mutationFn: async () => {
      const res = await fetch(`/api/v1/export-jobs/${job.id}`, {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
          "X-OpenVMS-Request": "1",
        },
        body: JSON.stringify({
          name: name.trim(),
          protected: isProtected,
        }),
      });
      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw new Error(err.message || "Error al actualizar la exportación");
      }
      return res.json();
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["export-jobs"] });
      onSaved?.();
      onClose();
    },
  });

  return (
    <Modal
      title="Editar trabajo de exportación"
      onClose={onClose}
      className="max-w-lg bg-surface-1 border border-outline-variant/60 p-6 flex flex-col gap-5"
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim()) mutation.mutate();
        }}
        className="flex flex-col gap-4"
      >
        <Field label="Nombre de la exportación / evidencia">
          <TextInput
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Ej: Incidente Acceso Principal"
            required
            autoFocus
          />
        </Field>

        <div className="flex flex-col gap-3 p-4 rounded-m3-lg bg-surface-2/60 border border-outline-variant/40">
          <Checkbox
            checked={isProtected}
            onChange={setIsProtected}
            label="Proteger evidencia forense"
            description="Evita la eliminación automática por política de retención o limpieza."
          />

          {isProtected ? (
            <div className="flex items-start gap-2.5 p-3 rounded-m3-md bg-primary/10 border border-primary/20 text-xs text-on-surface">
              <Icon icon={ShieldCheck} size="sm" className="text-primary shrink-0 mt-0.5" />
              <div>
                <span className="font-semibold text-primary block">Retención permanente garantizada</span>
                <span className="text-muted">
                  Esta evidencia no vencerá ni será eliminada por el recolector automático de almacenamiento.
                </span>
              </div>
            </div>
          ) : (
            <div className="flex items-start gap-2.5 p-3 rounded-m3-md bg-warn-container/60 border border-warn/30 text-xs text-on-surface">
              <Icon icon={Clock} size="sm" className="text-warn shrink-0 mt-0.5" />
              <div>
                <span className="font-semibold text-warn block">Auto-borrado programado (30 días)</span>
                <span className="text-muted">
                  Al no estar protegida, la evidencia se eliminará automáticamente en 30 días para preservar espacio en disco.
                </span>
              </div>
            </div>
          )}
        </div>

        <ErrorNote error={mutation.error} />

        <div className="flex items-center justify-end gap-2 pt-2 border-t border-outline-variant/40">
          <Button variant="outlined" type="button" onClick={onClose}>
            Cancelar
          </Button>
          <Button
            variant="filled"
            type="submit"
            disabled={!name.trim() || mutation.isPending}
          >
            {mutation.isPending ? "Guardando..." : "Guardar cambios"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
