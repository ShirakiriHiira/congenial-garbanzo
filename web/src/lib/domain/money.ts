interface MoneyValue { amount?: number; currency?: string }

// ISO 4217 minor-unit exponents that differ from the common two decimals.
// Everything not listed uses two decimal places.
const CURRENCY_EXPONENT: Record<string, number> = {
  BIF: 0, CLP: 0, DJF: 0, GNF: 0, ISK: 0, JPY: 0, KMF: 0, KRW: 0,
  PYG: 0, RWF: 0, UGX: 0, UYI: 0, VND: 0, VUV: 0, XAF: 0, XOF: 0, XPF: 0,
  BHD: 3, IQD: 3, JOD: 3, KWD: 3, LYD: 3, OMR: 3, TND: 3,
  CLF: 4, UYW: 4,
};

function currencyExponent(currency: string): number {
  return CURRENCY_EXPONENT[currency] ?? 2;
}

/**
 * Parses a decimal major-unit amount and returns exact minor units.
 * API amounts stay integers so existing data and the Telegram bot keep working.
 */
export function parseMoney(amount: string, currency: string): MoneyValue {
  const rawAmount = amount.trim();
  const rawCurrency = currency.trim();
  if (!rawAmount && !rawCurrency) return {};
  if (!rawAmount || !rawCurrency) throw new Error('金额和币种必须同时填写或同时留空');
  if (!/^[A-Z]{3}$/.test(rawCurrency)) throw new Error('币种必须为三位大写英文字母');
  if (rawAmount.startsWith('-')) throw new Error('金额必须是非负数');
  if (!/^\d+(?:\.\d+)?$/.test(rawAmount)) throw new Error('金额格式无效，请输入非负十进制数');
  const [whole, fraction = ''] = rawAmount.split('.');
  const exponent = currencyExponent(rawCurrency);
  if (fraction.length > exponent) {
    throw new Error(exponent === 0
      ? `${rawCurrency} 不支持小数`
      : `金额最多 ${exponent} 位小数（${rawCurrency}）`);
  }
  const digits = `${whole}${fraction.padEnd(exponent, '0')}`.replace(/^0+(?=\d)/, '');
  const parsed = Number(digits);
  if (!Number.isSafeInteger(parsed)) throw new Error('金额超出可精确表示的范围');
  return { amount: parsed, currency: rawCurrency };
}

/** Renders exact minor units as a compact decimal major-unit input value. */
export function formatMoneyInput(amount: number | null, currency: string | null): string {
  if (amount === null || currency === null) return '';
  if (!Number.isSafeInteger(amount) || amount < 0 || !/^[A-Z]{3}$/.test(currency)) return '';
  const exponent = currencyExponent(currency);
  if (exponent === 0) return String(amount);
  const scale = 10 ** exponent;
  const whole = Math.floor(amount / scale);
  const fraction = String(amount % scale).padStart(exponent, '0').replace(/0+$/, '');
  return fraction === '' ? String(whole) : `${whole}.${fraction}`;
}

export function formatMoney(amount: number | null, currency: string | null): string {
  if (amount === null && currency === null) return '未填写金额';
  if (amount === null || !Number.isSafeInteger(amount) || amount < 0) return '该金额不可精确显示';
  if (currency === null || !/^[A-Z]{3}$/.test(currency)) return '金额币种无效';
  return `${formatMoneyInput(amount, currency)} ${currency}`;
}
