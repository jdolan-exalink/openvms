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
- Puerto de entrada para los usuarios: 8000 (HTTP) o 80/443 si se usa un dominio.


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


3. Copiar el código
-------------------

    mkdir -p /root/openvms
    # copiar el repositorio (git clone, scp o rsync) dentro de /root/openvms
    cd /root/openvms


4. Configuración (.env)
-----------------------

Crear /root/openvms/.env a partir de .env.example. Lo importante:

    # Clave maestra que cifra las contraseñas de Frigate y los secretos MFA.
    # GENERARLA UNA VEZ Y GUARDARLA: sin ella no se pueden leer las credenciales guardadas.
    OPENVMS_MASTER_KEY=<salida de: openssl rand -base64 32>
    POSTGRES_PASSWORD=<contraseña larga>
    WEB_PORT=8000
    # ":80" sirve HTTP. Con un dominio público (vms.ejemplo.com) Caddy obtiene TLS solo.
    SITE_ADDRESS=:80

    chmod 600 .env

Variables opcionales (con su valor por defecto):

    EVENT_SYNC_INTERVAL=5s    cada cuánto se leen eventos de cada Frigate
    EVENT_BACKFILL=24h        historia que se importa al registrar un Frigate
    HEALTH_INTERVAL=30s       chequeo de salud de cada Frigate
    SESSION_TTL=12h           duración máxima de una sesión web
    SESSION_IDLE=2h           cierre de sesión por inactividad
    LOGIN_MAX_FAILURES=5      intentos fallidos antes de bloquear la cuenta
    LOGIN_LOCKOUT=15m         duración del bloqueo


5. Levantar
-----------

    docker compose up -d --build
    docker compose ps          # todos "running" / "healthy"
    curl -s localhost:8000/health/ready

La primera compilación tarda varios minutos (Go y la web se compilan dentro de Docker;
el servidor no necesita Go ni Node instalados).

Servicios: postgres, valkey, nats, seaweedfs (almacenamiento S3 de miniaturas),
mosquitto, api, worker, web (Caddy) y dos Frigate simulados (frigate-helvecia y
frigate-cayasta) para demo. Los simulados se pueden apagar sin problema:

    docker compose stop frigate-helvecia frigate-cayasta


6. Primer administrador
-----------------------

    docker compose exec api /vmsctl bootstrap -password 'UnaClaveLarga-2026'

Crea el usuario "admin" de plataforma con todos los permisos, le pone esa contraseña
e imprime además un token de API. Entrar a http://<servidor>:8000 con admin y esa
contraseña, y activar la verificación en dos pasos en "Mi cuenta".

Otros comandos:

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
4. Usuarios y grupos: menú Usuarios / Grupos. Contraseñas con Argon2id (mínimo 10
   caracteres, letras y números o símbolos). Bloqueo tras 5 intentos fallidos.
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
