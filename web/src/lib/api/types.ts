export type ID = string;

export type CardType = "physical_sim" | "esim";
export type ServiceStatus = "active" | "archived";
export type CategoryKind = "sim" | "generic";
export type ServiceStatusFilter = "active" | "due_soon" | "expired" | "no_period" | "archived" | "all";
export type TermsMode = "once" | "recurring" | "custom";
export type IntervalUnit = "day" | "week" | "month" | "year";
export type DatePrecision = "date" | "datetime";
export type PeriodStatus = "active" | "expired";
export type ReminderStatus = "pending" | "sending" | "sent" | "failed" | "cancelled";
export type ReminderStatusFilter = ReminderStatus | "all";

export interface ServiceFilter {
  q?: string;
  category_id?: ID;
  status?: ServiceStatusFilter;
}

export interface PeriodTermsInput {
  mode: TermsMode;
  interval_count: number | null;
  interval_unit: IntervalUnit | null;
  amount: number | null;
  currency: string | null;
}

export interface PeriodTerms extends PeriodTermsInput {
  revision: number;
}

export interface Category {
  id: ID;
  user_id: ID | null;
  code: string | null;
  name: string;
  kind: CategoryKind;
  created_at: string;
  updated_at: string;
}

export interface SimDetails {
  card_type: CardType;
  phone_number: string | null;
  country_or_region: string | null;
  iccid: string | null;
}

export interface ServiceItem {
  id: ID;
  user_id: ID;
  name: string;
  category_id: ID;
  provider: string | null;
  identifier: string | null;
  management_url: string | null;
  notes: string | null;
  status: ServiceStatus;
  created_at: string;
  updated_at: string;
  sim_details: SimDetails | null;
}

export interface ServiceCreateInput {
  name: string;
  category_id: ID;
  provider?: string | null;
  identifier?: string | null;
  management_url?: string | null;
  notes?: string | null;
  sim_details?: SimDetailsCreateInput | null;
}

export interface ServiceUpdateInput {
  name?: string;
  category_id?: ID;
  provider?: string | null;
  identifier?: string | null;
  management_url?: string | null;
  notes?: string | null;
  status?: ServiceStatus;
  sim_details?: SimDetailsUpdateInput | null;
}

export interface SimDetailsCreateInput {
  card_type: CardType;
  phone_number?: string | null;
  country_or_region?: string | null;
  iccid?: string | null;
}

export interface SimDetailsUpdateInput {
  card_type?: CardType;
  phone_number?: string | null;
  country_or_region?: string | null;
  iccid?: string | null;
}

export interface CategoryNameInput {
  name: string;
}

export interface User {
  id: ID;
  telegram_user_id: ID;
  timezone: string;
  has_private_chat: boolean;
  session_ttl_seconds: number | null;
  created_at: string;
  updated_at: string;
}

export interface Period {
  id: ID;
  service_id: ID;
  name: string;
  starts_at: string | null;
  expires_at: string;
  date_precision: DatePrecision;
  local_expires_date: string | null;
  timezone: string;
  status: PeriodStatus;
  cycle: number;
  terms: PeriodTerms;
  created_at: string;
  updated_at: string;
}

export interface ServiceOverviewItem {
  service: ServiceItem;
  primary_period: Period | null;
  periods: Period[];
}

export interface ServiceOverviewResponse {
  data: {
    counts: OverviewCounts;
    items: ServiceOverviewItem[];
  };
  next_cursor: ID | null;
}

export interface Rule {
  id: ID;
  period_id: ID;
  days_before: number;
  reminder_time: string;
  enabled: boolean;
  revision: number;
  created_at: string;
  updated_at: string;
}

export interface Renewal {
  id: ID;
  period_id: ID;
  previous_cycle: number;
  previous_expires_at: string;
  new_expires_at: string;
  previous_date_precision: DatePrecision;
  new_date_precision: DatePrecision;
  previous_local_expires_date: string | null;
  new_local_expires_date: string | null;
  timezone: string;
  renewed_at: string;
  amount: number | null;
  currency: string | null;
  notes: string | null;
  created_at: string;
}

export interface ReminderJob {
  id: ID;
  period_id: ID;
  reminder_rule_id: ID;
  cycle: number;
  rule_revision: number;
  scheduled_at: string;
  expires_at_snapshot: string;
  status: ReminderStatus;
  attempts: number;
  next_attempt_at: string | null;
  sent_at: string | null;
  telegram_message_id: ID | null;
  last_error_code: string | null;
  created_at: string;
  updated_at: string;
}

export interface OverviewCounts {
  total: number;
  due_soon: number;
  expired: number;
  no_period: number;
}

export interface Page<T> {
  data: T[];
  next_cursor: ID | null;
}

export interface PeriodCreateInput {
  name: string;
  starts_at?: string | null;
  date_precision: DatePrecision;
  expires_at: string;
  terms?: PeriodTermsInput | null;
}

export interface PeriodUpdateInput {
  expected_cycle: number;
  name?: string;
  starts_at?: string | null;
  date_precision?: DatePrecision;
  expires_at?: string;
  terms?: PeriodTermsInput | null;
  expected_terms_revision?: number;
}

export interface RuleCreateInput {
  days_before: number;
  reminder_time: string;
  enabled?: boolean;
}

export interface RuleUpdateInput {
  expected_revision: number;
  days_before?: number;
  reminder_time?: string;
  enabled?: boolean;
}

export interface RenewalCreateInput {
  expected_cycle: number;
  date_precision: DatePrecision;
  expires_at: string;
  amount?: number | null;
  currency?: string | null;
  notes?: string | null;
}

export interface UserUpdateInput {
  timezone?: string;
  session_ttl_seconds?: number;
}
