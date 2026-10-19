import type {
  Category,
  ID,
  OverviewCounts,
  Page,
  Period,
  PeriodTerms,
  ReminderJob,
  Renewal,
  Rule,
  ServiceItem,
  ServiceOverviewItem,
  ServiceOverviewResponse,
  SimDetails,
  User,
} from "./types";

export type Decoder<T> = (value: unknown, path?: string) => T;

type RecordValue = Record<string, unknown>;

const maximumID = 9_223_372_036_854_775_807n;
const decimalID = /^(?:0|[1-9][0-9]*)$/;
const localDate = /^\d{4}-\d{2}-\d{2}$/;
const utcTimestamp = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/;
const wallTime = /^(?:[01]\d|2[0-3]):[0-5]\d$/;

function fail(path: string, message: string): never {
  throw new TypeError(`Invalid API response at ${path}: ${message}`);
}

function object(value: unknown, path: string): RecordValue {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    fail(path, "expected an object");
  }
  return value as RecordValue;
}

function field(value: RecordValue, key: string, path: string): unknown {
  if (!(key in value)) {
    fail(`${path}.${key}`, "is required");
  }
  return value[key];
}

function text(value: unknown, path: string): string {
  if (typeof value !== "string") {
    fail(path, "expected a string");
  }
  return value;
}

function nullableText(value: unknown, path: string): string | null {
  return value === null ? null : text(value, path);
}

function boolean(value: unknown, path: string): boolean {
  if (typeof value !== "boolean") {
    fail(path, "expected a boolean");
  }
  return value;
}

function integer(value: unknown, path: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value)) {
    fail(path, "expected a safe integer");
  }
  return value;
}

function nonNegativeInteger(value: unknown, path: string): number {
  const parsed = integer(value, path);
  if (parsed < 0) {
    fail(path, "expected a non-negative integer");
  }
  return parsed;
}

function nonNegativeWholeNumber(value: unknown, path: string): number {
  if (typeof value !== "number" || !Number.isFinite(value) || !Number.isInteger(value) || value < 0) {
    fail(path, "expected a non-negative integer");
  }
  return value;
}

function timestamp(value: unknown, path: string): string {
  const parsed = text(value, path);
  if (!utcTimestamp.test(parsed) || Number.isNaN(Date.parse(parsed))) {
    fail(path, "expected a UTC RFC3339 timestamp");
  }
  return parsed;
}

function nullableTimestamp(value: unknown, path: string): string | null {
  return value === null ? null : timestamp(value, path);
}

function date(value: unknown, path: string): string {
  const parsed = text(value, path);
  if (!localDate.test(parsed)) {
    fail(path, "expected a YYYY-MM-DD date");
  }
  return parsed;
}

function id(value: unknown, path: string): ID {
  const parsed = text(value, path);
  if (!decimalID.test(parsed) || parsed === "0") {
    fail(path, "expected a positive decimal ID");
  }
  if (BigInt(parsed) > maximumID) {
    fail(path, "is outside the API ID range");
  }
  return parsed;
}

function nullableID(value: unknown, path: string): ID | null {
  return value === null ? null : id(value, path);
}

function oneOf<T extends string>(value: unknown, values: readonly T[], path: string): T {
  const parsed = text(value, path);
  if (!values.includes(parsed as T)) {
    fail(path, `expected one of ${values.join(", ")}`);
  }
  return parsed as T;
}

function decodeUser(value: unknown, path = "data"): User {
  const source = object(value, path);
  return {
    id: id(field(source, "id", path), `${path}.id`),
    telegram_user_id: id(field(source, "telegram_user_id", path), `${path}.telegram_user_id`),
    timezone: text(field(source, "timezone", path), `${path}.timezone`),
    has_private_chat: boolean(field(source, "has_private_chat", path), `${path}.has_private_chat`),
    created_at: timestamp(field(source, "created_at", path), `${path}.created_at`),
    updated_at: timestamp(field(source, "updated_at", path), `${path}.updated_at`),
  };
}

