import { describe, expect, it } from 'vitest';
import type { Period } from '../src/lib/api/types';
import { calendarDaysBetween, DateTimeError, formatDateTime, instantToWallTime, isValidDate, isValidTimeZone, wallTimeCandidates, wallTimeToRFC3339 } from '../src/lib/domain/datetime';
import { expiryDisplay, formatExpiry } from '../src/lib/domain/expiry';
import { maskPhone } from '../src/lib/domain/mask';
import { formatMoney, parseMoney } from '../src/lib/domain/money';
import { ApiError } from '../src/lib/api/errors';
import { errorLabels, errorMessage, intervalUnitLabels, periodLabel, termsModeLabels } from '../src/lib/domain/labels';
import { formatTermsCycle } from '../src/lib/domain/terms';

const period: Period = {
  id: '1', service_id: '2', name: '合成号码有效期',
  starts_at: null, expires_at: '2027-01-08T16:00:00Z', date_precision: 'date',
  local_expires_date: '2027-01-08', timezone: 'Asia/Taipei', status: 'active', cycle: 1,
  terms: { mode: 'custom', interval_count: null, interval_unit: null, amount: null, currency: null, revision: 1 },
  created_at: '2027-01-01T00:00:00Z', updated_at: '2027-01-01T00:00:00Z',
};
const now = '2027-01-01T00:00:00Z';

describe('用户时区墙上时间', () => {
  it('转换用户时区，不依赖设备时区', () => {
    expect(wallTimeToRFC3339('2027-01-31T18:00', 'Asia/Taipei')).toBe('2027-01-31T18:00:00+08:00');
    expect(formatDateTime('2027-01-31T10:00:00Z', 'Asia/Taipei')).toBe('2027-01-31 18:00:00');
  });

  it('保留秒并在省略秒时按零秒提交', () => {
    expect(wallTimeToRFC3339('2027-01-31T18:00', 'Asia/Taipei')).toBe('2027-01-31T18:00:00+08:00');
    expect(wallTimeToRFC3339('2027-01-31T18:00:45', 'Asia/Taipei')).toBe('2027-01-31T18:00:45+08:00');
    expect(instantToWallTime('2027-01-31T10:00:45Z', 'Asia/Taipei')).toEqual({ value: '2027-01-31T18:00:45', occurrence: 'earlier' });
    expect(wallTimeToRFC3339(instantToWallTime('2027-01-31T10:00:45Z', 'Asia/Taipei').value, 'Asia/Taipei')).toBe('2027-01-31T18:00:45+08:00');
    expect(formatDateTime('2027-01-31T10:00:45Z', 'Asia/Taipei')).toBe('2027-01-31 18:00:45');
  });
  it('拒绝夏令时不存在的时间', () => {
    expect(() => wallTimeToRFC3339('2027-03-14T02:30', 'America/New_York')).toThrow(DateTimeError);
    try { wallTimeCandidates('2027-03-14T02:30', 'America/New_York'); }
    catch (error) { expect((error as DateTimeError).code).toBe('gap'); }
  });
  it('重复时间必须明确选择，提供两个真实偏移', () => {
    const value = '2027-11-07T01:30';
    const zone = 'America/New_York';
    expect(wallTimeCandidates(value, zone).map((candidate) => candidate.offset)).toEqual(['-04:00', '-05:00']);
    expect(() => wallTimeToRFC3339(value, zone)).toThrow(DateTimeError);
    expect(wallTimeToRFC3339(value, zone, 'earlier')).toBe('2027-11-07T01:30:00-04:00');
    expect(wallTimeToRFC3339(value, zone, 'later')).toBe('2027-11-07T01:30:00-05:00');
    expect(instantToWallTime('2027-11-07T06:30:00Z', zone)).toEqual({ value: '2027-11-07T01:30:00', occurrence: 'later' });
  });
  it('校验真实日期和命名时区', () => {
    expect(isValidDate('2027-02-29')).toBe(false);
    expect(isValidDate('2028-02-29')).toBe(true);
    expect(isValidTimeZone('Local')).toBe(false);
    expect(isValidTimeZone('+08:00')).toBe(false);
    expect(isValidTimeZone('Asia/Taipei')).toBe(true);
    expect(() => wallTimeToRFC3339('2027-02-30T12:00', 'UTC')).toThrow(DateTimeError);
  });
});

