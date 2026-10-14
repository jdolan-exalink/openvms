:Telemetría y Benchmark de Estación Cliente para Videowall
Objetivo
Implementar en OpenBMS un módulo de telemetría de la estación cliente + benchmark automático de video, orientado especialmente al videowall.
Actualmente el sistema puede visualizar aproximadamente 26 streams simultáneos en baja resolución y, al maximizar una cámara, cambia al stream primario para obtener máxima calidad.
El objetivo de esta implementación es conocer de forma objetiva:
- qué hardware posee la estación cliente;
- qué capacidades de video ofrece el navegador;
- cuánto ancho de banda está consumiendo OpenBMS;
- cuánto cuesta decodificar/renderizar los streams;
- cuántos streams puede manejar esa estación de forma estable;
- cuándo conviene degradar automáticamente calidad/FPS;
- si una estación es apta para layouts de 16, 26, 32, 48 o más cámaras.
La solución debe diseñarse pensando en que, en el futuro, estos mismos conceptos puedan utilizarse en un cliente nativo OpenBMS Videowall para Windows.
1. Información de la estación cliente
Crear dentro de Información del sistema / Diagnóstico una sección llamada:
Estación cliente

Recolectar, siempre que el navegador/API lo permita:
Sistema
- Sistema operativo detectado.
- Arquitectura.
- Navegador.
- Versión del navegador.
- User Agent.
- Resolución de pantalla.
- Device Pixel Ratio.
- Cantidad de pantallas si existe API disponible.
- Resolución efectiva del viewport.
CPU
- Cantidad de logical processors (navigator.hardwareConcurrency).
- Información adicional disponible mediante APIs del navegador.
No inventar modelo de CPU si el navegador no lo expone.
Mostrar explícitamente:
CPU: 16 logical processors

cuando no pueda obtenerse el modelo real.
Memoria
Obtener memoria aproximada cuando esté disponible mediante navigator.deviceMemory u otra API segura.
Ejemplo:
Memoria reportada por navegador: 16 GB o más

No presentar estimaciones como valores exactos.
GPU / WebGL
Detectar:
- WebGL disponible.
- WebGL2 disponible.
- renderer;
- vendor;
- GPU identificable;
- límites relevantes de WebGL;
- contexto utilizado por OpenBMS.
Utilizar cuando corresponda:
- WEBGL_debug_renderer_info
- UNMASKED_VENDOR_WEBGL
- UNMASKED_RENDERER_WEBGL
La ausencia de estos datos no debe considerarse error.
2. Capacidades de decodificación de video
Implementar detección de codecs mediante APIs modernas del navegador, priorizando MediaCapabilities.
Evaluar como mínimo:
- H.264 / AVC
- H.265 / HEVC cuando esté disponible
- VP8
- VP9
- AV1
Para cada configuración evaluada determinar, cuando sea posible:
- supported
- smooth
- powerEfficient
Evaluar diferentes perfiles relevantes para CCTV, por ejemplo:
- 640×360 @ 10 FPS
- 640×360 @ 15 FPS
- 1280×720 @ 15 FPS
- 1920×1080 @ 15 FPS
- 1920×1080 @ 25/30 FPS
- 4K si corresponde
La UI debe distinguir entre:
- codec soportado;
- reproducción esperada como fluida;
- reproducción eficiente energéticamente.
No afirmar que existe hardware decoding únicamente porque el codec sea soportado.
3. Telemetría en tiempo real del Videowall
Agregar telemetría interna del reproductor.
Medir al menos:
- cantidad de cámaras visibles;
- streams conectados;
- streams reproduciendo;
- streams primarios activos;
- substreams activos;
- bitrate total recibido;
- bitrate estimado por cámara;
- FPS recibido;
- FPS efectivamente presentado;
- frames decodificados;
- frames descartados/dropped;
- errores de reproducción;
- reconexiones;
- latencia cuando pueda medirse;
- resolución efectiva de cada stream.
Cuando se utilice WebRTC, aprovechar RTCPeerConnection.getStats() para obtener estadísticas reales disponibles.
Cuando se utilicen otros transportes, instrumentar el pipeline correspondiente.
4. Medidor de ancho de banda
Agregar un medidor de tráfico específico de OpenBMS.
Debe mostrar:
Total
Recepción de video: 38.4 Mbps

