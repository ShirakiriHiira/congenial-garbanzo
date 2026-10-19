import type { PeriodTermsInput } from '../api/types';
import { intervalUnitLabels, termsModeLabels } from './labels';

/** Describes configured terms without deriving or advancing an expiry date. */
export function formatTermsCycle(terms: PeriodTermsInput): string {
  if (terms.mode === 'recurring') {
    if (terms.interval_count === null || terms.interval_unit === null) return termsModeLabels.recurring;
    return `每 ${terms.interval_count} ${intervalUnitLabels[terms.interval_unit]}`;
  }
  return terms.mode === 'once' ? termsModeLabels.once : '自定义周期';
}
