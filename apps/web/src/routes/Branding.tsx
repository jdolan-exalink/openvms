import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type ChangeEvent, type FormEvent, useMemo, useState } from "react";
import { api, unwrap } from "@/api/client";
import { brandingQuery, meQuery } from "@/api/queries";
import { Button, ErrorNote, Field, PageHeader, Select, TextInput } from "@/components/ui";
import { DEFAULT_WATERMARK_TIMEZONE } from "@/lib/format";
import { can } from "@/lib/perm";

const MAX_LOGO_BYTES = 512 * 1024;

/**
 * timeZoneOptions (PDW-7) lists every IANA zone the runtime knows about, via the standard
 * Intl.supportedValuesOf API (no hardcoded/maintained list to fall out of date), always
 * including current and DEFAULT_WATERMARK_TIMEZONE even if the runtime's canonical enumeration omits
 * them: Intl.supportedValuesOf("timeZone") only lists ICU's *canonical* zone identifiers, not
 * every legacy alias — notably "America/Argentina/Buenos_Aires" itself is one such omitted
 * alias (ICU's canonical form is "America/Buenos_Aires"), even though it is a perfectly valid
 * IANA name that both Intl.DateTimeFormat and Go's time.LoadLocation accept directly. Without
 * this, a tenant whose branding.timezone is that alias (including every tenant that has not
 * configured one, per the DB column's own default) would see a <select> with no matching
 * <option>, and changing anything else would silently corrupt it to "". Falls back to just
 * current/DEFAULT_WATERMARK_TIMEZONE if the runtime predates Intl.supportedValuesOf entirely (Baseline
 * widely-available since 2023).
 */
function timeZoneOptions(current: string): string[] {
  const known = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];
  return Array.from(new Set([...known, DEFAULT_WATERMARK_TIMEZONE, current])).sort();
}

/** readAsBase64 returns the file's base64 payload (without the data: URL prefix). */
function readAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("No se pudo leer el archivo."));
    reader.onload = () => {
      const result = String(reader.result ?? "");
      resolve(result.slice(result.indexOf(",") + 1));
    };
    reader.readAsDataURL(file);
  });
}

/**
 * Branding: owner display name and logo burned into every plate detail photo/clip download
 * as a watermark, and shown as an on-screen overlay while viewing (PDW-1). Tenant-scoped;
 * changing it needs tenant.manage (the closest existing tenant-administration permission —
 * there is no dedicated "branding" permission in internal/authz/catalog.go).
 */
export function Branding() {
  const t = useT();
  const me = useQuery(meQuery);
  const tenantId = me.data?.tenant_id ?? "";
  const branding = useQuery(brandingQuery(tenantId));
  const qc = useQueryClient();
  const manage = can(me.data, "tenant.manage");

  const [ownerName, setOwnerName] = useState<string | null>(null);
  const [timezone, setTimezone] = useState<string | null>(null);
  const [logoFile, setLogoFile] = useState<File | null>(null);
  const [logoError, setLogoError] = useState<string | null>(null);
  const timeZoneList = useMemo(() => timeZoneOptions(branding.data?.timezone ?? DEFAULT_WATERMARK_TIMEZONE), [branding.data?.timezone]);

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: ["branding", tenantId] });
    setLogoFile(null);
  };

  const save = useMutation({
    mutationFn: async () => {
      const body: { owner_name?: string; timezone?: string; logo?: string; logo_content_type?: "image/png" | "image/jpeg"; remove_logo: boolean } = {
        remove_logo: false,
      };
      if (ownerName !== null) body.owner_name = ownerName;
      if (timezone !== null) body.timezone = timezone;
      if (logoFile) {
        body.logo = await readAsBase64(logoFile);
        body.logo_content_type = logoFile.type === "image/jpeg" ? "image/jpeg" : "image/png";
      }
      return unwrap(await api.PUT("/api/v1/tenants/{tenantId}/branding", { params: { path: { tenantId } }, body }));
    },
    onSuccess: () => {
      setOwnerName(null);
      setTimezone(null);
      invalidate();
    },
  });

  const removeLogo = useMutation({
    mutationFn: async () => unwrap(await api.PUT("/api/v1/tenants/{tenantId}/branding", { params: { path: { tenantId } }, body: { remove_logo: true } })),
    onSuccess: invalidate,
  });

  const onLogoChange = (e: ChangeEvent<HTMLInputElement>) => {
    setLogoError(null);
    const file = e.target.files?.[0] ?? null;
    if (!file) {
      setLogoFile(null);
      return;
    }
    if (file.type !== "image/png" && file.type !== "image/jpeg") {
      setLogoError("El logo debe ser PNG o JPEG.");
      e.target.value = "";
      return;
    }
    if (file.size > MAX_LOGO_BYTES) {
      setLogoError("El logo no puede superar 512 KB.");
      e.target.value = "";
      return;
    }
    setLogoFile(file);
  };

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6">
      <PageHeader
        title={t("nav.watermark")}
        description={t("settings.watermark")}
      />
      <ErrorNote error={branding.error} />
      {branding.data && (
        <form
          className="flex flex-col gap-4 rounded border border-line bg-surface p-4"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field label="Nombre del propietario" hint="Se muestra junto a la fecha y hora en la marca de agua.">
            <TextInput
              value={ownerName ?? branding.data.owner_name}
              maxLength={200}
              disabled={!manage}
              onChange={(e) => setOwnerName(e.target.value)}
              placeholder="Municipalidad de Helvecia"
            />
          </Field>
          <Field label="Zona horaria" hint="Se usa para calcular la hora local y el desfase horario de la marca de agua.">
            <Select value={timezone ?? branding.data.timezone} disabled={!manage} onChange={(e) => setTimezone(e.target.value)}>
              {timeZoneList.map((tz) => (
                <option key={tz} value={tz}>
                  {tz}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Logo (PNG o JPEG, hasta 512 KB)">
            <div className="flex items-center gap-3">
              {branding.data.has_logo && !logoFile && (
                <img src={`/api/v1/tenants/${tenantId}/branding/logo`} alt="Logo actual" className="h-12 w-12 rounded border border-line object-contain" />
              )}
              {manage && <input type="file" accept="image/png,image/jpeg" onChange={onLogoChange} className="text-sm" />}
            </div>
            {logoError && <p role="alert" className="text-sm text-bad">{logoError}</p>}
          </Field>
          {manage && (
            <div className="flex gap-2">
              <Button type="submit" variant="primary" disabled={save.isPending || (ownerName === null && timezone === null && !logoFile)}>
                Guardar
              </Button>
              {branding.data.has_logo && (
                <Button type="button" onClick={() => removeLogo.mutate()} disabled={removeLogo.isPending}>
                  Quitar logo
                </Button>
              )}
            </div>
          )}
          <ErrorNote error={save.error ?? removeLogo.error} />
        </form>
      )}
      {!manage && <p className="text-sm text-muted">Necesitás el permiso tenant.manage para modificar la marca de agua.</p>}
    </div>
  );
}
