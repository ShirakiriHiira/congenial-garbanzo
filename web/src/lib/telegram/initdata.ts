export interface TelegramDisplayUser { firstName: string; photoUrl: string | null }

// Display metadata only; authorization is exclusively verified by the server.
export function parseDisplayUser(raw: string): TelegramDisplayUser {
  const fallback: TelegramDisplayUser = { firstName: '', photoUrl: null };
  try {
    const value: unknown = JSON.parse(new URLSearchParams(raw).get('user') ?? 'null');
    if (!value || typeof value !== 'object') return fallback;
    const record = value as Record<string, unknown>;
    const firstName = typeof record.first_name === 'string' ? record.first_name : '';
    let photoUrl: string | null = null;
    if (typeof record.photo_url === 'string') {
      try {
        const url = new URL(record.photo_url);
        if (url.protocol === 'https:' && !url.username && !url.password) photoUrl = url.href;
      } catch {
        // Invalid avatar URLs must not discard an otherwise usable display name.
      }
    }
    return { firstName, photoUrl };
  } catch {
    return fallback;
  }
}
