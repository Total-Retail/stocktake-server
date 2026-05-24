'use client'

import { useEffect, useState } from 'react'
import { useParams } from 'next/navigation'
import { sessions as sessionsApi, stores, ls } from '@/lib/api'
import type { Session, Counter, Store, SessionStatus } from '@/types'
import { SESSION_TYPE_LABELS, SESSION_STATUS_LABELS, SESSION_STATUS_COLOURS } from '@/types'
import { Button, Card, CardBody, CardHeader, Spinner, Empty, StatCard } from '@/components/ui'

type Worksheet = {
  worksheet_seq_no: number
  description: string
  store_no: string
  no_of_lines: number
}

const STATUS_ACTIONS: Record<string, { label: string; next: string; variant: 'primary' | 'danger' | 'secondary' }[]> = {
  DRAFT:          [{ label: 'Activate',                               next: 'ACTIVE',               variant: 'primary'   }],
  ACTIVE:         [{ label: 'Complete counting',                       next: 'complete_and_pull',    variant: 'primary'   }],
  PENDING_REVIEW: [{ label: 'Submit to LS',                           next: 'submit',               variant: 'primary'   }],
  POSTED:         [],
  REOPENED:       [{ label: 'Complete counting',                       next: 'complete_and_pull',    variant: 'primary'   }],
  ABORTED:        [],
}

const EDITABLE_STATUSES = ['DRAFT', 'ACTIVE', 'REOPENED']