Opcional por cámara
Ejemplo:
Cámara	Stream	Resolución	FPS	Bitrate
Acceso Norte	Sub	640×360	10	620 Kbps
Portón	Sub	640×360	10	540 Kbps
Calle 1	Main	1920×1080	25	4.8 Mbps


Agregar gráfica histórica corta configurable, inicialmente:
- últimos 60 segundos;
- Mbps totales;
- opcionalmente dropped frames.
No confundir tráfico total de la interfaz de red del equipo con tráfico generado por OpenBMS.
5. Salud del renderizado
Crear una métrica de salud del videowall.
Considerar:
- FPS de presentación;
- dropped frames;
- tiempo de frame;
- stalls;
- cantidad de streams;
- resolución;
- carga observada del pipeline;
- pérdida de frames sostenida.
Clasificar el estado en:
- Óptimo
- Aceptable
- Al límite
- Saturado
No utilizar únicamente el FPS global de WebGL como indicador del rendimiento de video.
Separar conceptualmente:
1. Red
2. Recepción/transporte
3. Decodificación
4. Render/composición WebGL
Esto es fundamental para identificar correctamente el cuello de botella.
6. Benchmark rápido de estación
Implementar un benchmark opcional que pueda ejecutarse desde:
Sistema → Estación cliente → Ejecutar benchmark

Duración objetivo:
aproximadamente 3–5 segundos para el test rápido.

El benchmark debe utilizar video real o muestras representativas del pipeline de OpenBMS, no únicamente operaciones gráficas sintéticas.
Probar progresivamente diferentes niveles de carga, por ejemplo:
- 4 streams
- 9 streams
- 16 streams
- 25 streams
- 32 streams
- 48 streams
Preferentemente utilizando el perfil típico de videowall:
- substream;
- baja resolución;
- FPS limitado;
- codec utilizado normalmente por las cámaras.
Durante la prueba medir:
- estabilidad;
- FPS;
- dropped frames;
- stalls;
- bitrate;
- tiempo de render;
- errores de decoder;
- comportamiento general del navegador.
El benchmark debe detenerse antes de producir una degradación severa o bloqueo de la UI.
7. Resultado del benchmark
Generar un resultado simple para el operador.
Ejemplo:
Capacidad estimada de esta estación
48 cámaras LQ: Óptimo
32 cámaras LQ: Óptimo
16 cámaras HQ: Aceptable
9 cámaras HQ: Óptimo

