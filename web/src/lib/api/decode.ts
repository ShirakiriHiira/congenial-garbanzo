import type {
  ID,
  OverviewCounts,
  OverviewItem,
  OverviewResponse,
  Page,
  Period,
  ReminderJob,
  Renewal,
  Rule,
  Sim,
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

function decodeSim(value: unknown, path = "data"): Sim {
  const source = object(value, path);
  return {
    id: id(field(source, "id", path), `${path}.id`),
    user_id: id(field(source, "user_id", path), `${path}.user_id`),
    name: text(field(source, "name", path), `${path}.name`),
    card_type: oneOf(field(source, "card_type", path), ["physical_sim", "esim"], `${path}.card_type`),
    operator: nullableText(field(source, "operator", path), `${path}.operator`),
    phone_number: nullableText(field(source, "phone_number", path), `${path}.phone_number`),
    country_or_region: nullableText(field(source, "country_or_region", path), `${path}.country_or_region`),
    iccid: nullableText(field(source, "iccid", path), `${path}.iccid`),
    notes: nullableText(field(source, "notes", path), `${path}.notes`),
    status: oneOf(field(source, "status", path), ["active", "archived"], `${path}.status`),
    created_at: timestamp(field(source, "created_at", path), `${path}.created_at`),
    updated_at: timestamp(field(source, "updated_at", path), `${path}.updated_at`),
  };
}

function decodePeriod(value: unknown, path = "data"): Period {
  const source = object(value, path);
  return {
    id: id(field(source, "id", path), `${path}.id`),
    sim_id: id(field(source, "sim_id", path), `${path}.sim_id`),
    period_type: oneOf(field(source, "period_type", path), ["number_validity", "plan_validity", "data_package_validity", "custom"], `${path}.period_type`),
    name: text(field(source, "name", path), `${path}.name`),
    starts_at: nullableTimestamp(field(source, "starts_at", path), `${path}.starts_at`),
    expires_at: timestamp(field(source, "expires_at", path), `${path}.expires_at`),
    date_precision: oneOf(field(source, "date_precision", path), ["date", "datetime"], `${path}.date_precision`),
    local_expires_date: field(source, "local_expires_date", path) === null ? null : date(field(source, "local_expires_date", path), `${path}.local_expires_date`),
    timezone: text(field(source, "timezone", path), `${path}.timezone`),
    status: oneOf(field(source, "status", path), ["active", "expired", "cancelled"], `${path}.status`),
    cycle: nonNegativeInteger(field(source, "cycle", path), `${path}.cycle`),
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
    validity_period_id: id(field(source, "validity_period_id", path), `${path}.validity_period_id`),
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
    validity_period_id: id(field(source, "validity_period_id", path), `${path}.validity_period_id`),
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
    validity_period_id: id(field(source, "validity_period_id", path), `${path}.validity_period_id`),
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
export const sim: Decoder<Sim> = (value) => decodeObject(value, decodeSim);
export const period: Decoder<Period> = (value) => decodeObject(value, decodePeriod);
export const rule: Decoder<Rule> = (value) => decodeObject(value, decodeRule);
export const renewal: Decoder<Renewal> = (value) => decodeObject(value, decodeRenewal);
export const reminder: Decoder<ReminderJob> = (value) => decodeObject(value, decodeReminder);
export const sims: Decoder<Page<Sim>> = (value) => decodePage(value, decodeSim);
export const periods: Decoder<Page<Period>> = (value) => decodePage(value, decodePeriod);
export const rules: Decoder<Page<Rule>> = (value) => decodePage(value, decodeRule);
export const renewals: Decoder<Page<Renewal>> = (value) => decodePage(value, decodeRenewal);
export const reminders: Decoder<Page<ReminderJob>> = (value) => decodePage(value, decodeReminder);

export const overview: Decoder<OverviewResponse> = (value) => {
  const source = object(value, "response");
  const data = object(field(source, "data", "response"), "response.data");
  const counts = object(field(data, "counts", "response.data"), "response.data.counts");
  const rawItems = field(data, "items", "response.data");
  if (!Array.isArray(rawItems)) {
    fail("response.data.items", "expected an array");
  }
  const decodedCounts: OverviewCounts = {
    total: nonNegativeInteger(field(counts, "total", "response.data.counts"), "response.data.counts.total"),
    due_soon: nonNegativeInteger(field(counts, "due_soon", "response.data.counts"), "response.data.counts.due_soon"),
    expired: nonNegativeInteger(field(counts, "expired", "response.data.counts"), "response.data.counts.expired"),
    no_period: nonNegativeInteger(field(counts, "no_period", "response.data.counts"), "response.data.counts.no_period"),
  };
  const items: OverviewItem[] = rawItems.map((entry, index) => {
    const item = object(entry, `response.data.items[${index}]`);
    const primary = field(item, "primary_period", `response.data.items[${index}]`);
    return {
      sim: decodeSim(field(item, "sim", `response.data.items[${index}]`), `response.data.items[${index}].sim`),
      primary_period: primary === null ? null : decodePeriod(primary, `response.data.items[${index}].primary_period`),
    };
  });
  return {
    data: { counts: decodedCounts, items },
    next_cursor: nullableID(field(source, "next_cursor", "response"), "response.next_cursor"),
  };
};

export const noContent: Decoder<void> = (value) => {
  if (value !== undefined) {
    fail("response", "expected no content");
  }
};
