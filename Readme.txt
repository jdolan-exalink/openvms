OpenVMS · Federated Frigate VMS
Guía de instalación y despliegue
================================

Este archivo registra todo lo necesario para instalar OpenVMS en un servidor nuevo
(probado en un contenedor LXC de Proxmox con Debian) y lo que se instaló en el
servidor de pruebas 10.1.1.126. La documentación de desarrollo está en README.md.


1. Requisitos del servidor
--------------------------

- Linux x86_64 (Debian 12/13 o Ubuntu 22.04+). 2 CPU y 4 GB de RAM alcanzan para
  decenas de cámaras; el VMS no graba video (cada Frigate graba el suyo).
- Docker Engine 24+ con el plugin Compose v2 (`docker compose version`).
- Si es un LXC de Proxmox: activar "nesting" y "keyctl" en el contenedor
  (Opciones > Características), si no Docker no arranca.
- Acceso de red desde el VMS a cada Frigate:
    * puerto 8971 (con login) o 5000 (sin login, solo redes de confianza),
    * el VMS pide /api, /live/mse (video en vivo), /vod (grabaciones) y /exports.
- Puerto de entrada para los usuarios: 8000 (HTTP), 8443 (HTTPS por IP con certificado
  local, ver paso 5) o 80/443 si se usa un dominio.


2. Instalar Docker (Debian)
---------------------------

    apt-get update
    apt-get install -y ca-certificates curl git
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
      https://download.docker.com/linux/debian $(. /etc/os-release && echo $VERSION_CODENAME) stable" \
      > /etc/apt/sources.list.d/docker.list
    apt-get update
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    systemctl enable --now docker

Ubuntu 24.04 puede usar los paquetes de Ubuntu en lugar del repositorio de Docker:

    apt-get update
    apt-get install -y docker.io docker-compose-v2 docker-buildx git
    systemctl enable --now docker
    docker compose version     # debe mostrar v2.x

Importante: el comando es `docker compose` (con espacio, Compose v2). El paquete
`docker-compose` (con guion, Compose v1) NO sirve: rechaza docker-compose.yml con
"'name' does not match any of the regexes" y "unsupported option: 'create_host_path'".


3. Copiar el código
-------------------

    git clone https://github.com/jdolan-exalink/openvms.git /root/openvms
    cd /root/openvms
    git tag --sort=-creatordate | head -3   # versiones publicadas, la más nueva primero
    git checkout <versión>     # por ejemplo v0.3.0; instalar una versión etiquetada, no main


4. Configuración (.env)
-----------------------

Primera instalación en un paso: `make install` crea .env desde .env.example con
OPENVMS_MASTER_KEY y POSTGRES_PASSWORD aleatorios (si .env ya existe no lo toca),
levanta el stack y espera /health/ready. Guardar la OPENVMS_MASTER_KEY generada.
Para hacerlo a mano, crear /root/openvms/.env a partir de .env.example. Lo importante:

    # Clave maestra que cifra las contraseñas de Frigate y los secretos MFA.
    # GENERARLA UNA VEZ Y GUARDARLA: sin ella no se pueden leer las credenciales guardadas.
    OPENVMS_MASTER_KEY=<salida de: openssl rand -base64 32>
    POSTGRES_PASSWORD=<contraseña larga>
    WEB_PORT=8000
    # ":80" sirve HTTP. Con un dominio público (vms.ejemplo.com) Caddy obtiene TLS solo.
    SITE_ADDRESS=:80
    # IP del servidor para HTTPS en el puerto 8443. OBLIGATORIA para usar HTTPS por IP: tiene que
    # ser la MISMA <IP del servidor> que se pasa al script del certificado en el paso 5. Si no se
    # define vale 10.1.1.24 (la IP de la instalación de desarrollo) y HTTPS no responde en este servidor.
    OPENVMS_HTTPS_SITE_IPV4=<IP del servidor>

    chmod 600 .env

