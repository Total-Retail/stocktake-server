const API_BASE = '/api/v1'

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('st_token') : null
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(options.headers ?? {}),
    },
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `Request failed: ${res.status}`)
  }
  return res.json()
}

// ── Auth ──────────────────────────────────────────────────────────────────────
export const auth = {
  login: (username: string, password: string) =>
    request<{ token: string; admin_id: string; is_super_admin: boolean }>('/auth/admin/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
}

// ── Stores ────────────────────────────────────────────────────────────────────
export const stores = {
  list: () => request<import('@/types').Store[]>('/stores'),
  get: (id: string) => request<import('@/types').Store>(`/stores/${id}`),
  create: (data: Partial<import('@/types').Store>) =>
    request<import('@/types').Store>('/stores', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: string, data: Partial<import('@/types').Store>) =>
    request<import('@/types').Store>(`/stores/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  getLayout: (id: string) => request<import('@/types').StoreLayout>(`/stores/${id}/layout`),
  createArea: (storeId: string, data: Partial<import('@/types').Area>) =>
    request<import('@/types').Area>(`/stores/${storeId}/areas`, { method: 'POST', body: JSON.stringify(data) }),
  createAisle: (storeId: string, data: Partial<import('@/types').Aisle>) =>
    request<import('@/types').Aisle>(`/stores/${storeId}/aisles`, { method: 'POST', body: JSON.stringify(data) }),
  createBin: (storeId: string, data: Partial<import('@/types').Bin>) =>
    request<import('@/types').Bin>(`/stores/${storeId}/bins`, { method: 'POST', body: JSON.stringify(data) }),
  labelsUrl: (id: string) => `${API_BASE}/stores/${id}/labels`,
  allLabelsUrl: (id: string) => `${API_BASE}/stores/${id}/labels`,
  binLabelUrl: (id: string, binId: string) => `${API_BASE}/stores/${id}/bins/${binId}/label`,
}

// ── Sessions ──────────────────────────────────────────────────────────────────
export const sessions = {
  list: (storeId?: string) =>
    request<import('@/types').Session[]>(`/sessions${storeId ? `?store_id=${storeId}` : ''}`),
  get: (id: string) => request<import('@/types').Session>(`/sessions/${id}`),
  create: (data: Partial<import('@/types').Session>) =>
    request<import('@/types').Session>('/sessions', { method: 'POST', body: JSON.stringify(data) }),
  updateStatus: (id: string, status: string) =>
    request(`/sessions/${id}/status`, { method: 'PUT', body: JSON.stringify({ status }) }),
  abort: (id: string, reason: string) =>
    request(`/sessions/${id}/abort`, { method: 'POST', body: JSON.stringify({ reason }) }),
  reopen: (id: string) =>
    request(`/sessions/${id}/reopen`, { method: 'POST' }),
  listCounters: (id: string) => request<import('@/types').Counter[]>(`/sessions/${id}/counters`),
  addCounter: (id: string, name: string, mobile: string) =>
    request<import('@/types').Counter>(`/sessions/${id}/counters`, {
      method: 'POST',
      body: JSON.stringify({ name, mobile }),
    }),
  removeCounter: (id: string, counterId: string) =>
    request(`/sessions/${id}/counters/${counterId}`, { method: 'DELETE' }),
  resendOtp: (id: string, counterId: string) =>
    request(`/sessions/${id}/counters/${counterId}/resend-otp`, { method: 'POST' }),
  pullTheoretical: (id: string) =>
    request(`/sessions/${id}/pull-theoretical`, { method: 'POST' }),
  submit: (id: string) =>
    request<{ export_url?: string }>(`/sessions/${id}/submit`, { method: 'POST' }),
  updateWorksheet: (id: string, worksheetSeqNo: number) =>
    request<import('@/types').Session>(`/sessions/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ worksheet_seq_no: worksheetSeqNo }),
    }),
  downloadExport: (id: string) => `${API_BASE}/sessions/${id}/export`,
}

// ── Variance & Audit ──────────────────────────────────────────────────────────
export const variance = {
  getConsolidated: (sessionId: string) =>
    request<import('@/types').ConsolidatedLine[]>(`/sessions/${sessionId}/consolidated`),
  getAudit: (sessionId: string) =>
    request<import('@/types').AuditLine[]>(`/sessions/${sessionId}/audit`),
  getReport: (sessionId: string) =>
    request<import('@/types').ConsolidatedLine[]>(`/sessions/${sessionId}/variance-report`),
  getFlags: (sessionId: string) =>
    request<import('@/types').VarianceFlag[]>(`/sessions/${sessionId}/variance-flags`),
  flagItems: (sessionId: string, itemNos: string[]) =>
    request(`/sessions/${sessionId}/variance-flags`, {
      method: 'POST',
      body: JSON.stringify({ item_nos: itemNos }),
    }),
  updateFlag: (sessionId: string, flagId: string, decision: string, notes?: string) =>
    request(`/sessions/${sessionId}/variance-flags/${flagId}`, {
      method: 'PUT',
      body: JSON.stringify({ decision, notes }),
    }),
}

// ── Settings ──────────────────────────────────────────────────────────────────
export const settings = {
  listVariance: () =>
    request<import('@/types').VarianceSetting[]>('/settings/variance'),
  updateVariance: (type: string, tolerancePct: number) =>
    request<import('@/types').VarianceSetting>(`/settings/variance/${type}`, {
      method: 'PUT',
      body: JSON.stringify({ tolerance_pct: tolerancePct }),
    }),
}

// ── Reporting ─────────────────────────────────────────────────────────────────
export const reporting = {
  getSummary: (sessionId: string) =>
    request<import('@/types').SessionSummary>(`/sessions/${sessionId}/performance`),
  getCounterPerformance: (sessionId: string) =>
    request<import('@/types').CounterPerformance[]>(`/sessions/${sessionId}/counter-performance`),
  getCounterDetail: (sessionId: string, counterId: string) =>
    request<import('@/types').CounterPerformance>(`/sessions/${sessionId}/counter-performance/${counterId}`),
}

// ── Admin users ───────────────────────────────────────────────────────────────
export const adminUsers = {
  list: () => request<import('@/types').AdminUser[]>('/admin/users'),
  create: (data: { username: string; password: string; full_name: string; is_super_admin?: boolean }) =>
    request<import('@/types').AdminUser>('/admin/users', { method: 'POST', body: JSON.stringify(data) }),
  deactivate: (id: string) =>
    request(`/admin/users/${id}/deactivate`, { method: 'PUT' }),
  resetPassword: (id: string, password: string) =>
    request(`/admin/users/${id}/password`, { method: 'PUT', body: JSON.stringify({ password }) }),
}

// ── LS Integration ────────────────────────────────────────────────────────────
export const ls = {
  worksheets: () =>
    request<Array<{
      worksheet_seq_no: number
      description: string
      store_no: string
      no_of_lines: number
    }>>('/ls/worksheets'),

  stores: () =>
    request<Array<{ code: string; name: string }>>('/ls/stores'),
}
