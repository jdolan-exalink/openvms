import { AppWindow, LayoutGrid, Palette, Share } from "lucide-react";
import { useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import type { LucideIcon } from "lucide-react";
import { useT } from "@/i18n";
import { useFocusTrap } from "@/lib/useFocusTrap";
import { rememberInstallDismissal } from "@/lib/pwa/useInstallSheet";
import { useInstallPrompt } from "@/lib/pwa/useInstallPrompt";
import { Button } from "./ui";
import { Icon } from "./Icon";

function Benefit({ icon, children }: { icon: LucideIcon; children: ReactNode }) {
  return (
    <li className="flex items-center gap-3">
      <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-secondary-container text-on-secondary-container">
        <Icon icon={icon} size="sm" />
      </span>
      <span className="text-sm text-on-surface">{children}</span>
    </li>
  );
}

/**
 * InstallSheet is the "add to home screen" bottom sheet. Android/desktop Chromium installs through
 * the captured browser prompt; iOS has no such API, so it shows how to use Safari's Share menu.
 */
export function InstallSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  if (!open) return null;
  return <Sheet onClose={onClose} />;
}

function Sheet({ onClose }: { onClose: () => void }) {
  const t = useT();
  const { canInstall, promptInstall, isIOS, isStandalone } = useInstallPrompt();
  const ref = useRef<HTMLDivElement>(null);
  const showIosHint = isIOS && !isStandalone;

  const close = () => {
    rememberInstallDismissal();
    onClose();
  };
  useFocusTrap(ref, close);

  const install = async () => {
    await promptInstall();
    close();
  };

  return createPortal(
    <div
      className="fixed inset-0 z-[90] flex items-end justify-center bg-scrim md:items-center md:p-4"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={t("common.installTitle")}
        className="flex w-full max-w-md flex-col gap-5 rounded-t-m3-2xl bg-surface-1 p-6 pb-[max(1.5rem,env(safe-area-inset-bottom))] shadow-lg md:rounded-m3-2xl"
      >
        <div className="flex items-center gap-4">
          <img src="/icon.svg" alt="" width={56} height={56} className="size-14 shrink-0 rounded-m3-lg" />
          <h2 className="text-[22px] font-bold text-on-surface">{t("common.installTitle")}</h2>
        </div>
        <ul className="flex flex-col gap-3">
          <Benefit icon={AppWindow}>{t("common.installBenefitFullscreen")}</Benefit>
          <Benefit icon={LayoutGrid}>{t("common.installBenefitIcon")}</Benefit>
          <Benefit icon={Palette}>{t("common.installBenefitTheme")}</Benefit>
        </ul>
        {showIosHint && (
          <p className="flex items-start gap-3 rounded-m3-lg bg-surface-2 p-3 text-sm text-on-surface-variant">
            <Icon icon={Share} size="sm" className="mt-0.5 shrink-0" />
            <span>{t("common.installIosHint")}</span>
          </p>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="text" onClick={close}>
            {t("common.installLater")}
          </Button>
          {!isIOS && canInstall && (
            <Button variant="filled" onClick={() => void install()}>
              {t("common.installNow")}
            </Button>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
