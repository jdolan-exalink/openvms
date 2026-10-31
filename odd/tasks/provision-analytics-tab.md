# ODD: Analíticas Provision, Tab Analíticas Unificada y Controles de Estado de Integraciones

## Contexto y Objetivos
1. Resolver el problema de funcionamiento de las analíticas de Provision (ingesta de eventos, compatibilidad HTTP Push y ONVIF Pull-Point).
2. Unificar la pestaña **Integraciones** dentro de **Analíticas** en la configuración de cámaras (`FrigateCameraConfig`).
3. Incorporar controles de estado con capacidad de selección y prueba interactiva ("Probar / Testear") para los 4 motores/servicios:
   - **LPR Provision-ISR**
   - **Analíticas Provision**
   - **Analíticas Frigate**
   - **LPR Frigate**
4. Diferenciar y marcar explícitamente el origen de cada analítica en las tarjetas de módulos:
   - **Internas de Frigate**
   - **Internas de cámaras Provision**
   - **ONVIF**
   - Para LPR: diferenciar **LPR** (Frigate) vs **LPR Provision-ISR** (Provision).
5. Mejorar sustancialmente la UI/UX, estados visuales, badges e interactividad de prueba.

---

## Plan de Tareas

- [x] **1. Backend: Modelo de Integraciones y Módulos de Analítica** (`internal/camconfig`)
  - [x] Extender `Integrations` para modelar explícitamente los 4 estados: `lpr_provision`, `analytics_provision`, `analytics_frigate`, `lpr_frigate`.
  - [x] En `internal/camconfig/analytics.go`, etiquetar orígenes como `"Internas de Frigate"`, `"Internas de cámaras Provision"`, `"ONVIF"`, `"OpenVMS"`.
  - [x] Mostrar `"LPR"` para Frigate y `"LPR Provision-ISR"` para Provision.
  - [x] Exponer módulos nativos inteligentes de Provision/ONVIF (`line_crossing`, `intrusion`, `people_counting`, `face`, etc.) cuando la cámara tiene evidencia Provision/ONVIF.
  - [x] Tests unitarios en `internal/camconfig/integrations_test.go` y `internal/camconfig/analytics_test.go`.

- [x] **2. Backend: Endpoints de Prueba para los 4 Servicios** (`internal/api` y `internal/provision`)
  - [x] Implementar/extender endpoints de prueba para que cada uno de los 4 motores pueda ejecutarse bajo demanda:
    - `lpr_provision`: verificación contra edge-agent (keepalive, registry, última lectura).
    - `lpr_frigate`: verificación de Frigate LPR.
    - `analytics_provision`: sondeo de analítica Provision (HTTP receiver / ONVIF pull-events).
    - `analytics_frigate`: verificación de detección de objetos/movimiento en Frigate.
  - [x] Tests en `internal/api/` para los endpoints de prueba de analíticas e integraciones.

- [x] **3. Frontend: Navegación y Estructura de Pestañas** (`apps/web/src/lib/cameraTabs.ts`)
  - [x] Mover la sección `INTEGRATIONS_STATUS` dentro de la pestaña `Analíticas`.
  - [x] Reubicar `CAMERA_WEB` y `ONVIF_INFO` en la pestaña `Avanzado`.
  - [x] Asegurar que `Analíticas` siempre esté disponible y visible cuando haya integraciones o analíticas activas/soportadas.
  - [x] Actualizar tests en `cameraTabs.test.ts` y `FrigateCameraConfig.test.tsx`.

- [x] **4. Frontend: Panel de Estado de Integraciones con Pruebas Interactivas** (`apps/web/src/components/camera/integrations/`)
  - [x] Crear componente con diseño moderno para los 4 motores:
    - Tarjeta por motor con icono, estado (`Conectado`, `No configurado`, `Error`, `Sin verificar`), badge de origen y detalles.
    - Botón "Probar" interactivo con spinner y reporte de resultado claro (éxito/falla, latencia, eventos observados).
  - [x] Integrar este panel en la parte superior de `AnalyticsTab`.

- [x] **5. Frontend: Módulos de Analítica con Origen Claro y Nombres Diferenciados** (`apps/web/src/components/camera/analytics/`)
  - [x] Actualizar `ModuleCard` y `draft.ts` para mostrar chips destacados de origen:
    - Chip "Internas de Frigate"
    - Chip "Internas de cámaras Provision"
    - Chip "ONVIF"
  - [x] Diferenciar "LPR" vs "LPR Provision-ISR".
  - [x] Actualizar tests en `AnalyticsTab.test.tsx` y `ModuleCard.test.tsx`.

- [x] **6. Diagnóstico y Corrección de Analíticas Provision en Ingesta**
  - [x] Verificar el flujo de ingesta de eventos de analíticas inteligentes Provision en `agent_ingest.go` y `provisionlpr`.
  - [x] Documentar y verificar los resultados end-to-end.
