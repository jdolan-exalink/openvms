import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ApiError, api, type Schemas, unwrap } from "@/api/client";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, Field, TextInput, Textarea } from "@/components/ui";

type ConnectResult = Schemas["ServerAgentConnectResult"];

interface HeaderRow {
  name: string;
  value: string;
}

const failureText: Record<string, string> = {
  agent_unreachable: "OpenVMS no pudo llegar al agente en esa dirección. Revisá la dirección, la VPN o el túnel, y que el agente esté en marcha.",
  agent_token_rejected: "El agente rechazó el token. Creá uno nuevo en el equipo con «openvms-agent connection» o «openvms-agent token create».",
  agent_token_scope: "El token es válido pero no tiene el alcance de lectura (read). Creá uno con ese alcance.",
  not_an_agent: "Esa dirección respondió, pero no es un agente de OpenVMS (o es anterior a la 0.3).",
};

const scopeRows: { key: keyof ConnectResult["scopes"]; label: string }[] = [
  { key: "read", label: "Lectura (estado del agente)" },
  { key: "events", label: "Lecturas de patentes de las cámaras" },
  { key: "control", label: "Control (ONVIF y registro de cámaras)" },
];

/**
 * ConnectAgentModal registers an agent that is already running somewhere OpenVMS can reach: its
 * address (a LAN or VPN address, or a name published through a tunnel) and a token created on the agent
 * host. OpenVMS calls the agent before storing anything, so a typo never replaces a working agent.
 */
