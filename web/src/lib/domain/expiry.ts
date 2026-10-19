import type { Period } from '../api/types';
import { calendarDaysBetween, formatDateTime, localDateAt } from './datetime';

export interface ExpiryDisplay {
  tone: 'muted' | 'ok' | 'warn' | 'danger';
  label: string;
  days: number | null;
  needsRefresh: boolean;
}

export function formatExpiry(period: Period): string {
  if (period.date_precision === 'date') return period.local_expires_date ?? '日期不可用';
  return formatDateTime(period.expires_at, period.timezone);
}

/** Server status wins; the clock is only used for supplementary calendar-day text. */
export function expiryDisplay(period: Period | null, now: string = new Date().toISOString()): ExpiryDisplay {
  if (!period) return { tone: 'muted', label: '未设置有效期', days: null, needsRefresh: false };
  if (period.status === 'expired') return { tone: 'danger', label: '已过期', days: null, needsRefresh: false };
  const endDate = period.date_precision === 'date' ? period.local_expires_date : localDateAt(period.expires_at, period.timezone);
  if (!endDate) return { tone: 'ok', label: '有效', days: null, needsRefresh: true };
  const days = calendarDaysBetween(localDateAt(now, period.timezone), endDate);
  if (days < 0) return { tone: 'ok', label: '有效', days: null, needsRefresh: true };
  return {
    tone: days <= 7 ? 'warn' : 'ok',
    label: days === 0 ? '今天到期' : days <= 7 ? `${days} 天后到期` : '有效',
    days,
    needsRefresh: false,
  };
}
