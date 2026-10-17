import { request } from "./client";
import { ApiError } from "./errors";
import {
  noContent,
  overview as decodeOverview,
  period as decodePeriod,
  periods as decodePeriods,
  reminder as decodeReminder,
  reminders as decodeReminders,
  renewal as decodeRenewal,
  renewals as decodeRenewals,
  rule as decodeRule,
  rules as decodeRules,
  sim as decodeSim,
  sims as decodeSims,
  user as decodeUser,
} from "./decode";
import type {
  ID,
  OverviewResponse,
  Page,
  Period,
  PeriodCreateInput,
  PeriodUpdateInput,
  ReminderJob,
  ReminderStatusFilter,
  Renewal,
  RenewalCreateInput,
  Rule,
  RuleCreateInput,
  RuleUpdateInput,
  Sim,
  SimCreateInput,
  SimUpdateInput,
  User,
  UserUpdateInput,
} from "./types";

/** Server accepts 1-100; 20 matches the documented first-screen page. */
const PAGE_SIZE = 20;

function assertCursorAdvance(after: ID, next: ID | null): void {
  if (next !== null && BigInt(next) <= BigInt(after)) {
    throw new ApiError("pagination", "Pagination did not advance.");
  }
}

/** Loads a bounded collection to completion; a stalled cursor is a protocol error. */
export async function fetchAllPages<T>(load: (after: ID | null) => Promise<Page<T>>): Promise<T[]> {
  const items: T[] = [];
  let after: ID | null = null;
  for (;;) {
    const page = await load(after);
    items.push(...page.data);
    if (page.next_cursor === null) return items;
    if (after !== null) assertCursorAdvance(after, page.next_cursor);
    after = page.next_cursor;
  }
}

function pageQuery(after: ID | null, extra?: Record<string, string>): string {
  const query = new URLSearchParams({ limit: String(PAGE_SIZE) });
  if (after !== null) query.set("after_id", after);
  for (const [key, value] of Object.entries(extra ?? {})) query.set(key, value);
  const text = query.toString();
  return text.length > 0 ? `?${text}` : "";
}

function entityPath(base: string, id: ID): string {
  return `${base}/${encodeURIComponent(id)}`;
}

async function getEntity<T>(path: string, signal: AbortSignal | undefined, decode: (value: unknown) => T): Promise<T> {
  return (await request(path, { signal }, decode)).data;
}

async function getPage<T>(path: string, after: ID | null, signal: AbortSignal | undefined, decode: (value: unknown) => Page<T>): Promise<Page<T>> {
  const page = (await request(path, { signal }, decode)).data;
  if (after !== null) assertCursorAdvance(after, page.next_cursor);
  return page;
}

async function writeEntity<T>(path: string, method: "POST" | "PATCH", body: unknown, signal: AbortSignal | undefined, decode: (value: unknown) => T): Promise<T> {
  return (await request(path, { method, body, signal }, decode)).data;
}

async function deleteEntity(path: string, signal: AbortSignal | undefined): Promise<void> {
  await request(path, { method: "DELETE", signal }, noContent);
}

