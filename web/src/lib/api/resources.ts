import { request } from "./client";
import { ApiError } from "./errors";
import {
  categories as decodeCategories,
  category as decodeCategory,
  noContent,
  period as decodePeriod,
  periods as decodePeriods,
  reminder as decodeReminder,
  reminders as decodeReminders,
  renewal as decodeRenewal,
  renewals as decodeRenewals,
  rule as decodeRule,
  rules as decodeRules,
  service as decodeService,
  serviceOverview as decodeServiceOverview,
  services as decodeServices,
  sim as decodeSim,
  user as decodeUser,
} from "./decode";
import type {
  Category,
  CategoryNameInput,
  ID,
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
  ServiceCreateInput,
  ServiceFilter,
  ServiceItem,
  ServiceOverviewResponse,
  ServiceUpdateInput,
  Sim,
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

function serviceQuery(after: ID | null, filter: ServiceFilter = {}): string {
  const query = new URLSearchParams({ limit: String(PAGE_SIZE) });
  if (after !== null) query.set("after_id", after);
  if (filter.q !== undefined && filter.q !== "") query.set("q", filter.q);
  if (filter.category_id !== undefined && filter.category_id !== "") query.set("category_id", filter.category_id);
  if (filter.status !== undefined) query.set("status", filter.status);
  return `?${query.toString()}`;
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

  categories: (after: ID | null = null, signal?: AbortSignal): Promise<Page<Category>> =>
    getPage(`/categories${pageQuery(after)}`, after, signal, decodeCategories),

  createCategory: (body: CategoryNameInput, signal?: AbortSignal): Promise<Category> =>
    writeEntity("/categories", "POST", body, signal, decodeCategory),

  updateCategory: (id: ID, body: CategoryNameInput, signal?: AbortSignal): Promise<Category> =>
    writeEntity(entityPath("/categories", id), "PATCH", body, signal, decodeCategory),

  services: (after: ID | null = null, filter: ServiceFilter = {}, signal?: AbortSignal): Promise<Page<ServiceItem>> =>
    getPage(`/services${serviceQuery(after, filter)}`, after, signal, decodeServices),

  serviceOverview: async (after: ID | null = null, filter: ServiceFilter = {}, signal?: AbortSignal): Promise<ServiceOverviewResponse> => {
    const result = (await request(`/services/overview${serviceQuery(after, filter)}`, { signal }, decodeServiceOverview)).data;
    if (after !== null) assertCursorAdvance(after, result.next_cursor);
    return result;
  },

  service: (id: ID, signal?: AbortSignal): Promise<ServiceItem> => getEntity(entityPath("/services", id), signal, decodeService),

  createService: (body: ServiceCreateInput, signal?: AbortSignal): Promise<ServiceItem> =>
    writeEntity("/services", "POST", body, signal, decodeService),

  updateService: (id: ID, body: ServiceUpdateInput, signal?: AbortSignal): Promise<ServiceItem> =>
    writeEntity(entityPath("/services", id), "PATCH", body, signal, decodeService),

  archiveService: (id: ID, signal?: AbortSignal): Promise<void> => deleteEntity(entityPath("/services", id), signal),

  deleteService: async (id: ID, signal?: AbortSignal): Promise<void> => {
    await request(`${entityPath("/services", id)}/delete`, { method: "POST", signal }, noContent);
  },

  sim: (id: ID, signal?: AbortSignal): Promise<Sim> => getEntity(entityPath("/sims", id), signal, decodeSim),

  periods: (serviceID: ID, after: ID | null = null, signal?: AbortSignal): Promise<Page<Period>> =>
    getPage(`${entityPath("/services", serviceID)}/periods${pageQuery(after)}`, after, signal, decodePeriods),

  period: (id: ID, signal?: AbortSignal): Promise<Period> => getEntity(entityPath("/periods", id), signal, decodePeriod),

  createPeriod: (serviceID: ID, body: PeriodCreateInput, signal?: AbortSignal): Promise<Period> =>
    writeEntity(`${entityPath("/services", serviceID)}/periods`, "POST", body, signal, decodePeriod),

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

  deleteRule: async (id: ID, revision: number, signal?: AbortSignal): Promise<void> => {
    await request(`${entityPath("/rules", id)}/delete?expected_revision=${revision}`, { method: "POST", signal }, noContent);
  },

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