function decodeCategory(value: unknown, path = "data"): Category {
  const source = object(value, path);
  return {
    id: id(field(source, "id", path), `${path}.id`),
    user_id: nullableID(field(source, "user_id", path), `${path}.user_id`),
    code: nullableText(field(source, "code", path), `${path}.code`),
    name: text(field(source, "name", path), `${path}.name`),
    kind: oneOf(field(source, "kind", path), ["sim", "generic"], `${path}.kind`),
    created_at: timestamp(field(source, "created_at", path), `${path}.created_at`),
    updated_at: timestamp(field(source, "updated_at", path), `${path}.updated_at`),
  };
}

function decodeSimDetails(value: unknown, path: string): SimDetails {
  const source = object(value, path);
  return {
    card_type: oneOf(field(source, "card_type", path), ["physical_sim", "esim"], `${path}.card_type`),
    phone_number: nullableText(field(source, "phone_number", path), `${path}.phone_number`),
    country_or_region: nullableText(field(source, "country_or_region", path), `${path}.country_or_region`),
    iccid: nullableText(field(source, "iccid", path), `${path}.iccid`),
  };
}

function decodeService(value: unknown, path = "data"): ServiceItem {
  const source = object(value, path);
  const details = field(source, "sim_details", path);
  return {
    id: id(field(source, "id", path), `${path}.id`),
    user_id: id(field(source, "user_id", path), `${path}.user_id`),
    name: text(field(source, "name", path), `${path}.name`),
    category_id: id(field(source, "category_id", path), `${path}.category_id`),
    provider: nullableText(field(source, "provider", path), `${path}.provider`),
    identifier: nullableText(field(source, "identifier", path), `${path}.identifier`),
    management_url: nullableText(field(source, "management_url", path), `${path}.management_url`),
    notes: nullableText(field(source, "notes", path), `${path}.notes`),
    status: oneOf(field(source, "status", path), ["active", "archived"], `${path}.status`),
    created_at: timestamp(field(source, "created_at", path), `${path}.created_at`),
    updated_at: timestamp(field(source, "updated_at", path), `${path}.updated_at`),
    sim_details: details === null ? null : decodeSimDetails(details, `${path}.sim_details`),
  };
}

function decodeTerms(value: unknown, path: string): PeriodTerms {
  const source = object(value, path);
  const mode = oneOf(field(source, "mode", path), ["once", "recurring", "custom"], `${path}.mode`);
  const rawCount = field(source, "interval_count", path);
  const rawUnit = field(source, "interval_unit", path);
  const rawAmount = field(source, "amount", path);
  const rawCurrency = field(source, "currency", path);
  const interval_count = rawCount === null ? null : nonNegativeInteger(rawCount, `${path}.interval_count`);
  const interval_unit = rawUnit === null ? null : oneOf(rawUnit, ["day", "week", "month", "year"], `${path}.interval_unit`);
  const amount = rawAmount === null ? null : nonNegativeWholeNumber(rawAmount, `${path}.amount`);
  const currency = nullableText(rawCurrency, `${path}.currency`);
  const revision = nonNegativeInteger(field(source, "revision", path), `${path}.revision`);
  if (revision < 1) fail(`${path}.revision`, "expected a positive integer");
  if (mode === "recurring") {
    if (interval_count === null || interval_count < 1 || interval_unit === null) {
      fail(path, "recurring terms require a positive interval count and unit");
    }
  } else if (interval_count !== null || interval_unit !== null) {
    fail(path, "non-recurring terms cannot include an interval");
  }
  if ((amount === null) !== (currency === null)) fail(path, "amount and currency must be paired");
  if (currency !== null && !/^[A-Z]{3}$/.test(currency)) fail(`${path}.currency`, "expected three uppercase letters");
  return { mode, interval_count, interval_unit, amount, currency, revision };
}

