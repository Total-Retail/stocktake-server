'use client'

// Shared counting devices (13 Oct 2026). A device is enrolled on the device itself: a
// supervisor signs in on the StockCount app (Supervisor) and sets it up for shared
// counting; counters then tap their NFC card to count on it. Here devices are listed,
// renamed, tied to a store, and revoked when lost or retired.

import { useEffect, useState } from 'react'
import { devices as devicesApi, stores as storesApi } from '@/lib/api'
import type { Device, Store } from '@/types'
import { Badge, Button, Card, CardBody, CardHeader, Empty, Spinner } from '@/components/ui'

function ago(ts: string | null) {
  if (!ts) return 'Never'
  const mins = Math.round((Date.now() - new Date(ts).getTime()) / 60000)
  if (mins < 1) return 'Just now'
  if (mins < 60) return `${mins} min ago`
  if (mins < 60 * 24) return `${Math.round(mins / 60)} h ago`
  return new Date(ts).toLocaleDateString()
}

export default function DevicesPage() {
  const [list, setList]       = useState<Device[]>([])
  const [stores, setStores]   = useState<Store[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError]     = useState('')
  const [editing, setEditing] = useState<string | null>(null)
  const [draft, setDraft]     = useState<{ name: string; store_id: string }>({ name: '', store_id: '' })
  const [busy, setBusy]       = useState<string | null>(null)

  async function load() {
    const [d, s] = await Promise.all([devicesApi.list(), storesApi.list().catch(() => [] as Store[])])
    setList(d ?? [])
    setStores(s ?? [])
  }

  useEffect(() => {
    load().catch(err => setError(err instanceof Error ? err.message : 'Failed to load devices')).finally(() => setLoading(false))
  }, [])

  function startEdit(d: Device) {
    setEditing(d.id)
    setDraft({ name: d.name, store_id: d.store_id ?? '' })
  }

  async function save(id: string) {
    setBusy(id)
    setError('')
    try {
      await devicesApi.update(id, { name: draft.name, store_id: draft.store_id || null })
      setEditing(null)
      await load()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Could not save the device')
    } finally {
      setBusy(null)
    }
  }

  async function revoke(d: Device) {
    if (!window.confirm(`Revoke "${d.name}"? It stops working for shared counting at once. Counts it already sent are kept; counts still waiting on it will not be accepted. A supervisor can set it up again from the device.`)) return
    setBusy(d.id)
    setError('')
    try {
      await devicesApi.revoke(d.id)
      await load()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Could not revoke the device')
    } finally {
      setBusy(null)
    }
  }

  if (loading) return <div className="flex justify-center items-center h-64"><Spinner size="lg" /></div>

  const active = list.filter(d => d.active).length

  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-gray-900">Shared devices</h1>
        <p className="text-sm text-gray-500 mt-0.5">
          Devices counters share by tapping their NFC card. {active} active.
        </p>
      </div>

      {error && <div className="bg-red-50 border border-red-200 text-red-700 text-sm px-4 py-3 rounded-lg">{error}</div>}

      <Card>
        <CardHeader>
          <h2 className="text-sm font-semibold text-gray-700">How to add a device</h2>
        </CardHeader>
        <CardBody>
          <ol className="list-decimal list-inside text-sm text-gray-600 space-y-1">
            <li>On the device, open StockCount. If it is not pointed at this server yet, use <strong>Server setup</strong> on the sign-in screen.</li>
            <li>Tap <strong>Supervisor</strong> and sign in with your portal username and password.</li>
            <li>Choose <strong>Set up for shared counting</strong>, name the device and pick its store.</li>
            <li>Link counters&apos; cards on the device (<strong>Link cards</strong>) or here, on a stock count&apos;s Overview.</li>
          </ol>
        </CardBody>
      </Card>

      <Card>
        <CardBody className="p-0">
          {list.length === 0 ? (
            <Empty message="No shared devices yet." />
          ) : (
            <table className="w-full text-sm">
              <thead className="bg-gray-50 border-b border-gray-100">
                <tr>
                  {['Device', 'Store', 'Last seen', 'Enrolled', 'App', 'Status', ''].map(h => (
                    <th key={h} className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wide">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50">
                {list.map(d => (
                  <tr key={d.id} className={d.active ? 'hover:bg-gray-50' : 'bg-gray-50/60 text-gray-400'}>
                    <td className="px-4 py-3">
                      {editing === d.id ? (
                        <input
                          value={draft.name}
                          onChange={e => setDraft(p => ({ ...p, name: e.target.value }))}
                          className="px-2 py-1 border border-gray-300 rounded-md text-sm w-44 focus:outline-none focus:ring-2 focus:ring-teal-500"
                        />
                      ) : (
                        <>
                          <p className="font-medium text-gray-900">{d.name}</p>
                          <p className="text-xs text-gray-400">{d.model}</p>
                        </>
                      )}
                    </td>
                    <td className="px-4 py-3 text-gray-600">
                      {editing === d.id ? (
                        <select
                          value={draft.store_id}
                          onChange={e => setDraft(p => ({ ...p, store_id: e.target.value }))}
                          className="px-2 py-1 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
                        >
                          <option value="">Any store</option>
                          {stores.map(s => <option key={s.id} value={s.id}>{s.store_name}</option>)}
                        </select>
                      ) : (d.store_name ?? <span className="text-gray-400">Any store</span>)}
                    </td>
                    <td className="px-4 py-3 text-gray-500 text-xs">{ago(d.last_seen_at)}</td>
                    <td className="px-4 py-3 text-gray-500 text-xs">
                      {new Date(d.enrolled_at).toLocaleDateString()}
                      {d.enrolled_by_name && <span className="block text-gray-400">by {d.enrolled_by_name}</span>}
                    </td>
                    <td className="px-4 py-3 text-gray-400 text-xs">{d.app_version || '—'}</td>
                    <td className="px-4 py-3">
                      {d.active ? <Badge color="green">Active</Badge> : <Badge color="gray">Revoked</Badge>}
                    </td>
                    <td className="px-4 py-3">
                      {d.active && (
                        editing === d.id ? (
                          <div className="flex gap-2">
                            <Button size="sm" loading={busy === d.id} onClick={() => save(d.id)}>Save</Button>
                            <Button size="sm" variant="secondary" onClick={() => setEditing(null)}>Cancel</Button>
                          </div>
                        ) : (
                          <div className="flex gap-3">
                            <button onClick={() => startEdit(d)} className="text-xs text-teal-600 hover:text-teal-800">Edit</button>
                            <button onClick={() => revoke(d)} disabled={busy === d.id} className="text-xs text-red-500 hover:text-red-700 disabled:opacity-50">Revoke</button>
                          </div>
                        )
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardBody>
      </Card>
    </div>
  )
}