export default function SessionOverviewPage() {
  const { id } = useParams<{ id: string }>()
  const [session, setSession]   = useState<Session | null>(null)
  const [store, setStore]       = useState<Store | null>(null)
  const [counters, setCounters] = useState<Counter[]>([])
  const [allWorksheets, setAllWorksheets] = useState<Worksheet[]>([])
  const [loading, setLoading]   = useState(true)

  const [actionLoading, setActionLoading] = useState(false)
  const [addLoading, setAddLoading]       = useState(false)
  const [resendingId, setResendingId]     = useState<string | null>(null)
  const [wsLoading, setWsLoading]         = useState(false)
  const [resyncLoading, setResyncLoading] = useState(false)
  const [abortLoading, setAbortLoading]   = useState(false)
  const [showAbortDialog, setShowAbortDialog] = useState(false)
  const [abortReason, setAbortReason]     = useState('')
  const [showSubmitDialog, setShowSubmitDialog] = useState(false)
  const [submitConfirmText, setSubmitConfirmText] = useState('')

  // selectedWorksheetSeqNo: 0 = none
  const [selectedWorksheetSeqNo, setSelectedWorksheetSeqNo] = useState(0)

  const [error, setError]     = useState('')
  const [success, setSuccess] = useState('')
  const [newCounter, setNewCounter] = useState({ name: '', mobile: '' })

  async function load() {
    const sess = await sessionsApi.get(id)
    setSession(sess)
    // worksheet_no is stored as a string "161" — parse back to int
    setSelectedWorksheetSeqNo(sess.worksheet_no ? parseInt(sess.worksheet_no, 10) : 0)
    const [st, ctrs] = await Promise.all([
      stores.get(sess.store_id),
      sessionsApi.listCounters(id),
    ])
    setStore(st)
    setCounters(ctrs ?? [])
    return st
  }

  useEffect(() => {
    load()
      .then((st) => {
        // Load worksheets filtered to this store
        ls.worksheets()
          .then((data: Worksheet[]) => {
            const filtered = data.filter(w => !st?.ls_store_code || w.store_no === st.ls_store_code)
            setAllWorksheets(filtered)
          })
          .catch(() => setAllWorksheets([]))
      })
      .finally(() => setLoading(false))
  }, [id])

  const worksheetDirty = selectedWorksheetSeqNo !== (session?.worksheet_no ? parseInt(session.worksheet_no, 10) : 0)

async function handleAction(action: string) {
  setActionLoading(true)
  setError('')
  setSuccess('')
  try {
    if (action === 'submit') {
      // Open confirmation dialog instead — handled by confirmSubmit
      setShowSubmitDialog(true)
      setActionLoading(false)
      return
    } else if (action === 'complete_and_pull') {
      await sessionsApi.pullTheoretical(id) // returns 202 — pull runs in background
      await sessionsApi.updateStatus(id, 'PENDING_REVIEW')
      setSuccess('Counting complete. Theoretical stock is syncing in the background — reload in a moment to see updated items.')
    } else {
      await sessionsApi.updateStatus(id, action)
      setSuccess('Session status updated.')
    }
    setSession(await sessionsApi.get(id))
  } catch (err: unknown) {
    setError(err instanceof Error ? err.message : 'Action failed')
  } finally {
    setActionLoading(false)
  }
}

async function handleAbort(e: React.FormEvent) {
  e.preventDefault()
  if (abortReason.trim().length < 10) { setError('Please provide a reason (at least 10 characters)'); return }
  setAbortLoading(true)
  setError('')
  setSuccess('')
  try {
    await sessionsApi.abort(id, abortReason)
    setShowAbortDialog(false)
    setAbortReason('')
    setSuccess('Session aborted.')
    setSession(await sessionsApi.get(id))
  } catch (err: unknown) {
    setError(err instanceof Error ? err.message : 'Abort failed')
  } finally {
    setAbortLoading(false)
  }
}

async function handleReopen() {
  setActionLoading(true)
  setError('')
  setSuccess('')
  try {
    await sessionsApi.reopen(id)
    setSuccess('Session reopened. LS worksheet lines have been cleared.')
    setSession(await sessionsApi.get(id))
  } catch (err: unknown) {
    setError(err instanceof Error ? err.message : 'Reopen failed')
  } finally {
    setActionLoading(false)
  }
}

async function confirmSubmit(e: React.FormEvent) {
  e.preventDefault()
  if (submitConfirmText !== 'SUBMIT') return
  setActionLoading(true)
  setError('')
  setSuccess('')
  try {
    const result = await sessionsApi.submit(id)
    setShowSubmitDialog(false)
    setSubmitConfirmText('')
    setSuccess('Session submitted to LS successfully.')
    setSession(await sessionsApi.get(id))
    if (result.export_url) {
      const token = localStorage.getItem('st_token')
      const res = await fetch(result.export_url, { headers: token ? { Authorization: `Bearer ${token}` } : {} })
      if (res.ok) {
        const blob = await res.blob()
        const objectUrl = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = objectUrl
        a.download = `stockcount-${id}.xlsx`
        a.click()
        setTimeout(() => URL.revokeObjectURL(objectUrl), 10000)
      }
    }
  } catch (err: unknown) {
    setError(err instanceof Error ? err.message : 'Submit failed')
  } finally {
    setActionLoading(false)
  }
}

  async function handleWorksheetSave() {
    setWsLoading(true)
    setError('')
    setSuccess('')
    try {
      const updated = await sessionsApi.updateWorksheet(id, selectedWorksheetSeqNo)
      setSession(updated)
      setSuccess(
        selectedWorksheetSeqNo > 0
          ? `Worksheet linked${updated.worksheet_no ? ' and theoreticals synced' : ''}.`
          : 'Worksheet unlinked.'
      )
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to update worksheet')
    } finally {
      setWsLoading(false)
    }
  }

  async function handleResync() {
    setResyncLoading(true)
    setError('')
    setSuccess('')
    try {
      await sessionsApi.pullTheoretical(id) // 202 — pull runs in background on server
      setSuccess('Sync started. Item barcodes and costs are being fetched from LS — reload in about 30 seconds to see the updated items.')
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Re-sync failed')
    } finally {
      setResyncLoading(false)
    }
  }

  async function handleAddCounter(e: React.FormEvent) {
    e.preventDefault()
    setAddLoading(true)
    setError('')
    setSuccess('')
    try {
      const counter = await sessionsApi.addCounter(id, newCounter.name, '+263' + newCounter.mobile.replace(/^\+?263/, ''))
      setCounters(prev => [...prev, counter])
      setNewCounter({ name: '', mobile: '' })
      setSuccess(`${newCounter.name} added.`)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to add counter')
    } finally {
      setAddLoading(false)
    }
  }

  async function handleRemoveCounter(counterId: string) {
    setError('')
    try {
      await sessionsApi.removeCounter(id, counterId)
      setCounters(prev => prev.filter(c => c.id !== counterId))
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to remove counter')
    }
  }

  async function handleResendOtp(counter: Counter) {
    setResendingId(counter.id)
    setError('')
    setSuccess('')
    try {
      await sessionsApi.resendOtp(id, counter.id)
      setSuccess(`OTP resent to ${counter.name} (${counter.mobile_number}).`)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to resend OTP')
    } finally {
      setResendingId(null)
    }
  }

  if (loading) return <div className="flex justify-center items-center h-64"><Spinner size="lg" /></div>
  if (!session) return <div className="p-6 text-gray-500">Session not found.</div>

  const actions = STATUS_ACTIONS[session.status] ?? []
  const canModifyCounters = session.status === 'DRAFT' || session.status === 'ACTIVE' || session.status === 'REOPENED'
  const canEditWorksheet  = EDITABLE_STATUSES.includes(session.status)
  const canAbort          = session.status === 'DRAFT' || session.status === 'ACTIVE' || session.status === 'PENDING_REVIEW' || session.status === 'REOPENED'

  // Find the display name for the currently linked worksheet
  const linkedWorksheet = allWorksheets.find(
    w => session.worksheet_no && w.worksheet_seq_no === parseInt(session.worksheet_no, 10)
  )

  return (
    <div className="p-6 space-y-6">
      {error   && <div className="bg-red-50 border border-red-200 text-red-700 text-sm px-4 py-3 rounded-lg">{error}</div>}
      {success && <div className="bg-teal-50 border border-teal-200 text-teal-700 text-sm px-4 py-3 rounded-lg">{success}</div>}

      {/* Stats */}
      <div className="grid grid-cols-2 lg:grid-cols-5 gap-4">
        <StatCard label="Store"    value={store?.store_name ?? '—'} />
        <StatCard label="Date"     value={session.stock_count_date} />
        <StatCard label="Type"     value={SESSION_TYPE_LABELS[session.type] ?? session.type} />
        <StatCard label="Status"   value={SESSION_STATUS_LABELS[session.status as SessionStatus] ?? session.status} />
        <StatCard label="Counters" value={String(counters.length)} />
      </div>

      {/* LS Worksheet */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold text-gray-700">LS Worksheet</h2>
            {session.worksheet_no && canEditWorksheet && (
              <Button size="sm" variant="secondary" loading={resyncLoading} onClick={handleResync}>
                Re-sync theoreticals
              </Button>
            )}
          </div>
        </CardHeader>
        <CardBody>
          {canEditWorksheet ? (
            <div className="flex gap-3 items-end">
              <div className="flex-1">
                <label className="block text-xs font-medium text-gray-500 mb-1">
                  Linked worksheet
                </label>
                {allWorksheets.length > 0 ? (
                  <select
                    value={selectedWorksheetSeqNo}
                    onChange={e => setSelectedWorksheetSeqNo(parseInt(e.target.value, 10))}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
                  >
                    <option value={0}>None — no theoreticals</option>
                    {allWorksheets.map(w => (
                      <option key={w.worksheet_seq_no} value={w.worksheet_seq_no}>
                        {w.description} ({w.no_of_lines} lines)
                      </option>
                    ))}
                  </select>
                ) : (
                  <p className="text-xs text-gray-400 py-2">
                    No worksheets available from LS for this store.
                  </p>
                )}
              </div>
              <Button
                size="sm"
                loading={wsLoading}
                disabled={!worksheetDirty}
                onClick={handleWorksheetSave}
              >
                Save
              </Button>
            </div>
          ) : (
            <div className="flex items-center gap-2 text-sm">
              <span className="text-gray-500">Linked worksheet:</span>
              {session.worksheet_no
                ? <span className="font-medium text-gray-900">
                    {linkedWorksheet ? linkedWorksheet.description : `#${session.worksheet_no}`}
                  </span>
                : <span className="text-gray-400 italic">None</span>
              }
            </div>
          )}
          {!session.worksheet_no && !canEditWorksheet && (
            <p className="text-xs text-amber-600 mt-2">
              No worksheet was linked before counting completed. Theoretical stock will be empty.
            </p>
          )}
        </CardBody>
      </Card>

      {/* Session actions */}
      {(actions.length > 0 || canAbort || session.status === 'POSTED') && (
        <Card>
          <CardHeader><h2 className="text-sm font-semibold text-gray-700">Actions</h2></CardHeader>
          <CardBody>
            <div className="flex gap-3 flex-wrap">
              {actions.map(a => (
                <Button
                  key={a.next}
                  variant={a.variant}
                  loading={actionLoading}
                  onClick={() => handleAction(a.next)}
                >
                  {a.label}
                </Button>
              ))}
              {session.status === 'POSTED' && (
                <Button variant="secondary" loading={actionLoading} onClick={handleReopen}>
                  Reopen session
                </Button>
              )}
              {canAbort && (
                <Button variant="danger" onClick={() => setShowAbortDialog(true)}>
                  Abort session
                </Button>
              )}
            </div>

            {/* Abort confirmation dialog */}
            {showAbortDialog && (
              <form onSubmit={handleAbort} className="mt-4 p-4 bg-red-50 border border-red-200 rounded-lg space-y-3">
                <p className="text-sm font-medium text-red-800">Abort this session?</p>
                <p className="text-xs text-red-600">
                  The session will be archived as ABORTED. This cannot be undone.
                </p>
                <textarea
                  value={abortReason}
                  onChange={e => setAbortReason(e.target.value)}
                  placeholder="Reason for aborting (required, at least 10 characters)"
                  required
                  minLength={10}
                  rows={3}
                  className="w-full px-3 py-2 border border-red-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-red-400"
                />
                <div className="flex gap-2">
                  <Button type="submit" variant="danger" size="sm" loading={abortLoading}>Confirm abort</Button>
                  <Button type="button" variant="secondary" size="sm" onClick={() => { setShowAbortDialog(false); setAbortReason('') }}>Cancel</Button>
                </div>
              </form>
            )}
          </CardBody>
        </Card>
      )}

      {/* Counters */}
      <Card>
        <CardHeader>
          <h2 className="text-sm font-semibold text-gray-700">Counters ({counters.length})</h2>
        </CardHeader>
        <CardBody className="p-0">
          {counters.length === 0 ? (
            <div className="px-4 py-6">
              <Empty message="No counters assigned yet." />
            </div>
          ) : (
            <table className="w-full text-sm">
              <thead className="bg-gray-50 border-b border-gray-100">
                <tr>
                  {['Name', 'Mobile', ...(canModifyCounters ? ['Actions'] : [])].map(h => (
                    <th key={h} className="px-4 py-2 text-left text-xs font-medium text-gray-500">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {counters.map(c => (
                  <tr key={c.id}>
                    <td className="px-4 py-3 font-medium text-gray-900">{c.name}</td>
                    <td className="px-4 py-3 text-gray-500">{c.mobile_number}</td>
                    {canModifyCounters && (
                      <td className="px-4 py-3">
                        <div className="flex gap-2">
                          <button
                            onClick={() => handleResendOtp(c)}
                            disabled={resendingId === c.id}
                            className="text-xs text-teal-600 hover:text-teal-800 disabled:opacity-50"
                          >
                            {resendingId === c.id ? 'Sending…' : 'Resend OTP'}
                          </button>
                          <button
                            onClick={() => handleRemoveCounter(c.id)}
                            className="text-xs text-red-500 hover:text-red-700"
                          >
                            Remove
                          </button>
                        </div>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardBody>
      </Card>

      {/* Add counter */}
      {canModifyCounters && (
        <Card>
          <CardHeader><h2 className="text-sm font-semibold text-gray-700">Add counter</h2></CardHeader>
          <CardBody>
            <form onSubmit={handleAddCounter} className="flex gap-3 flex-wrap items-end">
              <div>
                <label className="block text-xs font-medium text-gray-500 mb-1">Name</label>
                <input
                  value={newCounter.name}
                  onChange={e => setNewCounter(p => ({ ...p, name: e.target.value }))}
                  required
                  className="px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-500 mb-1">Mobile</label>
                <div className="flex items-center border border-gray-300 rounded-lg overflow-hidden focus-within:ring-2 focus-within:ring-teal-500">
                  <span className="px-3 py-2 bg-gray-100 text-sm text-gray-600 font-medium select-none border-r border-gray-300">+263</span>
                  <input
                    value={newCounter.mobile}
                    onChange={e => setNewCounter(p => ({ ...p, mobile: e.target.value.replace(/\D/g, '').slice(0, 9) }))}
                    required
                    placeholder="7XXXXXXXX"
                    maxLength={9}
                    pattern="\d{9}"
                    title="Enter 9-digit local number e.g. 771234567"
                    className="px-3 py-2 text-sm w-32 focus:outline-none"
                  />
                </div>
              </div>
              <Button type="submit" size="sm" loading={addLoading}>Add</Button>
            </form>
          </CardBody>
        </Card>
      )}

      {/* Submit confirmation modal */}
      {showSubmitDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-white rounded-xl shadow-2xl w-full max-w-md p-6 space-y-4">
            <div>
              <h2 className="text-lg font-semibold text-gray-900">Confirm submission</h2>
              <p className="text-sm text-gray-500 mt-1">
                This will post the final counts to LS and generate the Excel export.
                The session will move to <strong>Pending Review</strong> and cannot be edited until reviewed.
              </p>
            </div>
            <div className="bg-amber-50 border border-amber-200 rounded-lg px-4 py-3 text-sm text-amber-800">
              Please review the variance report before submitting. Pending recount items should be resolved first.
            </div>
            <form onSubmit={confirmSubmit} className="space-y-3">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Type <span className="font-mono font-bold text-gray-900">SUBMIT</span> to confirm
                </label>
                <input
                  type="text"
                  value={submitConfirmText}
                  onChange={e => setSubmitConfirmText(e.target.value)}
                  placeholder="SUBMIT"
                  autoFocus
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-teal-500"
                />
              </div>
              <div className="flex gap-2 justify-end">
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={() => { setShowSubmitDialog(false); setSubmitConfirmText('') }}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  size="sm"
                  loading={actionLoading}
                  disabled={submitConfirmText !== 'SUBMIT'}
                >
                  Submit to LS
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}