function decodePeriod(value: unknown, path = "data"): Period {
  const source = object(value, path);
  return {
    id: id(field(source, "id", path), `${path}.id`),
    service_id: id(field(source, "service_id", path), `${path}.service_id`),
    name: text(field(source, "name", path), `${path}.name`),
    starts_at: nullableTimestamp(field(source, "starts_at", path), `${path}.starts_at`),
    expires_at: timestamp(field(source, "expires_at", path), `${path}.expires_at`),
    date_precision: oneOf(field(source, "date_precision", path), ["date", "datetime"], `${path}.date_precision`),
    local_expires_date: field(source, "local_expires_date", path) === null ? null : date(field(source, "local_expires_date", path), `${path}.local_expires_date`),
    timezone: text(field(source, "timezone", path), `${path}.timezone`),
    status: oneOf(field(source, "status", path), ["active", "expired"], `${path}.status`),
    cycle: nonNegativeInteger(field(source, "cycle", path), `${path}.cycle`),
    terms: decodeTerms(field(source, "terms", path), `${path}.terms`),
    created_at: timestamp(field(source, "created_at", path), `${path}.created_at`),
    updated_at: timestamp(field(source, "updated_at", path), `${path}.updated_at`),
  };
}

function decodeRule(value: unknown, path = "data"): Rule {
  const source = object(value, path);
  const reminderTime = text(field(source, "reminder_time", path), `${path}.reminder_time`);
  if (!wallTime.test(reminderTime)) {
    fail(`${path}.reminder_time`, "expected HH:MM time");
  }
  return {
    id: id(field(source, "id", path), `${path}.id`),
    period_id: id(field(source, "period_id", path), `${path}.period_id`),
    days_before: nonNegativeInteger(field(source, "days_before", path), `${path}.days_before`),
    reminder_time: reminderTime,
    enabled: boolean(field(source, "enabled", path), `${path}.enabled`),
    revision: nonNegativeInteger(field(source, "revision", path), `${path}.revision`),
    created_at: timestamp(field(source, "created_at", path), `${path}.created_at`),
    updated_at: timestamp(field(source, "updated_at", path), `${path}.updated_at`),
  };
}

function decodeRenewal(value: unknown, path = "data"): Renewal {
  const source = object(value, path);
  const amount = field(source, "amount", path);
  return {
    id: id(field(source, "id", path), `${path}.id`),
    period_id: id(field(source, "period_id", path), `${path}.period_id`),
    previous_cycle: nonNegativeInteger(field(source, "previous_cycle", path), `${path}.previous_cycle`),
    previous_expires_at: timestamp(field(source, "previous_expires_at", path), `${path}.previous_expires_at`),
    new_expires_at: timestamp(field(source, "new_expires_at", path), `${path}.new_expires_at`),
    previous_date_precision: oneOf(field(source, "previous_date_precision", path), ["date", "datetime"], `${path}.previous_date_precision`),
    new_date_precision: oneOf(field(source, "new_date_precision", path), ["date", "datetime"], `${path}.new_date_precision`),
    previous_local_expires_date: field(source, "previous_local_expires_date", path) === null ? null : date(field(source, "previous_local_expires_date", path), `${path}.previous_local_expires_date`),
    new_local_expires_date: field(source, "new_local_expires_date", path) === null ? null : date(field(source, "new_local_expires_date", path), `${path}.new_local_expires_date`),
    timezone: text(field(source, "timezone", path), `${path}.timezone`),
    renewed_at: timestamp(field(source, "renewed_at", path), `${path}.renewed_at`),
    amount: amount === null ? null : nonNegativeWholeNumber(amount, `${path}.amount`),
    currency: nullableText(field(source, "currency", path), `${path}.currency`),
    notes: nullableText(field(source, "notes", path), `${path}.notes`),
    created_at: timestamp(field(source, "created_at", path), `${path}.created_at`),
  };
}

function decodeReminder(value: unknown, path = "data"): ReminderJob {
  const source = object(value, path);
  return {
    id: id(field(source, "id", path), `${path}.id`),
    period_id: id(field(source, "period_id", path), `${path}.period_id`),
    reminder_rule_id: id(field(source, "reminder_rule_id", path), `${path}.reminder_rule_id`),
    cycle: nonNegativeInteger(field(source, "cycle", path), `${path}.cycle`),
    rule_revision: nonNegativeInteger(field(source, "rule_revision", path), `${path}.rule_revision`),
    scheduled_at: timestamp(field(source, "scheduled_at", path), `${path}.scheduled_at`),
    expires_at_snapshot: timestamp(field(source, "expires_at_snapshot", path), `${path}.expires_at_snapshot`),
    status: oneOf(field(source, "status", path), ["pending", "sending", "sent", "failed", "cancelled"], `${path}.status`),
    attempts: nonNegativeInteger(field(source, "attempts", path), `${path}.attempts`),
    next_attempt_at: nullableTimestamp(field(source, "next_attempt_at", path), `${path}.next_attempt_at`),
    sent_at: nullableTimestamp(field(source, "sent_at", path), `${path}.sent_at`),
    telegram_message_id: nullableID(field(source, "telegram_message_id", path), `${path}.telegram_message_id`),
    last_error_code: nullableText(field(source, "last_error_code", path), `${path}.last_error_code`),
    created_at: timestamp(field(source, "created_at", path), `${path}.created_at`),
    updated_at: timestamp(field(source, "updated_at", path), `${path}.updated_at`),
  };
}

