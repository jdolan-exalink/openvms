# Agent Installation via Curl & Camera Web Auto-Login

## Objective
1. Improve Agent installation & connection UI:
   - Provide automatic prefilled IP/URL based on `server.base_url` (`http://<frigate-host>:7419`).
   - Display clear, easy-to-follow step-by-step instructions for installing via `curl` and obtaining the API key (`/etc/openvms/openvms.apikey` or `openvms-agent connection`).
   - Add copy-to-clipboard buttons for commands.
   - Integrate manual installation option in `AgentInstallDialog` and improve `ConnectAgentModal`.
2. Camera Web auto-login (`camweb`):
   - Leverage stored camera credentials (from RTSP stream or ONVIF) to automatically authenticate / autofill camera web interfaces.
   - Support Basic Auth challenges and DOM input autofill in `shimScript`.
   - Provide visual credential indicator and quick copy in `CameraWebView`.

## Tasks

### Slice 1: Agent Install & Connect UI
- [x] T1.1 — Implement host extraction helper and auto-fill agent URL in `ConnectAgentModal.tsx`.
- [x] T1.2 — Add clear, copyable `curl` install commands and API key retrieval instructions in `ConnectAgentModal.tsx`.
- [x] T1.3 — In `AgentInstallDialog` (`Servers.tsx`), add tab/switch between "Instalación automática (SSH)" and "Instalación manual (Terminal / curl)" with prefilled connect controls.
- [x] T1.4 — Update and add tests in `ConnectAgentModal.test.tsx` and `Servers.test.tsx`.

### Slice 2: Camera Web Auto-Login & Credential Support
- [x] T2.1 — Pass camera credentials (username, password) to `camweb.Service` session.
- [x] T2.2 — Update `camweb/proxy.go` to handle Basic Auth 401 challenges using stored credentials.
- [x] T2.3 — Update `camweb/rewrite.go` (`shimScript`) to auto-populate login form fields if credentials are provided.
- [x] T2.4 — Add credential preview & copy button in `CameraWebView.tsx`.
- [x] T2.5 — Add unit tests for camweb auth handling and `CameraWebView.test.tsx`.