export const api = {
  me: (signal?: AbortSignal): Promise<User> => getEntity("/me", signal, decodeUser),

  updateMe: (body: UserUpdateInput, signal?: AbortSignal): Promise<User> => writeEntity("/me", "PATCH", body, signal, decodeUser),

  overview: async (after: ID | null = null, signal?: AbortSignal): Promise<OverviewResponse> => {
    const result = (await request(`/overview${pageQuery(after)}`, { signal }, decodeOverview)).data;
    if (after !== null) assertCursorAdvance(after, result.next_cursor);
    return result;
  },

  sims: (after: ID | null = null, signal?: AbortSignal): Promise<Page<Sim>> => getPage(`/sims${pageQuery(after)}`, after, signal, decodeSims),

  sim: (id: ID, signal?: AbortSignal): Promise<Sim> => getEntity(entityPath("/sims", id), signal, decodeSim),

  createSim: (body: SimCreateInput, signal?: AbortSignal): Promise<Sim> => writeEntity("/sims", "POST", body, signal, decodeSim),

  updateSim: (id: ID, body: SimUpdateInput, signal?: AbortSignal): Promise<Sim> => writeEntity(entityPath("/sims", id), "PATCH", body, signal, decodeSim),

  archiveSim: (id: ID, signal?: AbortSignal): Promise<void> => deleteEntity(entityPath("/sims", id), signal),

  deleteSim: async (id: ID, signal?: AbortSignal): Promise<void> => {
    await request(`${entityPath("/sims", id)}/delete`, { method: "POST", signal }, noContent);
  },

  periods: (simID: ID, after: ID | null = null, signal?: AbortSignal): Promise<Page<Period>> =>
    getPage(`${entityPath("/sims", simID)}/periods${pageQuery(after)}`, after, signal, decodePeriods),

  period: (id: ID, signal?: AbortSignal): Promise<Period> => getEntity(entityPath("/periods", id), signal, decodePeriod),

  createPeriod: (simID: ID, body: PeriodCreateInput, signal?: AbortSignal): Promise<Period> =>
    writeEntity(`${entityPath("/sims", simID)}/periods`, "POST", body, signal, decodePeriod),

  updatePeriod: (id: ID, body: PeriodUpdateInput, signal?: AbortSignal): Promise<Period> =>
    writeEntity(entityPath("/periods", id), "PATCH", body, signal, decodePeriod),

  deletePeriod: async (id: ID, version: number, signal?: AbortSignal): Promise<void> => {
    await request(`${entityPath("/periods", id)}/delete?expected_cycle=${version}`, { method: "POST", signal }, noContent);
  },

  rules: (periodID: ID, after: ID | null = null, signal?: AbortSignal): Promise<Page<Rule>> =>
    getPage(`${entityPath("/periods", periodID)}/rules${pageQuery(after)}`, after, signal, decodeRules),

  rule: (id: ID, signal?: AbortSignal): Promise<Rule> => getEntity(entityPath("/rules", id), signal, decodeRule),

  createRule: (periodID: ID, body: RuleCreateInput, signal?: AbortSignal): Promise<Rule> =>
    writeEntity(`${entityPath("/periods", periodID)}/rules`, "POST", body, signal, decodeRule),

  updateRule: (id: ID, body: RuleUpdateInput, signal?: AbortSignal): Promise<Rule> =>
    writeEntity(entityPath("/rules", id), "PATCH", body, signal, decodeRule),

  disableRule: (id: ID, revision: number, signal?: AbortSignal): Promise<void> =>
    deleteEntity(`${entityPath("/rules", id)}?expected_revision=${revision}`, signal),

  renewals: (periodID: ID, after: ID | null = null, signal?: AbortSignal): Promise<Page<Renewal>> =>
    getPage(`${entityPath("/periods", periodID)}/renewals${pageQuery(after)}`, after, signal, decodeRenewals),

  renew: async (periodID: ID, body: RenewalCreateInput, signal?: AbortSignal): Promise<{ status: number; data: Renewal }> =>
    request(`${entityPath("/periods", periodID)}/renewals`, { method: "POST", body, signal }, decodeRenewal),

  reminders: (after: ID | null = null, status: ReminderStatusFilter = "all", signal?: AbortSignal): Promise<Page<ReminderJob>> =>
    getPage(`/reminders${pageQuery(after, { status })}`, after, signal, decodeReminders),

  reminder: (id: ID, signal?: AbortSignal): Promise<ReminderJob> => getEntity(entityPath("/reminders", id), signal, decodeReminder),

  cancelReminder: async (id: ID, signal?: AbortSignal): Promise<void> => {
    await request(`${entityPath("/reminders", id)}/cancel`, { method: "POST", signal }, noContent);
  },
};

export type Api = typeof api;
