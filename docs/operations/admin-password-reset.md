# Reset the Compose platform admin password

## Option 1: recovery code from the API log (browser)

Use this when you can read the API log of the server but cannot sign in:

1. On the login page choose **¿Olvidaste la contraseña?**, enter the platform admin username and choose **Generar código**.
2. On the server, read the code:

   ```bash
   docker compose logs api | grep recovery
   ```

   The API logs JSON, so the line looks like `{"level":"WARN","msg":"platform admin password recovery code","user":"admin","code":"482913","expires_in":"10m0s",...}`.
3. Enter the code with the new password twice and choose **Cambiar contraseña**. Then sign in with the new password.

Rules:

- Only active platform administrators can recover this way; tenant users are reset by an administrator from **Usuarios**.
- The answer is the same for any username, so the page never reveals whether an account exists.
- A code is valid for 10 minutes, one code per account is active at a time, and a new code can be requested once a minute. After 5 wrong codes the active code is invalidated.
- After 10 wrong codes in 24 hours the account cannot recover this way until those attempts are older than a day: no new code is issued (the log shows `platform admin password recovery blocked`) and even the right code is refused. Attempts refused by the block do not extend it. Use Option 2 below in that case.
- Both endpoints share a per-IP limit (a burst of 10, then one every 30 seconds) and answer `429` with `Retry-After` when exceeded. Behind a reverse proxy, set `TRUST_FORWARDED_FOR=true` so the limit applies per client instead of per proxy.
- A successful recovery clears the failed-login lock and closes the account's sessions. MFA is not changed.
- Every request, failure and recovery is audited (`PASSWORD_RECOVERY_REQUESTED`, `PASSWORD_RECOVERY_FAILED`, `PASSWORD_RECOVERED`); codes never reach the audit log.
- Set `PASSWORD_RECOVERY_LOG_CODES=false` in `.env` to disable it, for example when API logs are shipped to a third party. Both endpoints then answer `404 recovery_disabled`.

If the login "does nothing" after a reset, the browser is probably dropping the session cookie. The login page now says so explicitly; open the site through the same address the server uses (HTTPS when TLS is enabled).

## Option 2: reset script (terminal)

Use this only when an operator needs to regain access to the existing local Compose installation:

```bash
bash tools/reset_admin.sh
```

The script targets Compose project `openvms` and platform username `admin` by default. Set `RESET_ADMIN_PROJECT` or `OPENVMS_ADMIN_USERNAME` only when the installation was intentionally created with different values. Compose reads `.env` to resolve deployment settings, but the script never prints its values. Before prompting, it rebuilds only the API image from this checkout and resolves its image from the unfiltered `docker compose config --images` list (some Compose versions return no result for the service-filtered form). It does not restart or replace the running API service. The password is entered twice with terminal echo disabled, then streamed over stdin to a temporary container running the rebuilt image's `/vmsctl reset-admin-password` command. That container uses the running API service's database URL and default Compose network; only the temporary CLI container is removed afterward. Validation and Argon2id hashing run inside the image; the password is never placed in arguments or environment variables. The command rejects a missing, disabled, or tenant-scoped account. After a successful reset, the account must change the password at next login, active web sessions are revoked, and any failed-login lock is cleared.

This operation never runs automatically at startup and does not remove or restart existing service containers, volumes, or data. Run it from the repository root with Docker Compose and a running API service; host-side Go is not required. Do not place the password in shell arguments, environment variables, or command history.
