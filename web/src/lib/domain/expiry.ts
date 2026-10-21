import type { DatePrecision, Period } from '../api/types';
import { calendarDaysBetween, formatDateTime, localDateAt } from './datetime';

export interface ExpiryDisplay {
  tone: 'muted' | 'ok' | 'warn' | 'danger';
  label: string;
  days: number | null;
}

/**
 * Renders a cutoff as a local date or a local wall time. A date-precision
 * cutoff has no local date only when the server could not resolve one.
 */
export function formatLocalCutoff(precision: DatePrecision, localDate: string | null, expiresAt: string, timezone: string, fallback = '日期不可用'): string {
  if (precision === 'date') return localDate ?? fallback;
  return formatDateTime(expiresAt, timezone);
}

export function formatExpiry(period: Period): string {
  return formatLocalCutoff(period.date_precision, period.local_expires_date, period.expires_at, period.timezone);
}

/** Server status wins; the clock is only used for supplementary calendar-day text. */
export function expiryDisplay(period: Period | null, now: string = new Date().toISOString()): ExpiryDisplay {
  if (!period) return { tone: 'muted', label: '未设置有效期', days: null };
  if (period.status === 'expired') return { tone: 'danger', label: '已过期', days: null };
  const endDate = period.date_precision === 'date' ? period.local_expires_date : localDateAt(period.expires_at, period.timezone);
  if (!endDate) return { tone: 'ok', label: '有效', days: null };
  const days = calendarDaysBetween(localDateAt(now, period.timezone), endDate);
  if (days < 0) return { tone: 'ok', label: '有效', days: null };
  return {
    tone: days <= 7 ? 'warn' : 'ok',
    label: days === 0 ? '今天到期' : days <= 7 ? `${days} 天后到期` : '有效',
    days,
  };
}
