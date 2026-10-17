export interface MoneyValue { amount?: number; currency?: string }

/** API amounts are integer minor units, not decimal prices. Empty pairs are omitted. */
export function parseMoney(amount: string, currency: string): MoneyValue {
  const rawAmount = amount.trim();
  const rawCurrency = currency.trim();
  if (!rawAmount && !rawCurrency) return {};
  if (!rawAmount || !rawCurrency) throw new Error('金额和币种必须同时填写或同时留空');
  if (!/^\d+$/.test(rawAmount)) throw new Error('金额必须是非负整数，单位为最小货币单位');
  const parsed = Number(rawAmount);
  if (!Number.isSafeInteger(parsed)) throw new Error('金额超出可精确表示的范围（最大 9007199254740991）');
  if (!/^[A-Z]{3}$/.test(rawCurrency)) throw new Error('币种必须为三位大写英文字母');
  return { amount: parsed, currency: rawCurrency };
}

export function formatMoney(amount: number | null, currency: string | null): string {
  if (amount === null && currency === null) return '未填写金额';
  if (amount === null || !Number.isSafeInteger(amount) || amount < 0) return '该金额不可精确显示';
  if (currency === null || !/^[A-Z]{3}$/.test(currency)) return '金额币种无效';
  return `${amount} ${currency}（最小货币单位）`;
}
