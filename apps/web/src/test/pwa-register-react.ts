// Vitest alias for `virtual:pwa-register/react`, which only exists inside the PWA build plugin.
type Setter = (value: boolean) => void;

export function useRegisterSW(): {
  needRefresh: [boolean, Setter];
  offlineReady: [boolean, Setter];
  updateServiceWorker: (reloadPage?: boolean) => Promise<void>;
} {
  return { needRefresh: [false, () => {}], offlineReady: [false, () => {}], updateServiceWorker: async () => {} };
}
