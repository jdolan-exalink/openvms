# Weekly Alpine package refresh of the web image

The web image is built on `caddy:2-alpine`. Its `apk upgrade` layer is cached, both by the local Docker
cache and by the CI layer cache, so a library fix published after the last build (for example a `curl`
CVE that the CI image scan fails on) would stay in the running container. Two things keep it current:

1. **Weekly refresh on the host (this document).** Every Sunday at 23:59 (host time zone) a systemd timer
   upgrades the Alpine packages of the web image that is running, restarts only the web container and logs
   what changed.
2. **Fresh layer on every build.** `deploy/docker/web.Dockerfile` takes `APK_REFRESH`: `make deploy` passes
   the build time and CI passes the day, so the `apk upgrade` layer is rebuilt instead of reused.

## What the refresh does

`deploy/maintenance/web-apk-refresh.sh`, installed as `/usr/local/sbin/openvms-web-apk-refresh`:

1. Skips (exit 0, logged) when another refresh or a `make deploy` is running, when the web container is not
   running, or when the running image has no `apk`.
2. Lists the installed packages of the running image.
3. Builds one extra layer on top of that same image: `RUN apk upgrade --no-cache`, with `--no-cache`.
   It does not use the repository checkout, so it works whichever branch is checked out.
4. Lists the packages again. With no difference it logs "no package updates" and leaves the container alone.
5. Otherwise it tags the running image `openvms-web:rollback-<UTC timestamp>-apk`, points
   `openvms-web:latest` at the refreshed image and recreates only the web container with
   `docker compose up -d --no-build --no-deps --wait`.
6. If the new container does not become ready, `latest` goes back to the rollback tag and the container is
   recreated again (exit 1, logged). If that also fails the log says how to restore by hand
   (`make rollback TAG=rollback-...-apk`).
7. Keeps the newest 5 `rollback-*-apk` tags (`APK_REFRESH_KEEP`).

The API, relay and worker images are not Alpine (distroless or Debian) and are not touched.

## The log

`/var/log/openvms/web-apk-refresh.log`, one line per event with a UTC timestamp, rotated weekly and kept 26
weeks (`/etc/logrotate.d/openvms-web-apk-refresh`). The journal keeps a copy:
`journalctl -u openvms-web-apk-refresh`.

```text
2026-11-01T23:59:02Z == web apk refresh 20261101T235902Z (dry-run=0)
2026-11-01T23:59:03Z running image sha256:…
2026-11-01T23:59:05Z packages installed before: 62
2026-11-01T23:59:05Z building the upgrade layer without cache (openvms-web:apk-20261101T235902Z)
    apk: (1/2) Upgrading libcurl (8.22.0-r0 -> 8.23.0-r0)
    apk: (2/2) Upgrading curl (8.22.0-r0 -> 8.23.0-r0)
2026-11-01T23:59:19Z packages updated: 2
    curl: 8.22.0-r0 -> 8.23.0-r0
    libcurl: 8.22.0-r0 -> 8.23.0-r0
2026-11-01T23:59:19Z swapping the web container (rollback tag openvms-web:rollback-20261101T235902Z-apk)
2026-11-01T23:59:31Z the web container is ready on the refreshed image
2026-11-01T23:59:31Z == done: 2 packages updated, rollback tag rollback-20261101T235902Z-apk
```

## Install, run by hand, remove

```bash
sudo bash deploy/maintenance/install-web-apk-refresh.sh        # script, service, timer, logrotate
systemctl list-timers openvms-web-apk-refresh.timer             # next run
sudo APK_REFRESH_DRY_RUN=1 /usr/local/sbin/openvms-web-apk-refresh   # report only, container untouched
sudo systemctl start openvms-web-apk-refresh.service            # run now
sudo bash deploy/maintenance/install-web-apk-refresh.sh --uninstall
```

`Persistent=true`: if the host was off at Sunday 23:59 the refresh runs at the next boot. To change the
schedule edit `OnCalendar=` in `/etc/systemd/system/openvms-web-apk-refresh.timer` and run
`systemctl daemon-reload`. Tests: `bash deploy/maintenance/web_apk_refresh_test.sh`.

A `make deploy` rebuilds the web image from the Dockerfile (with a fresh `apk upgrade` layer), so it
supersedes a refreshed image; the next weekly run starts from whatever is running then.
