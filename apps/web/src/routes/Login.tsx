import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { clearToken, setToken } from "@/api/auth";
import { api, unwrap } from "@/api/client";
import { meQuery } from "@/api/queries";
import { AccountMenu } from "@/components/AccountMenu";
import { AppVersion } from "@/components/AppVersion";
import { Button, ErrorNote, Field, TextInput } from "@/components/ui";
import { brandIcon as Brand } from "@/components/nav";
import { useT } from "@/i18n";

export function Login() {
  const t = useT();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<"password" | "token" | "recover">("password");
  const [notice, setNotice] = useState<string | null>(null);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [totp, setTotp] = useState("");
  const [needTotp, setNeedTotp] = useState(false);
  const [token, setTokenValue] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  async function enter() {
    queryClient.clear();
    // A login can succeed while the browser drops the session cookie (wrong scheme or host);
    // without this check the app guard bounces back here with no explanation.
    try {
      await queryClient.fetchQuery(meQuery);
    } catch {
      throw new Error(t("auth.sessionNotKept"));
    }
    await navigate({ to: "/" });
  }

  async function submitPassword(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setNotice(null);
    clearToken();
    try {
      const res = unwrap(
        await api.POST("/api/v1/auth/login", {
          body: { username: username.trim(), password, totp_code: needTotp ? totp.trim() : undefined },
        }),
      );
      if (res.mfa_required) {
        setNeedTotp(true);
        return;
      }
      await enter();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  async function submitToken(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setToken(token.trim());
    try {
      unwrap(await api.GET("/api/v1/me"));
      await enter();
    } catch {
      clearToken();
      setError(new Error(t("auth.tokenInvalid")));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="relative flex min-h-dvh items-center justify-center bg-surface-dim px-4 py-16">
      <div className="absolute right-4 top-4">
        <AccountMenu />
      </div>
      <div className="flex w-full max-w-sm flex-col gap-5 rounded-m3-2xl bg-surface-1 p-6 sm:p-8">
        <div className="flex items-center gap-3">
          <span className="flex size-12 shrink-0 items-center justify-center rounded-m3-lg bg-primary text-on-primary">
            <Brand className="size-6" aria-hidden />
          </span>
          <h1 className="text-3xl font-extrabold tracking-tight">OpenVMS</h1>
        </div>
        {mode === "recover" ? (
          <RecoveryForm
            initialUsername={username}
            onDone={(name) => {
              setUsername(name);
              setPassword("");
              setError(null);
              setNotice(t("auth.passwordRecovered"));
              setMode("password");
            }}
            onCancel={() => {
              setError(null);
              setMode("password");
            }}
          />
        ) : mode === "password" ? (
          <form onSubmit={submitPassword} className="flex flex-col gap-4">
            {notice && <p role="status" className="rounded-m3-md bg-primary/10 px-3 py-2 text-sm text-primary">{notice}</p>}
            <Field label={t("auth.username")}>
              <TextInput required autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} disabled={needTotp} />
            </Field>
            <Field label={t("auth.password")}>
              <TextInput
                required
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                disabled={needTotp}
              />
            </Field>
            {needTotp && (
              <Field label={t("auth.verificationCode")} hint={t("auth.verificationHint")}>
                <TextInput
                  required
                  autoFocus
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  pattern="[0-9 ]{6,7}"
                  value={totp}
                  onChange={(e) => setTotp(e.target.value)}
                />
              </Field>
            )}
            <ErrorNote error={error} />
            <Button type="submit" variant="filled" className="w-full" disabled={busy}>
              {busy ? t("auth.verifying") : needTotp ? t("auth.verify") : t("auth.signIn")}
            </Button>
            <Button
              variant="text"
              className="self-start"
              onClick={() => {
                setError(null);
                setNotice(null);
                setNeedTotp(false);
                setMode("recover");
              }}
            >
              {t("auth.forgotPassword")}
            </Button>
          </form>
        ) : (
          <form onSubmit={submitToken} className="flex flex-col gap-4">
            <Field label={t("auth.accessToken")} hint={t("auth.tokenHint")}>
              <TextInput type="password" autoComplete="off" required value={token} onChange={(e) => setTokenValue(e.target.value)} placeholder="ovms_…" />
            </Field>
            <ErrorNote error={error} />
            <Button type="submit" variant="filled" className="w-full" disabled={busy || !token.trim()}>
              {busy ? t("auth.verifying") : t("auth.signIn")}
            </Button>
          </form>
        )}
        {mode !== "recover" && (
          <Button
            variant="text"
            className="self-start"
            onClick={() => {
              setError(null);
              setNeedTotp(false);
              setMode(mode === "password" ? "token" : "password");
            }}
          >
            {mode === "password" ? t("auth.signInToken") : t("auth.signInPassword")}
          </Button>
        )}
        <AppVersion className="text-center" />
      </div>
    </main>
  );
}

/**
 * Platform admin recovery: ask for a code (written to the API log), then send it with the new
 * password. The request step answers the same for any username.
 */
function RecoveryForm({ initialUsername, onDone, onCancel }: { initialUsername: string; onDone: (username: string) => void; onCancel: () => void }) {
  const t = useT();
  const [username, setUsername] = useState(initialUsername);
  const [requested, setRequested] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  async function requestCode(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = unwrap(await api.POST("/api/v1/auth/recovery", { body: { username: username.trim() } }));
      setRequested(res.message);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  async function confirm(e: FormEvent) {
    e.preventDefault();
    setError(null);
    if (password !== confirmation) {
      setError(new Error(t("auth.passwordsDiffer")));
      return;
    }
    setBusy(true);
    try {
      unwrap(await api.POST("/api/v1/auth/recovery/confirm", { body: { username: username.trim(), code: code.trim(), new_password: password } }));
      onDone(username.trim());
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-on-surface-variant">{t("auth.recoveryIntro")}</p>
      {requested === null ? (
        <form onSubmit={requestCode} className="flex flex-col gap-4">
          <Field label={t("auth.username")}>
            <TextInput required autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} />
          </Field>
          <ErrorNote error={error} />
          <Button type="submit" variant="filled" className="w-full" disabled={busy || !username.trim()}>
            {busy ? t("auth.verifying") : t("auth.requestCode")}
          </Button>
        </form>
      ) : (
        <form onSubmit={confirm} className="flex flex-col gap-4">
          <p role="status" className="rounded-m3-md bg-primary/10 px-3 py-2 text-sm text-primary">{requested}</p>
          <Field label={t("auth.recoveryCode")} hint={t("auth.recoveryCodeHint")}>
            <TextInput
              required
              autoFocus
              inputMode="numeric"
              autoComplete="one-time-code"
              pattern="[0-9]{6}"
              maxLength={6}
              value={code}
              onChange={(e) => setCode(e.target.value)}
            />
          </Field>
          <Field label={t("auth.newPassword")}>
            <TextInput required type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Field label={t("auth.repeatPassword")}>
            <TextInput required type="password" autoComplete="new-password" value={confirmation} onChange={(e) => setConfirmation(e.target.value)} />
          </Field>
          <ErrorNote error={error} />
          <Button type="submit" variant="filled" className="w-full" disabled={busy}>
            {busy ? t("auth.verifying") : t("auth.changePassword")}
          </Button>
        </form>
      )}
      <Button variant="text" className="self-start" onClick={onCancel}>
        {t("auth.backToSignIn")}
      </Button>
    </div>
  );
}
