# OpenVMS — Desarrollo del plugin Provision-ISR

> Verbatim copy of the prompt the user pasted on 2026-10-23 (source of the specs in
> `provision-isr-plugin.md`). Do not edit; corrections go in the Log of the feature document.

## 1. Objetivo

Desarrollar un plugin integral para cámaras **Provision-ISR**, que permita aprovechar sus motores internos de inteligencia artificial, centralizando los eventos en OpenVMS.

El plugin deberá soportar LPR y todas las analíticas que realmente estén disponibles y puedan obtenerse de cada modelo y firmware.

**Principio fundamental:** aprovechar primero el procesamiento de las cámaras y evitar duplicar inferencias en Frigate o OpenVMS cuando no sea necesario.

## 2. Arquitectura existente

OpenVMS cuenta con:

- Backend central y frontend React.
- Agentes OpenVMS instalados en servidores Frigate.
- Integración LPR Provision-ISR existente mediante HTTP POST.
- Módulos LPR, Vision AI, Live View, búsqueda y alarmas.
- Arquitectura multiservidor y multisitio.

El agente OpenVMS deberá recibir los eventos POST y transmitirlos al backend central.

Flujo propuesto:

```text
CÁMARA PROVISION-ISR
   │
   ├── LPR
   ├── Detección facial
   ├── Detección de personas
   ├── Detección de vehículos
   └── Otras analíticas compatibles
   │
   ▼
HTTP POST
   │
   ▼
AGENTE OPENVMS
   ├── Receptor HTTP
   ├── Parser Provision-ISR
   ├── Normalización
   ├── Deduplicación
   ├── Cola persistente
   └── Transporte al central
   │
   ▼
OPENVMS CENTRAL
   ├── LPR
   ├── Vision AI
   ├── Reconocimiento facial
   ├── Búsqueda
   ├── Alarmas
   └── Base de datos
```

## 3. Auditoría inicial obligatoria

Antes de programar:

1. Inspeccionar el repositorio completo de OpenVMS.
2. Localizar el código de integración LPR Provision-ISR.
3. Identificar cómo recibe eventos el agente.
4. Identificar cómo se comunican los agentes con el backend central.
5. Revisar los esquemas existentes de cámaras, eventos y detecciones.
6. Identificar los módulos de búsqueda, Vision AI y alarmas.
7. Proponer cómo ampliar la integración sin romper funcionalidades existentes.

**No crear una integración paralela si podemos extender la existente.**

## 4. Compatibilidad Provision-ISR

Investigar la línea I6, especialmente el equipo disponible para pruebas, identificado inicialmente como I6-340.

Confirmar el modelo exacto y su firmware.

Investigar y comprobar:

- LPR.
- Detección de rostros.
- Detección de personas.
- Detección y clasificación de vehículos.
- Cruce de línea.
- Intrusión en áreas.
- Atributos de personas y vehículos.
- Eventos HTTP POST.
- ONVIF Events y Profile M, cuando corresponda.
- SDK o API del fabricante, si existen.

No asumir que todas las cámaras soportan las mismas capacidades.

Construir una matriz que diferencie funcionalidades confirmadas, pendientes de verificar y no soportadas.

## 5. Receptor de eventos

Extender el agente OpenVMS para recibir eventos de Provision-ISR.

Requisitos:

- Reutilizar el receptor LPR actual cuando sea posible.
- Aceptar los formatos realmente enviados por las cámaras.
- Identificar inequívocamente la cámara de origen.
- Validar autenticación y origen.
- Registrar eventos con fecha y hora.
- Procesar imágenes adjuntas cuando estén disponibles.
- Detectar mensajes duplicados.
- Soportar múltiples cámaras simultáneamente.
- Evitar pérdida de eventos durante cortes del servidor central.

Implementar una cola persistente con reintentos e idempotencia.

No confiar únicamente en la IP de origen como mecanismo de autenticación.

## 6. Normalización de eventos

Crear un formato interno independiente del fabricante.

Ejemplo conceptual:

```json
{
  "source": "provision-isr",
  "camera_id": "camera_001",
  "event_type": "face_detected",
  "timestamp": "2026-10-23T18:00:00-03:00",
  "confidence": 0.91,
  "bbox": [120, 80, 180, 210],
  "snapshot_ref": "snapshot_123"
}
```

Este JSON es ilustrativo, no representa necesariamente el formato enviado por Provision-ISR.

El objetivo es que OpenVMS procese los eventos sin depender del fabricante.

Conservar la procedencia original y los datos necesarios para auditoría.

## 7. Integración con Frigate

Permitir configurar, por cámara y por tipo de analítica:

- Motor nativo Provision-ISR.
- Motor Frigate.
- Ambos motores.

Cuando ambos detecten el mismo evento, implementar correlación y deduplicación sin descartar eventos legítimos.

No ejecutar automáticamente un segundo LPR si ya existe una lectura válida procedente de Provision-ISR.

Mantener Frigate funcionando independientemente del plugin.

## 8. Reconocimiento facial

Separar claramente dos funciones.

**Detección facial:** la cámara detecta que existe un rostro y, si lo soporta, proporciona ubicación, confianza e imagen.