También guardar métricas técnicas detalladas para diagnóstico.
Crear un clientPerformanceScore interno normalizado.
No utilizar ese score como única fuente para tomar decisiones; conservar también las métricas originales.
8. Perfil automático
Al iniciar OpenBMS realizar un análisis muy liviano y prácticamente instantáneo.
Este análisis debe detectar:
- navegador;
- CPU lógica;
- memoria reportada;
- GPU/WebGL;
- codecs;
- resolución;
- capacidades conocidas;
- benchmark anterior almacenado.
Si existe un benchmark reciente para la misma combinación relevante de:
- navegador;
- versión;
- GPU;
- configuración gráfica;
- versión de OpenBMS;
reutilizarlo.
No ejecutar automáticamente un benchmark pesado en cada inicio.
9. Cache del benchmark
Guardar localmente los resultados.
Puede utilizarse IndexedDB.
Registrar:
- fecha;
- versión OpenBMS;
- navegador;
- GPU;
- resolución;
- codec probado;
- resultados;
- score;
- límites observados.
Invalidar o recomendar repetir benchmark cuando cambie de forma significativa:
- navegador;
- GPU;
- versión del navegador;
- versión importante de OpenBMS;
- configuración del pipeline de video.
10. Adaptación automática del Videowall
Utilizar la telemetría para preparar un sistema adaptativo.
Ejemplo:
Vista 48 cámaras
Preferir:
- substream;
- resolución reducida;
- FPS limitado;
- main stream desactivado.
Cámara maximizada
Cambiar automáticamente a:
- stream primario;
- resolución máxima disponible;
- FPS completo.
Al volver al grid:
- liberar inmediatamente el main stream;
- volver al substream.
Evitar mantener decoders/streams HQ innecesarios en segundo plano.
11. Degradación inteligente
Si la estación comienza a saturarse, degradar progresivamente.
Orden sugerido:
1. reducir FPS de streams secundarios cuando sea posible;
2. utilizar substream de menor resolución;
3. pausar streams completamente ocultos/no visibles;
4. reducir trabajo visual secundario;
5. limitar animaciones/overlays costosos;
6. advertir al usuario si el hardware no puede sostener el layout.
La degradación debe ser gradual y evitar cambios visuales bruscos.
Nunca degradar silenciosamente una cámara maximizada considerada prioritaria salvo situación crítica.
12. Diagnóstico del cuello de botella
El sistema debe intentar identificar si la limitación predominante parece provenir de:
Network bound
Indicadores posibles:
- bitrate requerido cercano/superior al disponible;
- pérdida/retransmisiones;
- buffering;
- latencia creciente.
Decode bound
Indicadores:
- dropped frames elevados;
- reproducción no fluida;
- render todavía con margen;
- problemas que aumentan al sumar streams aunque el ancho de banda siga disponible.
Render/GPU bound
Indicadores:
- aumento importante del frame time;
- composición WebGL incapaz de mantener la frecuencia objetivo;
- decoder funcionando pero presentación atrasada.
Mostrar algo similar a:
Cuello de botella probable: Decodificación de video

pero siempre marcarlo como estimación cuando el navegador no permita conocer métricas directas.
13. Panel de diagnóstico avanzado
Agregar una vista técnica expandible.
Ejemplo:
OpenBMS Client Diagnostics

Browser: Chrome 141
OS: Windows 11
CPU threads: 16
Memory reported: 16 GB
GPU/WebGL: NVIDIA GeForce RTX 3070
WebGL2: Yes

Video
H264: supported / smooth / power efficient
HEVC: supported
AV1: supported

Videowall
Visible cameras: 48
Active streams: 48
Main streams: 1
Substreams: 47
Traffic: 42.8 Mbps
Dropped frames: 0.7%
Render: 59 FPS

Performance state: OPTIMAL
Estimated bottleneck: none
14. Botón "Copiar diagnóstico"
Agregar:
Copiar diagnóstico

Debe generar texto o JSON fácilmente pegable en soporte.
Incluir:
- versión OpenBMS;
- navegador;
- SO;
- CPU lógica;
- memoria disponible/reportada;
- GPU;
- codecs;
- WebGL;
- cantidad de cámaras;
- estadísticas del videowall;
- resultado del benchmark;
- errores recientes relevantes.
No incluir:
- contraseñas;
- tokens;
- credenciales;
- URLs con secretos;
- información sensible innecesaria.
15. Modo desarrollador
Agregar un overlay opcional para desarrollo similar a:
48 streams
42.8 Mbps
Render 59 FPS
Dropped 0.7%
Main: 1
Sub: 47
State: OPTIMAL
Debe poder activarse/desactivarse sin afectar significativamente el rendimiento.
16. Arquitectura
No acoplar la lógica exclusivamente a React/UI.
Crear módulos independientes, por ejemplo:
ClientCapabilitiesService
VideoTelemetryService
ClientBenchmarkService
PerformanceClassifier
AdaptiveVideoPolicy
DiagnosticsExporter
El frontend únicamente consume estados/resultados.
Preparar interfaces para que en el futuro exista otra implementación para:
OpenBMS Native Videowall Client