Variables opcionales (con su valor por defecto):

    EVENT_SYNC_INTERVAL=5s    cada cuánto se leen eventos de cada Frigate
    EVENT_BACKFILL=24h        historia que se importa al registrar un Frigate
    HEALTH_INTERVAL=30s       chequeo de salud de cada Frigate
    SESSION_TTL=12h           duración máxima de una sesión web
    SESSION_IDLE=2h           cierre de sesión por inactividad
    LOGIN_MAX_FAILURES=5      intentos fallidos antes de bloquear la cuenta (0 desactiva el bloqueo)
    LOGIN_LOCKOUT=15m         duración del bloqueo
    OPENVMS_ADMIN_USERNAME=admin        usuario del primer administrador (ver sección 6)
    OPENVMS_ADMIN_PASSWORD=             contraseña inicial; vacía = se genera una al azar (ver sección 6)
    OPENVMS_DEFAULT_TENANT=openvms      organización inicial (ver sección 6)
    OPENVMS_DEFAULT_SITE="Sitio principal"   sitio inicial dentro de esa organización
    OPENVMS_DEFAULT_SITE_TZ=America/Argentina/Cordoba   zona horaria de ese sitio

Desactivar el bloqueo por intentos fallidos reduce la protección contra adivinación de
contraseñas en línea. Si se configura `LOGIN_MAX_FAILURES=0`, conserve límites de intentos
en el proxy o la capa de ingreso (rate limiting).


5. Levantar
-----------

Antes del primer arranque, generar el certificado HTTPS local (autofirmado, válido 365
días) para la IP del servidor. El contenedor web lo monta y sin él no arranca
("bind source path does not exist: .../deploy/docker/local-tls/tls.crt"):

    sh deploy/docker/bootstrap-local-tls.sh <IP del servidor> deploy/docker/local-tls

Ese comando también crea el certificado dedicado del canal gRPC del agente en
`deploy/docker/local-tls/agent-grpc`, antes de que Compose monte esos archivos.

El comando se puede repetir en instalaciones existentes: valida y conserva el par HTTPS,
y crea el par gRPC dedicado si falta. En los destinos de archivo del certificado/clave gRPC
solo elimina directorios vacíos (posibles restos de un bind mount anterior); si contienen
archivos, falla sin borrarlos. Compose no crea automáticamente esos destinos de archivo.
El navegador pide aceptar el certificado la primera vez que se entra por https://IP:8443;
por http://IP:8000 no hace falta. Para ver cuándo vence:

    openssl x509 -enddate -noout -in deploy/docker/local-tls/tls.crt

Para renovar HTTPS (antes de que venza, o si cambia la IP), conserva los archivos actuales,
genera el nuevo par en otro directorio y actualiza `OPENVMS_WEB_TLS_CERT_HOST_FILE`,
`OPENVMS_WEB_TLS_KEY_HOST_FILE` y `OPENVMS_AGENT_API_CA_HOST_FILE` antes de recrear web/api.
El bootstrap nunca reemplaza material existente.

    docker compose up -d --build
    docker compose ps          # todos "running" / "healthy"
    curl -s localhost:8000/health/ready

La primera compilación tarda varios minutos (Go y la web se compilan dentro de Docker;
el servidor no necesita Go ni Node instalados).

Servicios: postgres, valkey, nats, seaweedfs (almacenamiento S3 de miniaturas),
api, worker y web (Caddy). No incluye datos de demostración. Mosquitto (MQTT) es
opcional y OpenVMS no lo necesita: para activarlo agregue COMPOSE_PROFILES=mqtt al .env
o use `docker compose --profile mqtt up -d`. Si actualiza desde una versión anterior,
quite el contenedor viejo con `docker compose rm -sf mosquitto`.

Los Frigate simulados (frigate-helvecia y frigate-cayasta) son solo para desarrollo:
están en docker-compose.dev.yml y se levantan con `make dev`.


6. Primer administrador
-----------------------

En una instalación nueva, la API crea sola el administrador de plataforma al arrancar:

    usuario:     admin                  (OPENVMS_ADMIN_USERNAME)
    contraseña:  al azar, se muestra una sola vez en el log de la API

No hay contraseña por defecto. Para ver la generada (solo aparece en el primer arranque):

    docker compose logs api | grep "initial admin"

Entrar con ese usuario y esa contraseña; el sistema obliga a cambiarla en el primer ingreso.
Guardarla enseguida: no se vuelve a registrar.

Para elegir la contraseña en vez de generarla, definir en .env antes de levantar:

    OPENVMS_ADMIN_PASSWORD=<clave de al menos 8 caracteres>

Notas:
- Solo se crea si la instalación no tiene ningún usuario de plataforma (ni siquiera borrado):
  nunca pisa ni reinicia una instalación existente ni agrega un admin al actualizar.
- Si OPENVMS_ADMIN_PASSWORD tiene menos de 8 caracteres, la API crea el admin con una clave
  aleatoria, lo avisa en el log y obliga a cambiarla en el primer ingreso.
