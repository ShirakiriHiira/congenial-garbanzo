import { ApiError, type ApiErrorKind } from '../api/errors';
import type { CardType, IntervalUnit, Period, PeriodStatus, ReminderStatus, ServiceStatus, TermsMode } from '../api/types';

export const labels = {
  appName: 'SERVICE MANAGER',
  title: '服务与订阅',
  openTelegram: '请从 Telegram 内打开',
  expired: '会话已过期，请关闭并重新打开 Mini App',
  forbidden: '你没有访问权限，请联系管理员。',
  loading: '正在加载…',
  accountFailed: '账户信息加载失败，当前仅可浏览。',
  loadFailed: '加载失败，请重试。',
  retry: '重试',
  refresh: '刷新',
  total: '全部服务',
  dueSoon: '即将到期',
  expiredCount: '已过期',
  cardList: '我的服务',
  emptyCards: '还没有服务。',
  noPeriod: '未设置有效期',
  loadMore: '加载更多',
  notFound: '此页面不存在',
  home: '返回首页',
  back: '返回',
  leaveConfirm: '尚有未保存的修改，确定离开吗？',
  readOnly: '当前为只读模式。',
  settings: '设置',
  statistics: '服务统计',
  filter: '筛选',
  filterActive: '当前服务',
  filterAllWithArchived: '全部（含归档）',
  filterArchived: '已归档',
  filterNoPeriod: '未设置有效期',
  search: '搜索',
  searchPlaceholder: '搜索名称、服务商或公开标识',
  category: '类别',
  allCategories: '全部类别',
  filterEmpty: '没有符合条件的服务。',
  remainingDays: (days: number) => `剩余 ${days} 天`,
  addCard: '添加服务',
  close: '关闭',
  cardsTotal: (total: number | string) => `共 ${total} 项服务`,
  deletePeriod: '删除有效期',
  deletePeriodTitle: '永久删除这个有效期？',
  deletePeriodMessage: (name: string) => `将永久删除「${name}」及其全部提醒规则、提醒任务和续期历史，无法恢复；服务和其他有效期不受影响。`,
  deletePeriodConfirm: '确认永久删除有效期',
  periodDeleted: '有效期及其关联数据已永久删除。',
  deleteConflict: '删除未完成，请核对最新值后重新确认。',
  deleteConflictFailed: '读取最新有效期失败，删除已暂停。',
} as const;

export const cardTypeLabels: Record<CardType, string> = { physical_sim: '实体 SIM', esim: 'eSIM' };
export const serviceStatusLabels: Record<ServiceStatus, string> = { active: '使用中', archived: '已归档' };
export const termsModeLabels: Record<TermsMode, string> = { once: '一次性', recurring: '周期性', custom: '暂不设置' };
export const intervalUnitLabels: Record<IntervalUnit, string> = { day: '天', week: '周', month: '月', year: '年' };
export const periodStatusLabels: Record<PeriodStatus, string> = { active: '有效', expired: '已过期' };
// SIM services have no generic identifier: their public facts live in
// sim_details, so the map covers the remaining categories only.
export const serviceFieldLabels: Record<string, { provider: string; identifier: string }> = {
  vps: { provider: '服务商', identifier: '主机名或 IP' },
  domain: { provider: '注册商', identifier: '域名' },
  subscription: { provider: '服务商', identifier: '账号标识' },
  other: { provider: '服务商', identifier: '公开标识' },
};
export function fieldLabels(code: string | null | undefined): { provider: string; identifier: string } {
  return (code && serviceFieldLabels[code]) || serviceFieldLabels.other;
}
export const reminderStatusLabels: Record<ReminderStatus, string> = {
  pending: '待发送', sending: '发送中', sent: '已发送', failed: '发送失败', cancelled: '已取消',
};
export const errorLabels: Record<ApiErrorKind, string> = {
  auth: labels.expired,
  forbidden: labels.forbidden,
  validation: '提交内容无效，请检查后重试。',
  'not-found': '记录不存在或已不可访问。',
  conflict: '记录已变化，请刷新后重试。',
  payload: '内容过大，请缩短后重试。',
  'unsupported-media': '请求格式不受支持，请重新打开。',
  server: '服务暂时无法完成请求，请稍后重试。',
  unavailable: '服务暂时不可用，请稍后重试。',
  network: '网络连接失败，请检查网络后重试。',
  timeout: '请求超时，请重试。',
  pagination: '分页数据异常，请刷新后重试。',
};