export function ConnectAgentModal({ server, onClose }: { server: Schemas["Server"]; onClose: () => void }) {
  const qc = useQueryClient();
  const [url, setUrl] = useState("");
  const [token, setToken] = useState("");
  const [rows, setRows] = useState<HeaderRow[]>([{ name: "", value: "" }]);
  const [ca, setCa] = useState("");
  const [needsReplace, setNeedsReplace] = useState(false);
  const [replace, setReplace] = useState(false);
  const [localError, setLocalError] = useState("");
  const [result, setResult] = useState<ConnectResult | null>(null);

  const connect = useMutation({
    mutationFn: () => {
      const headers: Record<string, string> = {};
      for (const row of rows) if (row.name.trim() !== "" && row.value !== "") headers[row.name.trim()] = row.value;
      const body: Schemas["ServerAgentConnectRequest"] = { url: url.trim(), token: token.trim() };
      if (Object.keys(headers).length > 0) body.headers = headers;
      if (ca.trim() !== "") body.ca_pem = ca.trim();
      if (replace) body.replace_existing = true;
      return api.PUT("/api/v1/servers/{serverId}/agent/connection", { params: { path: { serverId: server.id } }, body }).then(unwrap);
    },
    onSuccess: async (data) => {
      setResult(data);
      setToken(""); // the token is not kept once it is stored
      setNeedsReplace(false);
      await qc.invalidateQueries({ queryKey: ["server-agent", server.id] });
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 409) setNeedsReplace(true);
    },
  });

  const submit = () => {
    setLocalError("");
    if (url.trim() === "" || token.trim() === "") {
      setLocalError("Completá la dirección y el token del agente.");
      return;
    }
    connect.mutate();
  };

  const serverError = connect.error instanceof ApiError && connect.error.status !== 409 ? (failureText[connect.error.code] ?? connect.error.message) : null;

  return (
    <Modal title={`Conectar agente · ${server.name}`} onClose={onClose}>
      {result ? (
        <div className="flex flex-col gap-3">
          <p className="text-sm">
            Agente conectado en <span className="font-mono">{result.base_url}</span>. Versión {result.version}.
          </p>
          <ul aria-label="Lo que permite el token" className="flex flex-col gap-1 text-sm">
            {scopeRows.map(({ key, label }) => (
              <li key={key} className={result.scopes[key] ? "text-ok" : "text-warn"}>
                {result.scopes[key] ? "✓" : "✗"} {label}
              </li>
            ))}
          </ul>
          {(!result.scopes.events || !result.scopes.control) && (
            <p role="status" className="rounded-m3-lg bg-warn/15 p-2 text-xs text-warn">
              {[!result.scopes.events && "Sin el alcance «events» OpenVMS no puede recoger las patentes de las cámaras.", !result.scopes.control && "Control no se pudo comprobar o no está permitido: ONVIF y el registro de cámaras pueden no funcionar."]
                .filter(Boolean)
                .join(" ")}
            </p>
          )}
          <div className="flex justify-end">
            <Button onClick={onClose}>Cerrar</Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <p className="text-xs text-on-surface-variant">
            Para un agente que ya está instalado (por ejemplo con el comando curl de GitHub). OpenVMS se conecta al agente; el equipo del agente no necesita llegar a OpenVMS. El token se
            crea en ese equipo con <span className="font-mono">openvms-agent connection</span>.
          </p>
          <Field label="Dirección del agente" hint="http://10.8.0.5:7419 en una LAN o VPN, https://agente.ejemplo.com si lo publicás por un túnel.">
            <TextInput value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://agente.ejemplo.com" autoComplete="off" spellCheck={false} />
          </Field>
          <Field label="Token del agente" hint="Se guarda cifrado y no se vuelve a mostrar.">
            <TextInput type="password" value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" spellCheck={false} />
          </Field>

          <details>
            <summary className="cursor-pointer text-sm font-medium">Opciones avanzadas</summary>
            <div className="mt-2 flex flex-col gap-3">
              <div className="flex flex-col gap-2">
                <span className="text-xs text-muted">Cabeceras que pide un gateway delante del agente (por ejemplo el token de servicio de Cloudflare Access). Se guardan cifradas.</span>
                {rows.map((row, i) => (
                  <div key={i} className="flex gap-2">
                    <TextInput
                      aria-label={`Nombre de la cabecera ${i + 1}`}
                      value={row.name}
                      placeholder="CF-Access-Client-Id"
                      onChange={(e) => setRows(rows.map((r, j) => (j === i ? { ...r, name: e.target.value } : r)))}
                    />
                    <TextInput
                      aria-label={`Valor de la cabecera ${i + 1}`}
                      type="password"
                      value={row.value}
                      onChange={(e) => setRows(rows.map((r, j) => (j === i ? { ...r, value: e.target.value } : r)))}
                    />
                  </div>
                ))}
                {rows.length < 8 && (
                  <div>
                    <Button size="sm" variant="outlined" onClick={() => setRows([...rows, { name: "", value: "" }])}>
                      Agregar cabecera
                    </Button>
                  </div>
                )}
              </div>
              <Field label="Certificado de la autoridad (PEM)" hint="Solo si el agente usa https con una autoridad privada. Aplica a esta dirección.">
                <Textarea value={ca} onChange={(e) => setCa(e.target.value)} rows={4} spellCheck={false} className="font-mono text-xs" />
              </Field>
            </div>
          </details>

          {needsReplace && (
            <label className="flex items-start gap-2 text-sm">
              <input type="checkbox" checked={replace} onChange={(e) => setReplace(e.target.checked)} className="mt-1" />
              <span>Reemplazar el agente registrado para este servidor. El agente anterior deja de sincronizarse con este OpenVMS.</span>
            </label>
          )}
          {localError && (
            <p role="alert" className="rounded-m3-lg bg-bad/12 px-4 py-3 text-sm text-bad">
              {localError}
            </p>
          )}
          {serverError ? (
            <p role="alert" className="rounded-m3-lg bg-bad/12 px-4 py-3 text-sm break-words text-bad">
              {serverError}
            </p>
          ) : (
            connect.error && !(connect.error instanceof ApiError) && <ErrorNote error={connect.error} />
          )}
          {needsReplace && !replace && (
            <p role="status" className="text-xs text-warn">
              Este servidor ya tiene un agente registrado. Marcá la casilla para reemplazarlo.
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="text" onClick={onClose}>
              Cancelar
            </Button>
            <Button onClick={submit} disabled={connect.isPending}>
              {connect.isPending ? "Conectando…" : "Conectar"}
            </Button>
          </div>
        </div>
      )}
    </Modal>
  );
}
