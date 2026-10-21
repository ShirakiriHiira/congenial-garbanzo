import { getWebApp, supportsVersion } from './sdk';

export function startTheme(): () => void {
  const sdk = getWebApp();
  const media = window.matchMedia('(prefers-color-scheme: dark)');
  const sync = () => {
    const theme = sdk?.colorScheme ?? (media.matches ? 'dark' : 'light');
    document.documentElement.dataset.theme = theme;
    if (!sdk) return;
    const style = getComputedStyle(document.documentElement);
    const header = style.getPropertyValue('--header-bg').trim();
    const page = style.getPropertyValue('--page-bg').trim();
    if (supportsVersion(sdk, '6.1')) {
      if (header) sdk.setHeaderColor?.(header);
      if (page) sdk.setBackgroundColor?.(page);
    }
    if (page && supportsVersion(sdk, '7.10')) sdk.setBottomBarColor?.(page);
  };
  sync();
  sdk?.onEvent?.('themeChanged', sync);
  media.addEventListener('change', sync);
  return () => {
    sdk?.offEvent?.('themeChanged', sync);
    media.removeEventListener('change', sync);
  };
}
