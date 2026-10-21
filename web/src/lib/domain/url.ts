/**
 * Returns a safe href for an absolute http(s) URL without embedded
 * credentials, or null when the value is not safe to link.
 */
export function safeManagementURL(value: string | null | undefined): string | null {
  if (!value) return null;
  if (Array.from(value).length > 2048 || !/^https?:\/\/[^\s/@]+(?:[/?#]|$)/.test(value) || /^[^/]*\/\/[^/?#]*@/.test(value)) return null;
  try {
    const url = new URL(value);
    return (url.protocol === 'http:' || url.protocol === 'https:') && url.hostname && !url.username && !url.password ? url.href : null;
  } catch {
    return null;
  }
}
