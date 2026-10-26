import { CheckCircle2, Info } from "lucide-react";
import { Icon } from "@/components/Icon";
import { cn } from "@/lib/cn";

/** ConfigureResult is what configuring a Provision-ISR camera answers; see ConfigureCameraProvisionLPRHandler. */
export interface ConfigureResult {
  /** True only when the camera is already reporting to the agent. */
  success: boolean;
  verified?: boolean;
  registered?: boolean;
  registration_error?: string;
  method?: string;
  message?: string;
  web_ui_path?: string;
  parameters?: Record<string, string>;
}

function registrationLine(r: ConfigureResult): string | null {
  if (r.registered) return "Cámara registrada en el agente: se identifica por su dirección.";
  switch (r.registration_error) {
    case undefined:
      return null;
    case "agent_outdated":
      return "El agente de este servidor es anterior al registro de cámaras: actualizalo para registrar la cámara.";
    case "agent_unreachable":
      return "No se pudo contactar al agente para registrar la cámara.";
    default:
      return `No se pudo registrar la cámara: ${r.registration_error}`;
  }
}

/**
 * ProvisionConfigureResult says where the camera stands after "configure": registered on the agent or
 * not, and whether it already reports. It never claims success on its own: the camera has to be
 * configured by hand in its web interface, so success is only shown once its keepalives arrive.
 */
export function ProvisionConfigureResult({ result }: { result: ConfigureResult }) {
  const registration = registrationLine(result);
  // Manual instructions are how this integration works, not a failure: only a registration problem is a warning.
  const tone = result.success ? "bg-ok/15 text-ok" : result.registration_error ? "bg-warn/15 text-warn" : "bg-surface-2 text-on-surface";
  return (
    <div className={cn("flex flex-col gap-1.5 rounded-m3-xl p-3 text-xs", tone)}>
      {!result.success && <span className="text-[11px] font-bold uppercase tracking-wider opacity-80">Instrucciones manuales</span>}
      <div className="flex items-center gap-2 font-bold">
        <Icon icon={result.success ? CheckCircle2 : Info} size="xs" />
        <span>{result.message || (result.success ? "La cámara ya reporta a este agente." : "Falta configurar la cámara.")}</span>
      </div>
      {registration && <p className="text-[11px] opacity-90">{registration}</p>}
      {!result.success && (
        <p className="text-[11px] opacity-90">
          OpenVMS no aplica estos valores en la cámara: el fabricante no documenta cómo hacerlo de forma remota. Ingresalos en su
          interfaz web con los parámetros de la derecha; cuando empiece a reportar, queda verificada.
        </p>
      )}
    </div>
  );
}
