export interface TelegramWebApp {
  initData: string;
  colorScheme?: 'light' | 'dark';
  viewportStableHeight?: number;
  contentSafeAreaInset?: { top: number; bottom: number; left: number; right: number };
  ready?: () => void;
  expand?: () => void;
  isVersionAtLeast?: (version: string) => boolean;
  onEvent?: (name: string, callback: () => void) => void;
  offEvent?: (name: string, callback: () => void) => void;
  setHeaderColor?: (color: string) => void;
  setBackgroundColor?: (color: string) => void;
  setBottomBarColor?: (color: string) => void;
  enableClosingConfirmation?: () => void;
  disableClosingConfirmation?: () => void;
  BackButton?: {
    show: () => void;
    hide: () => void;
    onClick: (callback: () => void) => void;
    offClick: (callback: () => void) => void;
  };
}

declare global {
  interface Window { Telegram?: { WebApp?: TelegramWebApp } }
}

export function getWebApp(): TelegramWebApp | undefined {
  return window.Telegram?.WebApp;
}

export function getInitData(): string {
  return getWebApp()?.initData ?? '';
}

export function supportsVersion(sdk: TelegramWebApp, version: string): boolean {
  return sdk.isVersionAtLeast?.(version) ?? true;
}

export function setClosingConfirmation(dirty: boolean): void {
  const sdk = getWebApp();
  if (!sdk || !supportsVersion(sdk, '6.2')) return;
  if (dirty) sdk.enableClosingConfirmation?.();
  else sdk.disableClosingConfirmation?.();
}

export function startTelegram(): () => void {
  const sdk = getWebApp();
  if (!sdk) return () => {};
  sdk.ready?.();
  sdk.expand?.();
  const updateViewport = () => {
    const root = document.documentElement;
    if (sdk.viewportStableHeight && sdk.viewportStableHeight > 0) {
      root.style.setProperty('--tg-viewport-stable-height', `${sdk.viewportStableHeight}px`);
    }
    const inset = sdk.contentSafeAreaInset;
    if (inset) {
      for (const side of ['top', 'bottom', 'left', 'right'] as const) {
        if (Number.isFinite(inset[side]) && inset[side] >= 0) {
          root.style.setProperty(`--tg-content-safe-area-${side}`, `${inset[side]}px`);
        }
      }
    }
  };
  updateViewport();
  sdk.onEvent?.('viewportChanged', updateViewport);
  sdk.onEvent?.('contentSafeAreaChanged', updateViewport);
  return () => {
    sdk.offEvent?.('viewportChanged', updateViewport);
    sdk.offEvent?.('contentSafeAreaChanged', updateViewport);
    setClosingConfirmation(false);
    for (const name of ['viewport-stable-height', 'content-safe-area-top', 'content-safe-area-bottom', 'content-safe-area-left', 'content-safe-area-right']) {
      document.documentElement.style.removeProperty(`--tg-${name}`);
    }
  };
}
