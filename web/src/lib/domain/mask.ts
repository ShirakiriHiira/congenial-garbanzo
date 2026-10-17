/** Never reveal an entire short number; discard formatting except an initial plus. */
export function maskPhone(value: string | null | undefined): string {
  if (!value) return '';
  const digits = value.replace(/\D/g, '');
  if (!digits) return '';
  const prefix = value.trim().startsWith('+') ? '+' : '';
  if (digits.length > 8) return `${prefix}${digits.slice(0, 3)}${'*'.repeat(digits.length - 7)}${digits.slice(-4)}`;
  const visible = Math.min(2, Math.max(0, digits.length - 1));
  return `${prefix}${'*'.repeat(digits.length - visible)}${visible ? digits.slice(-visible) : ''}`;
}