function decodePage<T>(value: unknown, item: Decoder<T>): Page<T> {
  const source = object(value, "response");
  const rawData = field(source, "data", "response");
  if (!Array.isArray(rawData)) {
    fail("response.data", "expected an array");
  }
  return {
    data: rawData.map((entry, index) => item(entry, `response.data[${index}]`)),
    next_cursor: nullableID(field(source, "next_cursor", "response"), "response.next_cursor"),
  };
}

function decodeObject<T>(value: unknown, item: Decoder<T>): T {
  const source = object(value, "response");
  return item(field(source, "data", "response"), "response.data");
}

export const user: Decoder<User> = (value) => decodeObject(value, decodeUser);
export const period: Decoder<Period> = (value) => decodeObject(value, decodePeriod);
export const rule: Decoder<Rule> = (value) => decodeObject(value, decodeRule);
export const renewal: Decoder<Renewal> = (value) => decodeObject(value, decodeRenewal);
export const category: Decoder<Category> = (value) => decodeObject(value, decodeCategory);
export const categories: Decoder<Page<Category>> = (value) => decodePage(value, decodeCategory);
export const service: Decoder<ServiceItem> = (value) => decodeObject(value, decodeService);
export const services: Decoder<Page<ServiceItem>> = (value) => decodePage(value, decodeService);

export const serviceOverview: Decoder<ServiceOverviewResponse> = (value) => {
  const source = object(value, "response");
  const data = object(field(source, "data", "response"), "response.data");
  const counts = object(field(data, "counts", "response.data"), "response.data.counts");
  const rawItems = field(data, "items", "response.data");
  if (!Array.isArray(rawItems)) fail("response.data.items", "expected an array");
  const decodedCounts: OverviewCounts = {
    total: nonNegativeInteger(field(counts, "total", "response.data.counts"), "response.data.counts.total"),
    due_soon: nonNegativeInteger(field(counts, "due_soon", "response.data.counts"), "response.data.counts.due_soon"),
    expired: nonNegativeInteger(field(counts, "expired", "response.data.counts"), "response.data.counts.expired"),
    no_period: nonNegativeInteger(field(counts, "no_period", "response.data.counts"), "response.data.counts.no_period"),
  };
  const items: ServiceOverviewItem[] = rawItems.map((entry, index) => {
    const path = `response.data.items[${index}]`;
    const item = object(entry, path);
    const primary = field(item, "primary_period", path);
    return {
      service: decodeService(field(item, "service", path), `${path}.service`),
      primary_period: primary === null ? null : decodePeriod(primary, `${path}.primary_period`),
    };
  });
  return {
    data: { counts: decodedCounts, items },
    next_cursor: nullableID(field(source, "next_cursor", "response"), "response.next_cursor"),
  };
};

export const reminder: Decoder<ReminderJob> = (value) => decodeObject(value, decodeReminder);
export const periods: Decoder<Page<Period>> = (value) => decodePage(value, decodePeriod);
export const rules: Decoder<Page<Rule>> = (value) => decodePage(value, decodeRule);
export const renewals: Decoder<Page<Renewal>> = (value) => decodePage(value, decodeRenewal);
export const reminders: Decoder<Page<ReminderJob>> = (value) => decodePage(value, decodeReminder);

export const noContent: Decoder<void> = (value) => {
  if (value !== undefined) {
    fail("response", "expected no content");
  }
};
