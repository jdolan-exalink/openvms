# Reset the Compose platform admin password

Use this only when an operator needs to regain access to the existing local Compose installation:

```bash
bash tools/reset_admin.sh
```

The script targets Compose project `openvms` and platform username `admin` by default. Set `RESET_ADMIN_PROJECT` or `OPENVMS_ADMIN_USERNAME` only when the installation was intentionally created with different values. Before prompting, it rebuilds only the API image from this checkout and resolves its image from the unfiltered `docker compose config --images` list (some Compose versions return no result for the service-filtered form). It does not restart or replace the running API service. The password is entered twice with terminal echo disabled, then streamed over stdin to a temporary container running the rebuilt image's `/vmsctl reset-admin-password` command. That container uses the running API service's database URL and default Compose network without reading or printing `.env`; only the temporary CLI container is removed afterward. Validation and Argon2id hashing run inside the image; the password is never placed in arguments or environment variables. The command rejects a missing, disabled, or tenant-scoped account. After a successful reset, the account must change the password at next login, active web sessions are revoked, and any failed-login lock is cleared.

This operation never runs automatically at startup and does not remove or restart existing service containers, volumes, or data. Run it from the repository root with Docker Compose and a running API service; host-side Go is not required. Do not place the password in shell arguments, environment variables, or command history.