**Reconocimiento facial:** OpenVMS procesa el rostro mediante un modelo especializado para obtener un embedding y permitir búsquedas o comparaciones.

Diseñar una segunda etapa opcional:

1. Recibir evento facial.
2. Obtener el recorte o snapshot disponible.
3. Evaluar resolución, nitidez, orientación y calidad.
4. Descartar imágenes insuficientes para reconocimiento fiable.
5. Generar embedding con un modelo facial evaluado técnicamente.
6. Permitir búsqueda de similitud entre cámaras y servidores.
7. Aplicar umbrales, controles de acceso y retención adecuados.

No considerar una coincidencia biométrica como identificación concluyente. No habilitar identificación automática de personas por defecto.

La detección facial deberá funcionar aunque el reconocimiento esté desactivado.

## 9. Configuración desde OpenVMS

Agregar una sección **Analíticas de cámara** dentro de la configuración.

Mostrar:

- Fabricante y modelo.
- Firmware, cuando sea detectable.
- Estado del plugin.
- Dirección de recepción de eventos.
- Último evento recibido.
- Analíticas disponibles y habilitadas.
- Fuente de inferencia seleccionada.
- Diagnóstico de conectividad.

Permitir habilitar y deshabilitar las analíticas soportadas desde la interfaz, cuando exista un mecanismo técnico comprobado.

Incorporar un sistema de diagnóstico para visualizar los eventos recibidos y los errores de integración.

## 10. Integración con módulos existentes

**LPR:** reutilizar el módulo actual, incluyendo búsqueda, imágenes, filtros y correlación.

**Vision AI:** permitir que las detecciones nativas Provision-ISR participen en la selección dinámica de cámaras.

**Alarmas:** permitir crear reglas basadas en eventos de Provision-ISR, utilizando el motor propio de alarmas OpenVMS.

**Búsqueda:** unificar eventos independientemente de su origen, manteniendo filtros por cámara, servidor, fecha, tipo y fuente.

**Permisos:** respetar los permisos existentes por sitio, servidor, cámara y usuario.

## 11. Diseño extensible a otros fabricantes

No desarrollar un componente rígido exclusivo de Provision-ISR.

Crear una interfaz de proveedores que permita agregar posteriormente:

- Hikvision.
- Dahua.
- Axis.
- Hanwha.
- Otros fabricantes ONVIF.

Cada proveedor implementará su propio adaptador de eventos, mientras OpenVMS utilizará un esquema común.

No implementar esas marcas ahora: solamente preparar una arquitectura extensible.

## 12. Pruebas con la cámara I6

Realizar una prueba real de integración:

1. Confirmar modelo y firmware.
2. Capturar eventos LPR actuales.
3. Activar detección facial y otras analíticas disponibles.
4. Capturar POST originales y sus encabezados.
5. Verificar si incluyen snapshots o metadatos.
6. Crear ejemplos de prueba anonimizados.
7. Verificar recepción simultánea de eventos.
8. Probar desconexiones, reinicios y duplicados.
9. Verificar aparición en OpenVMS central.
10. Comprobar que el LPR existente continúa funcionando.

Si alguna analítica no se transmite mediante POST, investigar otros mecanismos documentados, sin simular compatibilidad.

## 13. Rendimiento

El plugin debe ser liviano.

Priorizar:

- Procesamiento de eventos en lugar de decodificación continua de video.
- Uso mínimo de CPU y RAM.
- Cola persistente con límites de almacenamiento.
- Comunicación eficiente con el backend.
- Procesamiento asíncrono.
- Métricas por cámara y por agente.

No ejecutar modelos de inferencia adicionales en el agente salvo que exista una necesidad justificada y configurable.

## 14. Entregables

1. Auditoría del código existente.
2. Matriz de capacidades Provision-ISR.
3. Diseño de arquitectura y contratos.
4. Plugin integrado en el agente OpenVMS.
5. Integración con el backend central.
6. Configuración desde la interfaz.
7. Integración con LPR, Vision AI, búsqueda y alarmas.
8. Tests unitarios, integración y pruebas reales.
9. Documentación de instalación, configuración y diagnóstico.
10. Plan de rollback.

## 15. Criterios de aceptación

El desarrollo se considera terminado cuando:

- El LPR existente continúa funcionando.
- Los eventos de analíticas comprobadas llegan al servidor central.
- Se identifica correctamente la cámara de origen.
- No se generan eventos duplicados por reintentos.
- El agente recupera eventos pendientes después de una desconexión.
- Los eventos aparecen en los módulos correspondientes.
- Una cámara no autorizada no puede inyectar eventos.
- El sistema informa qué analíticas están realmente disponibles.
- La solución funciona con múltiples cámaras y servidores.

## 16. Instrucción final para Claude

**Primero inspeccioná el proyecto existente.**

Presentá un diagnóstico técnico y un plan de implementación por etapas, indicando los archivos y componentes que modificarías.

Reutilizá al máximo el código funcional y preservá la compatibilidad.

Implementá progresivamente, ejecutando pruebas después de cada etapa.

No inventes protocolos, endpoints ni capacidades del fabricante.

El resultado debe ser una integración nativa de OpenVMS, preparada para producción y ampliable a otras marcas.