/** Do not present raw backend/network messages, which may include sensitive details. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) return errorLabels[error.kind];
  if (error instanceof DOMException && error.name === 'AbortError') return '';
  return labels.loadFailed;
}

export function periodLabel(period: Pick<Period, 'name'>): string {
  return period.name.trim() || '有效期';
}

export const settingsLabels = {
  currentTimezone: '当前时区',
  privateChat: '机器人私聊绑定',
  bound: '已绑定私聊',
  unbound: '尚未绑定私聊',
  bindHint: '请先向此机器人私聊发送 /start，再刷新绑定状态',
  refreshBinding: '刷新绑定状态',
  saveTimezone: '保存时区',
  invalidTimezone: '请输入有效的 IANA 时区（不能使用 Local）。',
  confirmTimezone: '确认修改时区',
  timezoneImpact: '仅日期截止会按新时区重新计算，精确时间的实际时刻不变；未发送的提醒会被取消。',
  confirmSave: '确认修改',
  reminders: '提醒记录',
  sessionValidity: '会话有效期',
  saveSession: '保存会话有效期',
} as const;

export const sessionTTLOptions = [
  { value: '3600', label: '1 小时' },
  { value: '21600', label: '6 小时' },
  { value: '86400', label: '24 小时' },
  { value: '604800', label: '7 天' },
] as const;

export const DEFAULT_SESSION_TTL = 86400;

export const remindersLabels = {
  title: '提醒记录',
  filter: '提醒状态',
  empty: '暂无符合条件的提醒记录。',
  period: (_id: string) => `有效期`,
  task: (_id: string) => `提醒`,
  scheduled: '计划发送',
  sent: '实际发送',
  attempts: '尝试次数',
  errorCategory: '失败类别',
  detail: '查看详情',
  hideDetail: '收起详情',
  cancel: '取消提醒',
  cancelTitle: '取消这次提醒？',
  cancelConfirm: '确认取消',
  cancelMessage: '只取消这一次，提醒规则保留。',
  sendingWarning: '提醒正在发送，可能已经送达。',
  cancelled: '这次提醒已取消。',
  changed: (status: string) => `取消未完成，提醒当前状态：${status}。请按最新状态操作。`,
  conflictRefreshFailed: '提醒状态已变化，但读取最新状态失败。请先重试刷新详情。',
  partial: '后续记录加载失败，已保留当前记录。',
  refreshFailed: '刷新失败，已保留当前记录。',
  refreshing: '正在刷新…',
  zone: '显示时区',
  snapshot: '到期时间快照',
  nextAttempt: '下次尝试',
  created: '创建时间',
  updated: '更新时间',
  deliveryError: '消息投递失败',
  networkError: '网络或连接异常',
  rateLimitError: '发送频率受限',
  permissionError: '投递被 Telegram 拒绝',
  noPrivateChatError: '尚未绑定私聊，无法发送消息',
  unknownError: '发送异常，请稍后刷新查看',
} as const;

export const renewalLabels = {
  title: '续期',
  previousExpiry: '原截止',
  expiryInstant: '截止时刻',
  timezone: '时区',
  terms: '约定条款',
  dateUnavailable: '日期不可用',
  invalidDate: '请输入有效的截止日期',
  laterExpiry: '新截止必须晚于原截止和当前时间，请选择更晚的日期或时间。',
  notes: '备注',
  clearNotes: '清除备注',
  notesTooLong: '备注不能超过 4000 个字符。',
  invalidForm: '请检查日期、金额和备注。',
  submit: '确认续期',
  submitting: '正在提交…',
  success: '续期成功',
  replayed: '该周期已提交过相同续期，已载入原记录',
  history: '续期历史',
  noHistory: '暂无续期记录。',
  renewedAt: '续期时间',
  newExpiry: '新截止',
  conflictTitle: '数据已被其他操作更新',
  conflictMessage: '草稿已保留。请核对最新值后重新提交。',
  conflictPending: '请先读取最新数据。',
  reloadLatest: '重试获取最新值',
  editLatest: '基于最新值重新编辑',
  currentExpiry: '服务端截止',
  conflictReloadFailed: '无法读取最新有效期，草稿已保留。',
} as const;

export const rulesLabels = {
  title: '提醒规则',
  noRules: '还没有提醒规则。',
  create: '添加规则',
  edit: '编辑规则',
  days: '提前天数（0 表示当天）',
  daysInvalid: '提前天数必须是安全范围内的非负整数。',
  time: '提醒时间',
  timeInvalid: '提醒时间必须为 HH:MM（00:00–23:59）。',
  enabled: '启用此规则',
  enabledState: '已启用',
  disabledState: '已禁用',
  enable: '启用',
  disable: '禁用',
  save: '保存规则',
  cancelEdit: '取消编辑',
  remove: '删除',
  deleteTitle: '删除提醒规则？',
  deleteMessage: '将永久删除这条规则及其提醒任务，无法恢复。',
  created: '规则已添加',
  updated: '规则已更新',
  deleted: '规则已删除。',
  conflictTitle: '提醒规则发生冲突',
  conflictMessage: '规则已变化，或相同提前天数和提醒时间的规则已存在；已禁用规则仍占用此组合。草稿已保留。',
  conflictContinue: '基于最新版本继续编辑',
  conflictDeferred: '冲突尚未确认，请查看最新规则后继续；草稿已保留。',
  conflictFailed: '无法读取最新规则，写入已暂停。',
  latest: '服务端最新规则',
  reloadRequired: '操作已完成，但最新规则读取失败。请刷新后再操作。',
  partial: '规则列表加载不完整，已保留已加载的规则。',
  summary: (days: number, time: string) => `提前 ${days} 天 ${time}`,
  detail: (days: number, time: string) => `提前 ${days} 天 · ${time}`,
} as const;