describe('服务端状态与日历有效期', () => {
  it('跨月、闰年及夏令时仍按日历天数', () => {
    expect(calendarDaysBetween('2027-01-31', '2027-02-01')).toBe(1);
    expect(calendarDaysBetween('2028-02-28', '2028-03-01')).toBe(2);
    expect(calendarDaysBetween('2027-03-13', '2027-03-15')).toBe(2);
    expect(calendarDaysBetween('2027-11-06', '2027-11-08')).toBe(2);
  });
  it('日期精度直接使用本地截止日', () => {
    expect(formatExpiry(period)).toBe('2027-01-08');
    expect(formatExpiry({ ...period, date_precision: 'datetime' })).toBe('2027-01-09 00:00:00');
    expect(formatExpiry({ ...period, date_precision: 'datetime', expires_at: '2027-01-08T16:00:37Z' })).toBe('2027-01-09 00:00:37');
  });
  it('区分今天、七天、八天和无主期', () => {
    expect(expiryDisplay({ ...period, local_expires_date: '2027-01-01' }, now)).toMatchObject({ days: 0, tone: 'warn' });
    expect(expiryDisplay(period, now)).toMatchObject({ days: 7, tone: 'warn' });
    expect(expiryDisplay({ ...period, local_expires_date: '2027-01-09' }, now)).toMatchObject({ days: 8, tone: 'ok' });
    expect(expiryDisplay(null, now)).toMatchObject({ days: null, tone: 'muted' });
  });
  it('服务端状态优先，不将本地负天数转为过期', () => {
    expect(expiryDisplay({ ...period, status: 'expired' }, now)).toMatchObject({ days: null, tone: 'danger' });
    expect(expiryDisplay({ ...period, local_expires_date: '2026-12-31' }, now)).toMatchObject({ days: null, tone: 'ok' });
  });
});

describe('号码隐私', () => {
  it('长号码仅保留前三后四位及加号', () => {
    expect(maskPhone('+886 912-345-678')).toBe('+886*****5678');
    expect(maskPhone('123456789')).toBe('123**6789');
  });
  it('短号码不会完整泄露', () => {
    expect(maskPhone('12345678')).toBe('******78');
    expect(maskPhone('12')).toBe('*2');
    expect(maskPhone('1')).toBe('*');
    expect(maskPhone(null)).toBe('');
    expect(maskPhone('---')).toBe('');
  });
});

describe('带小数的金额', () => {
  it('按币种标准小数位换算为整数最小单位', () => {
    expect(parseMoney('', '')).toEqual({});
    expect(parseMoney('0', 'CNY')).toEqual({ amount: 0, currency: 'CNY' });
    expect(parseMoney('12.34', 'CNY')).toEqual({ amount: 1234, currency: 'CNY' });
    expect(parseMoney('12.3', 'CNY')).toEqual({ amount: 1230, currency: 'CNY' });
    expect(parseMoney('100', 'JPY')).toEqual({ amount: 100, currency: 'JPY' });
    expect(parseMoney('1.234', 'KWD')).toEqual({ amount: 1234, currency: 'KWD' });
    expect(parseMoney('90071992547409.91', 'USD').amount).toBe(Number.MAX_SAFE_INTEGER);
    expect(formatMoney(1234, 'CNY')).toBe('12.34 CNY');
    expect(formatMoney(1200, 'CNY')).toBe('12 CNY');
  });
  it.each(['-1', '1e3', 'NaN', '12.345', '1.5'])('拒绝无效金额 %s', (amount) => {
    expect(() => parseMoney(amount, amount === '1.5' ? 'JPY' : 'CNY')).toThrow();
  });
  it('金额币种必须成对且币种为三位大写', () => {
    expect(() => parseMoney('1', '')).toThrow();
    expect(() => parseMoney('', 'CNY')).toThrow();
    expect(() => parseMoney('1', 'cny')).toThrow();
    expect(formatMoney(Number.MAX_SAFE_INTEGER + 1, 'CNY')).not.toContain('9007199254740992');
  });
});

describe('安全中文标签', () => {
  it('只展示受控错误类别，不回显后端内容', () => {
    expect(errorMessage(new ApiError('auth', '敏感服务端内容'))).toBe(errorLabels.auth);
    expect(errorMessage(new ApiError('timeout', '敏感服务端内容'))).toBe(errorLabels.timeout);
    expect(errorMessage(new Error('敏感服务端内容'))).not.toContain('敏感服务端内容');
    expect(errorMessage(new DOMException('中止', 'AbortError'))).toBe('');
  });
  it('有效期使用名称或回退', () => {
    expect(periodLabel({ name: '合成自定义有效期' })).toBe('合成自定义有效期');
  });
});

describe('通用条款与默认有效期名称', () => {
  it('未命名的有效期显示为有效期', () => {
    expect(periodLabel({ name: '' })).toBe('有效期');
  });
  it('约定周期只描述条款，不推导日期', () => {
    expect(formatTermsCycle({ mode: 'once', interval_count: null, interval_unit: null, amount: null, currency: null })).toBe(termsModeLabels.once);
    expect(formatTermsCycle({ mode: 'recurring', interval_count: 3, interval_unit: 'month', amount: 0, currency: 'CNY' })).toBe('每 3 月');
    expect(formatTermsCycle({ mode: 'recurring', interval_count: 1, interval_unit: 'year', amount: null, currency: null })).toBe(`每 1 ${intervalUnitLabels.year}`);
    expect(formatTermsCycle({ mode: 'custom', interval_count: null, interval_unit: null, amount: null, currency: null })).toBe('自定义周期');
  });
});
