import { Temporal } from '@js-temporal/polyfill';

export type Occurrence = 'earlier' | 'later';
export interface WallTimeCandidate { occurrence: Occurrence; offset: string; value: string }
export class DateTimeError extends Error {
  constructor(public readonly code: 'invalid' | 'gap' | 'ambiguous', message: string) {
    super(message);
    this.name = 'DateTimeError';
  }
}

export function isValidTimeZone(zone: string): boolean {
  if (!zone || zone !== zone.trim() || zone.toLowerCase() === 'local' || /^[+-]/.test(zone)) return false;
  try { new Intl.DateTimeFormat('zh-CN', { timeZone: zone }); return true; } catch { return false; }
}

export function timeZoneOptions(): string[] {
  const supported = (Intl as typeof Intl & { supportedValuesOf?: (key: 'timeZone') => string[] }).supportedValuesOf;
  return supported ? ['UTC', ...supported('timeZone').filter((zone) => zone !== 'UTC')] : [];
}

export function isValidDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  try { Temporal.PlainDate.from(value, { overflow: 'reject' }); return true; } catch { return false; }
}

/** Never silently advance a nonexistent wall time or select an overlap occurrence. */
export function wallTimeCandidates(value: string, zone: string): WallTimeCandidate[] {
  if (!isValidTimeZone(zone) || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d{1,9})?)?$/.test(value)) {
    throw new DateTimeError('invalid', '请输入有效的日期、时间和时区');
  }
  try {
    const wall = Temporal.PlainDateTime.from(value, { overflow: 'reject' });
    const earlier = wall.toZonedDateTime(zone, { disambiguation: 'earlier' });
    const later = wall.toZonedDateTime(zone, { disambiguation: 'later' });
    if (!earlier.toPlainDateTime().equals(wall) || !later.toPlainDateTime().equals(wall)) {
      throw new DateTimeError('gap', '该时间因夏令时切换不存在，请选择其他时间');
    }
    const candidate = (time: typeof earlier, occurrence: Occurrence): WallTimeCandidate => ({
      occurrence, offset: time.offset,
      value: time.toString({ timeZoneName: 'never', calendarName: 'never' }),
    });
    return earlier.epochNanoseconds === later.epochNanoseconds
      ? [candidate(earlier, 'earlier')]
      : [candidate(earlier, 'earlier'), candidate(later, 'later')];
  } catch (error) {
    if (error instanceof DateTimeError) throw error;
    throw new DateTimeError('invalid', '请输入有效的日期、时间和时区');
  }
}

export function wallTimeToRFC3339(value: string, zone: string, occurrence?: Occurrence): string {
  const candidates = wallTimeCandidates(value, zone);
  if (candidates.length === 1) return candidates[0]!.value;
  const selected = candidates.find((candidate) => candidate.occurrence === occurrence);
  if (!selected) throw new DateTimeError('ambiguous', '该时间出现两次，请明确选择时区偏移');
  return selected.value;
}

/** Retains seconds and overlap choice when editing an existing instant. */
export function instantToWallTime(value: string, zone: string): { value: string; occurrence: Occurrence } {
  const instant = Temporal.Instant.from(value);
  const zoned = instant.toZonedDateTimeISO(zone);
  const wall = zoned.toPlainDateTime().toString();
  const candidates = wallTimeCandidates(wall, zone);
  const selected = candidates.find((candidate) => Temporal.Instant.from(candidate.value).equals(instant));
  return { value: wall, occurrence: selected?.occurrence ?? 'earlier' };
}

export function localDateAt(value: string, zone: string): string {
  return Temporal.Instant.from(value).toZonedDateTimeISO(zone).toPlainDate().toString();
}

export function formatDateTime(value: string, zone: string): string {
  const wall = Temporal.Instant.from(value).toZonedDateTimeISO(zone).toPlainDateTime();
  return wall.toString({ smallestUnit: 'minute' }).replace('T', ' ');
}

/** Calendar arithmetic intentionally ignores elapsed hours, including DST days. */
export function calendarDaysBetween(from: string, to: string): number {
  return Temporal.PlainDate.from(from).until(Temporal.PlainDate.from(to), { largestUnit: 'day' }).days;
}
