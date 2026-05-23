export interface Store {
  id: string
  store_code: string
  store_name: string
  ls_store_code: string
  location_code: string | null
  active: boolean
}

export interface Area {
  id: string
  store_id: string
  area_code: string
  area_name: string
}

export interface Aisle {
  id: string
  area_id: string
  aisle_code: string
  aisle_name: string
}

export interface Bin {
  id: string
  aisle_id: string
  bin_code: string
  bin_name: string
  barcode: string
  active: boolean
}

export type SessionStatus =
  | 'DRAFT'
  | 'ACTIVE'
  | 'PENDING_REVIEW'
  | 'POSTED'
  | 'REOPENED'
  | 'ABORTED'

export interface Session {
  id: string
  store_id: string
  stock_count_date: string
  type: string
  status: SessionStatus
  worksheet_no: string | null
  document_number: string | null
  document_checked_at: string | null
  export_file_path: string | null
  abort_reason: string | null
  aborted_at: string | null
  aborted_by: string | null
  created_by: string
  created_at: string
}

export interface Counter {
  id: string
  name: string
  mobile_number: string
  created_at: string
}

export interface TheoreticalStock {
  session_id: string
  item_no: string
  theoretical_qty: number
  unit_cost: number
}

// Counting
export interface CountLine {
  id: string
  session_id: string
  bin_id: string
  item_no: string
  counter_id: string
  quantity: number
  counted_at: string
  synced_at: string
  round_no: number
  client_uuid: string
}

export interface BinSubmission {
  id: string
  session_id: string
  bin_id: string
  counter_id: string
  submitted_at: string
}

// Variance & Audit
export type FlagStatus = 'PENDING' | 'ACCEPTED' | 'REJECTED'

export interface ConsolidatedLine {
  item_no: string
  description: string
  counted_qty: number
  theoretical_qty: number
  variance: number
  variance_pct: number
  unit_cost: number
  variance_cost: number
  flagged: boolean
}

export interface AuditLine {
  item_no: string
  description: string
  bin_code: string
  counter_name: string
  quantity: number
  round_no: number
  counted_at: string
}

export interface VarianceFlag {
  id: string
  session_id: string
  item_no: string
  flagged_by: string
  flagged_at: string
  status: FlagStatus
}

// Settings
export interface VarianceSetting {
  id: string
  stock_count_type: string
  tolerance_pct: number
  updated_by: string | null
  updated_at: string
}

// Reporting
export interface CounterPerformance {
  counter_id: string
  counter_name: string
  mobile: string
  items_counted: number
  bins_completed: number
  recount_rate_pct: number
  recount_accepted: number
  recount_rejected: number
  last_activity: string
}

export interface HourlyActivity {
  counter_id: string
  hour: number
  count: number
}

export interface SessionSummary {
  session_id: string
  total_items: number
  total_bins: number
  bins_completed: number
  total_counts: number
  counters: CounterPerformance[]
  hourly_activity: HourlyActivity[]
}

// Admin users
export interface AdminUser {
  id: string
  username: string
  full_name: string
  active: boolean
  is_super_admin: boolean
  created_at: string
}

// WebSocket events
export type WSEventType =
  | 'count.submitted'
  | 'bin.completed'
  | 'counter.connected'
  | 'counter.disconnected'
  | 'session.status_changed'

export interface WSEvent {
  type: WSEventType
  session_id: string
  payload: Record<string, unknown>
}

export interface StoreLayout {
  areas: Area[]
  aisles: Aisle[]
  bins: Bin[]
}

// ── Helper maps ────────────────────────────────────────────────────────────────

export const SESSION_TYPE_LABELS: Record<string, string> = {
  FLOOR:      'Floor',
  BAKERY:     'Bakery',
  BUTCHERY:   'Butchery',
  FRUIT_VEG:  'Fruit & Veg',
  DELI_COLD:  'Deli Cold',
  DELI_HOT:   'Deli Hot',
  QSR:        'QSR',
  RESTAURANT: 'Restaurant',
  PARTIAL:    'Partial',
}

export const SESSION_STATUS_LABELS: Record<SessionStatus, string> = {
  DRAFT:          'Draft',
  ACTIVE:         'Active',
  PENDING_REVIEW: 'Pending Review',
  POSTED:         'Posted',
  REOPENED:       'Reopened',
  ABORTED:        'Aborted',
}

export const SESSION_STATUS_COLOURS: Record<SessionStatus, string> = {
  DRAFT:          'bg-gray-100 text-gray-700',
  ACTIVE:         'bg-blue-100 text-blue-700',
  PENDING_REVIEW: 'bg-yellow-100 text-yellow-800',
  POSTED:         'bg-green-100 text-green-700',
  REOPENED:       'bg-orange-100 text-orange-700',
  ABORTED:        'bg-red-100 text-red-700',
}