17. Preparación para cliente nativo Windows
No desarrollar todavía el cliente nativo como parte de esta tarea, pero diseñar las estructuras de telemetría de manera reutilizable.
Un futuro cliente Windows podría utilizar:
- Media Foundation;
- Direct3D 11/12;
- DXVA;
- decodificación por hardware;
- render GPU directo;
- acceso real a CPU/GPU/VRAM;
- telemetría de adaptadores;
- soporte multi-monitor avanzado.
El formato de ClientCapabilities y VideoPerformanceMetrics debe poder representar tanto un navegador como un cliente nativo.
18. Métricas sugeridas
Definir estructuras similares a:
interface ClientCapabilities {
  platform: string;
  browser?: string;
  browserVersion?: string;
  logicalProcessors?: number;
  memoryGB?: number;
  gpuVendor?: string;
  gpuRenderer?: string;
  webgl: boolean;
  webgl2: boolean;
  codecs: CodecCapability[];
}

interface VideoPerformanceMetrics {
  timestamp: number;
  visibleCameras: number;
  activeStreams: number;
  mainStreams: number;
  subStreams: number;
  receivedMbps: number;
  decodedFrames: number;
  droppedFrames: number;
  droppedFrameRatio: number;
  presentationFps?: number;
  renderFrameTimeMs?: number;
  stalls?: number;
}
Adaptar estas interfaces a la arquitectura actual del proyecto; no copiar literalmente si existe un modelo mejor.
19. Performance
La propia telemetría no debe transformarse en un problema de rendimiento.
Requisitos:
- evitar polling agresivo;
- agregar estadísticas en intervalos razonables;
- no provocar re-render React por cada frame;
- utilizar buffers/ring buffers para históricos;
- separar estadísticas de alta frecuencia de la UI;
- actualizar visualmente el dashboard a una frecuencia moderada, por ejemplo 1 Hz;
- liberar listeners/resources al desmontar componentes.
20. Pruebas
Realizar pruebas al menos con layouts:
- 1
- 4
- 9
- 16
- 25/26
- 32
- 48 cámaras
Probar:
- todos substream;
- maximización de una cámara;
- cambio substream → main;
- retorno main → substream;
- múltiples cambios rápidos;
- pérdida de una cámara;
- reconexión;
- red limitada;
- CPU cliente limitada;
- GPU integrada;
- GPU dedicada;
- diferentes navegadores soportados.
Verificar especialmente que al maximizar/desmaximizar no queden streams primarios o decoders huérfanos.
21. Criterio de éxito inicial
El objetivo inmediato NO es garantizar arbitrariamente 48 streams en cualquier PC.
El objetivo es que OpenBMS pueda:
1. medir correctamente el comportamiento de 48 streams;
2. determinar dónde aparece el cuello de botella;
3. conocer las capacidades de la estación;
4. adaptar el videowall;
5. informar claramente al operador;
6. generar diagnóstico útil para soporte;
7. establecer una base objetiva para decidir posteriormente si se desarrolla el cliente nativo Windows.
Resultado esperado
Al finalizar esta implementación, OpenBMS debe poder responder técnicamente preguntas como:
¿Esta PC puede mostrar 48 cámaras?

¿Cuánto ancho de banda consume este videowall?

¿El problema está en la red, el decoder o WebGL?

¿Cuántos streams HQ puede manejar esta estación?

¿Qué GPU/navegador/codecs está utilizando el cliente?

¿Por qué esta PC reproduce 48 cámaras correctamente y otra no?

Todo esto debe integrarse manteniendo la UI limpia para el operador normal y ofreciendo información profunda en el modo de diagnóstico/administración.