- Después, activar la verificación en dos pasos en "Mi cuenta".

Organización y sitio iniciales: si la instalación no tiene ninguna organización, la API crea
también una (OPENVMS_DEFAULT_TENANT, por defecto "openvms") con un sitio (OPENVMS_DEFAULT_SITE,
por defecto "Sitio principal"), para poder registrar servidores sin pasos previos. Si ya existe
alguna organización (aunque esté borrada) no hace nada: nunca crea, renombra ni restaura. Los
nombres se pueden cambiar después desde la web y el cambio se respeta en los reinicios.
La zona horaria del sitio es OPENVMS_DEFAULT_SITE_TZ (por defecto America/Argentina/Cordoba;
si no es válida se registra un error y se usa la de por defecto). Si esta creación inicial
falla, la API arranca igual y lo registra en el log.

Alternativa manual (instalaciones con contraseña propia o para crear el admin con otro nombre):

    docker compose exec api /vmsctl bootstrap -password 'UnaClaveLarga-2026'

Crea el usuario "admin" de plataforma con todos los permisos, le pone esa contraseña
e imprime además un token de API.

Otros comandos:

    bash tools/reset_admin.sh                                # recuperar acceso del admin de plataforma (ver docs/operations/admin-password-reset.md)
    docker compose exec api /vmsctl passwd -user admin -password 'Otra-Clave-2026'
    docker compose exec api /vmsctl token -user admin        # token para scripts
    docker compose exec api /vmsctl seed-demo                # organización demo con los Frigate simulados


7. Cargar la primera instalación (desde la web)
-----------------------------------------------

1. Organización: la crea un administrador de plataforma (API POST /api/v1/tenants o
   `vmsctl seed-demo` para la demo).
2. Sitios: menú Sitios > nuevo sitio.
3. Servidores: menú Servidores > Registrar servidor.
     - "Con usuario y contraseña": https://IP:8971 y un usuario de Frigate. La contraseña
       se guarda cifrada con AES-256-GCM y nunca vuelve a mostrarse.
     - "Sin login": http://IP:5000. Solo en redes privadas o VPN: quien llega a ese puerto
       controla Frigate.
   "Probar conexión" muestra versión y cámaras; "Registrar" las importa.
4. Usuarios y grupos: menú Usuarios / Grupos. Contraseñas con Argon2id (mínimo 8
   caracteres, sin otros requisitos). Bloqueo tras 5 intentos fallidos.
5. Permisos: menú Permisos. Elegir usuario o grupo, un perfil (Operador, Solo en vivo,
   Investigador, Administrador) o un permiso suelto, y el alcance (organización, sitio,
   servidor, grupo de cámaras o cámara). Denegar siempre gana sobre permitir.

Los eventos empiezan a aparecer en Eventos a los pocos segundos de registrar un servidor
(se importan las últimas 24 h). La columna "Eventos" en Servidores muestra el estado de
la sincronización de cada Frigate.


8. Seguridad
------------

- Guardar OPENVMS_MASTER_KEY fuera del servidor (gestor de contraseñas). Si se pierde,
  hay que volver a cargar las contraseñas de Frigate y los usuarios deben reconfigurar MFA.
- Con dominio público: SITE_ADDRESS=vms.ejemplo.com y abrir 80/443; Caddy renueva el
  certificado solo. Las cookies de sesión se marcan Secure automáticamente bajo HTTPS.
- Postgres, NATS, Valkey y SeaweedFS solo escuchan en 127.0.0.1 del servidor.
- La auditoría (menú Auditoría) registra ingresos, fallos, cambios, vistas en vivo,
  reproducciones y descargas, y no se puede modificar desde la aplicación.


9. Actualizar
-------------

    cd /root/openvms
    # traer el código nuevo
    docker compose up -d --build

Las migraciones de base de datos se aplican solas al arrancar la API.


10. Backup
----------

    docker compose exec postgres pg_dump -U openvms openvms | gzip > openvms-$(date +%F).sql.gz

Guardar también el archivo .env (contiene la clave maestra). Las grabaciones no se
respaldan acá: están en cada Frigate.


11. Diagnóstico
---------------

    docker compose logs -f api worker
    docker compose exec api /vmsctl migrate
    curl -s localhost:8000/health/ready | jq


12. Registro del servidor de pruebas (10.1.1.126, /root/openvms)
-----------------------------------------------------------------

Instalado y configurado para las pruebas del MVP:

- (se completa durante el despliegue)
