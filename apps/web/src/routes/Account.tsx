import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { api, unwrap } from "@/api/client";
import { meQuery } from "@/api/queries";
import { Button, ErrorNote, Field, PageHeader, TextInput } from "@/components/ui";

/** Account: change your password and set up the second factor (TOTP). */
export function Account() {
  const t = useT();
  const me = useQuery(meQuery);
  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6">
      <PageHeader title={t("nav.myAccount")} description={me.data ? `${me.data.display_name} (${me.data.username})` : undefined} />
      {me.data?.must_change_password && (
        <p role="status" className="rounded border border-warn/40 bg-warn/10 px-3 py-2 text-sm text-warn">{t("account.adminAskedPassword")}</p>
      )}
      <PasswordForm />
      <MfaSection enabled={!!me.data?.mfa_enabled} />
    </div>
  );
}

function PasswordForm() {
  const t = useT();
  const qc = useQueryClient();
  const [f, setF] = useState({ current: "", next: "", again: "" });
  const [done, setDone] = useState(false);
  const change = useMutation({
    mutationFn: async () => {
      if (f.next !== f.again) throw new Error(t("account.passwordMismatch"));
      return unwrap(await api.POST("/api/v1/me/password", { body: { current_password: f.current, new_password: f.next } }));
    },
    onSuccess: () => {
      setDone(true);
      setF({ current: "", next: "", again: "" });
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
  });
  return (
    <form
      className="flex flex-col gap-3 rounded border border-line bg-surface p-4"
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        setDone(false);
        change.mutate();
      }}
    >
      <h2 className="font-semibold">{t("auth.password")}</h2>
      <Field label={t("account.currentPassword")}>
        <TextInput type="password" autoComplete="current-password" value={f.current} onChange={(e) => setF({ ...f, current: e.target.value })} />
      </Field>
      <Field label={t("account.newPassword")} hint={t("account.passwordRules")}>
        <TextInput type="password" required minLength={10} autoComplete="new-password" value={f.next} onChange={(e) => setF({ ...f, next: e.target.value })} />
      </Field>
      <Field label={t("account.repeatPassword")}>
        <TextInput type="password" required minLength={10} autoComplete="new-password" value={f.again} onChange={(e) => setF({ ...f, again: e.target.value })} />
      </Field>
      <ErrorNote error={change.error} />
      {done && <p className="text-sm text-ok">{t("account.passwordUpdated")}</p>}
      <Button type="submit" variant="primary" disabled={change.isPending} className="self-start">
        {t("account.changePassword")}
      </Button>
    </form>
  );
}

function MfaSection({ enabled }: { enabled: boolean }) {
  const t = useT();
  const qc = useQueryClient();
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const setup = useMutation({ mutationFn: async () => unwrap(await api.POST("/api/v1/me/mfa/setup")) });
  const enable = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/me/mfa/enable", { body: { code: code.trim() } })),
    onSuccess: () => {
      setup.reset();
      setCode("");
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
  });
  const disable = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/me/mfa/disable", { body: { password } })),
    onSuccess: () => {
      setPassword("");
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
  });

  return (
    <section className="flex flex-col gap-3 rounded border border-line bg-surface p-4">
      <h2 className="font-semibold">{t("account.twoFactor")}</h2>
      {enabled ? (
        <>
          <p className="text-sm text-ok">{t("account.twoFactorOn")}</p>
          <form
            className="flex flex-wrap items-end gap-2"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              disable.mutate();
            }}
          >
            <Field label={t("account.passwordToDisable")}>
              <TextInput type="password" required value={password} onChange={(e) => setPassword(e.target.value)} />
            </Field>
            <Button type="submit" disabled={disable.isPending}>
              {t("account.disable")}
            </Button>
          </form>
          <ErrorNote error={disable.error} />
        </>
      ) : setup.data ? (
        <form
          className="flex flex-col gap-3"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            enable.mutate();
          }}
        >
          <p className="text-sm">
            {t("account.setupKey")}
          </p>
          <code className="rounded bg-raised px-3 py-2 font-mono text-sm tracking-widest break-all select-all">{setup.data.secret.match(/.{1,4}/g)?.join(" ")}</code>
          <a href={setup.data.otpauth_url} className="text-sm text-accent underline break-all">
            {setup.data.otpauth_url}
          </a>
          <Field label={t("account.appCode")}>
            <TextInput required inputMode="numeric" pattern="[0-9 ]{6,7}" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} />
          </Field>
          <ErrorNote error={enable.error} />
          <Button type="submit" variant="primary" disabled={enable.isPending} className="self-start">
            {t("account.enable")}
          </Button>
        </form>
      ) : (
        <>
          <p className="text-sm text-muted">{t("account.twoFactorHelp")}</p>
          <ErrorNote error={setup.error} />
          <Button variant="primary" onClick={() => setup.mutate()} disabled={setup.isPending} className="self-start">
            {t("account.setup")}
          </Button>
        </>
      )}
    </section>
  );
}
