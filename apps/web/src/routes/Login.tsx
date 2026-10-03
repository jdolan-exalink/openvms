import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { clearToken, setToken } from "@/api/auth";
import { api, unwrap } from "@/api/client";
import { AccountMenu } from "@/components/AccountMenu";
import { Button, ErrorNote, Field, TextInput } from "@/components/ui";
import { brandIcon as Brand } from "@/components/nav";
import { useT } from "@/i18n";

export function Login() {
  const t = useT();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<"password" | "token">("password");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [totp, setTotp] = useState("");
  const [needTotp, setNeedTotp] = useState(false);
  const [token, setTokenValue] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  async function enter() {
    queryClient.clear();
    await navigate({ to: "/" });
  }

  async function submitPassword(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
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
    <main className="relative flex min-h-dvh items-center justify-center px-4">
      <div className="absolute right-4 top-4">
        <AccountMenu />
      </div>
      <div className="flex w-full max-w-sm flex-col gap-4 rounded border border-line bg-surface p-6">
        <div className="flex items-center gap-2">
          <Brand className="size-5 text-accent" aria-hidden />
          <h1 className="text-lg font-semibold tracking-tight">OpenVMS</h1>
        </div>
        {mode === "password" ? (
          <form onSubmit={submitPassword} className="flex flex-col gap-4">
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
            <Button type="submit" variant="primary" disabled={busy}>
              {busy ? t("auth.verifying") : needTotp ? t("auth.verify") : t("auth.signIn")}
            </Button>
          </form>
        ) : (
          <form onSubmit={submitToken} className="flex flex-col gap-4">
            <Field label={t("auth.accessToken")} hint={t("auth.tokenHint")}>
              <TextInput type="password" autoComplete="off" required value={token} onChange={(e) => setTokenValue(e.target.value)} placeholder="ovms_…" />
            </Field>
            <ErrorNote error={error} />
            <Button type="submit" variant="primary" disabled={busy || !token.trim()}>
              {busy ? t("auth.verifying") : t("auth.signIn")}
            </Button>
          </form>
        )}
        <button
          type="button"
          className="self-start text-xs text-muted underline-offset-2 hover:text-ink hover:underline"
          onClick={() => {
            setError(null);
            setNeedTotp(false);
            setMode(mode === "password" ? "token" : "password");
          }}
        >
          {mode === "password" ? t("auth.signInToken") : t("auth.signInPassword")}
        </button>
      </div>
    </main>
  );
}
