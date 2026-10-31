import type { IntegrationStatus } from "./IntegrationsTab";

/**
 * One line explaining what a backend integration *is*, so its status badge means something to
 * someone who does not know the platform. Keyed by integration id (see camconfig.Integration*).
 */
export const INTEGRATION_WHAT: Record<string, string> = {
  frigate: "Detección de objetos (personas, vehículos, …) que hace el servidor Frigate sobre el stream de la cámara.",
  onvif: "Lectura y control de la cámara con el protocolo estándar ONVIF: streams, configuración de video y PTZ.",
  provision_isr:
    "Analíticas y LPR que procesa la propia cámara Provision-ISR y reporta al agente de OpenVMS del servidor por HTTP.",
  lpr_provision: "Patentes que reconoce la cámara Provision-ISR y envía al agente de OpenVMS.",
  analytics_provision: "Objetos que detecta la cámara Provision-ISR (analítica Profile M) y envía al agente.",
  analytics_frigate: "Objetos que detecta Frigate sobre el stream de la cámara.",
  lpr_frigate: "Patentes que reconoce Frigate sobre el stream de la cámara.",
};

/** What a person should do to clear a problem status, or null when there is nothing to advise. */
export function integrationRemediation(id: string, status: IntegrationStatus): string[] | null {
  if (status === "connected") return null;
  switch (id) {
    case "provision_isr":
      if (status === "error") {
        return [
          "La cámara todavía no reporta al agente: no está registrada o nunca mandó su keepalive.",
          "1. Abrí el panel “Provision LPR” (pestaña Avanzado de esta cámara) y usá “Registrar cámara y preparar configuración”.",
          "2. En la web de la cámara: Configuración → Red → Configuración de Servidor → HTTP POST / API, con la IP del agente y el puerto 7419 (keepalive /SendKeepalive, eventos /v1/provision-lpr/event).",
          "3. Con el primer keepalive el estado pasa a Conectado.",
        ];
      }
      if (status === "not_configured") {
        return [
          "Esta cámara no se reconoce como Provision-ISR. Si lo es, forzá la compatibilidad Provision-ISR en el panel “Provision LPR”.",
        ];
      }
      return null;
    case "lpr_provision":
      if (status === "error") {
        return ["Registrá y configurá la cámara Provision-ISR en el panel “Provision LPR” (pestaña Avanzado)."];
      }
      if (status === "not_configured") {
        return ["Sin LPR Provision. Si la cámara lo soporta, activá el servicio LPR en el panel “Provision LPR”."];
      }
      return null;
    case "analytics_provision":
      if (status === "unknown" || status === "not_configured") {
        return ["Configurá la cámara Provision-ISR para que reporte sus analíticas al agente (panel “Provision LPR”)."];
      }
      return null;
    case "onvif":
      if (status === "error") {
        return [
          "OpenVMS no pudo leer la cámara por ONVIF. Revisá que la IP ONVIF sea alcanzable desde el servidor (agente) y volvé a probar.",
        ];
      }
      if (status === "unknown") {
        return ["Todavía no se verificó: usá el check ONVIF de la cámara para leerla y actualizar sus capacidades."];
      }
      return null;
    case "frigate":
      if (status === "error") {
        return ["No se pudo leer Frigate. Revisá que el servidor Frigate responda (estado del servidor Frigate)."];
      }
      if (status === "not_configured") {
        return ["La cámara no está en la configuración de Frigate: agregala en el servidor Frigate."];
      }
      return null;
    case "analytics_frigate":
    case "lpr_frigate":
      if (status === "error") {
        return ["No se pudo leer Frigate. Revisá el estado del servidor Frigate."];
      }
      if (status === "not_configured") {
        return ["Habilitá esta opción en la configuración de Frigate de la cámara."];
      }
      return null;
    default:
      return null;
  }
}
