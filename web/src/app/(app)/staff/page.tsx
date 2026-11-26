'use client'

// Staff (24 Nov 2026): everyone who signs in on the Vantage Suite app. Counters are still
// added to a stock count from the count's page; here an admin adds people who only do
// label printing, and ticks who may use Label Printing (scanning items onto a Business
// Central label worksheet). A person with the tick can sign in (phone OTP, or their card
// on a shared device) even when they are not on a stock count.

import { useEffect, useState } from 'react'
import { counters as countersApi } from '@/lib/api'
import type { Counter } from '@/types'
import { Badge, Button, Card, CardBody, CardHeader, Empty, Spinner } from '@/components/ui'

export default function StaffPage() {
  const [list, setList]       = useState<Counter[]>([])
  const [q, setQ]             = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError]     = useState('')
  const [busy, setBusy]       = useState<string | null>(null)
  const [draft, setDraft]     = useState({ name: '', mobile: '', can_print_labels: true })
  const [adding, setAdding]   = useState(false)

  async function load(search = q) {
    setList((await countersApi.list(undefined, search || undefined)) ?? [])
  }

  useEffect(() => {
    load('').catch(err => setError(err instanceof Error ? err.message : 'Failed to load staff')).finally(() => setLoading(false))
  }, [])

  async function toggleLabels(c: Counter) {
    setBusy(c.id)
    setError('')
    try {
      const updated = await countersApi.update(c.id, { can_print_labels: !c.can_print_labels })
      setList(prev => prev.map(x => (x.id === c.id ? { ...x, ...updated } : x)))
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Could not save')
    } finally {
      setBusy(null)
    }
  }

  async function add(e: React.FormEvent) {
    e.preventDefault()
    setAdding(true)
    setError('')
    try {
      await countersApi.create(draft)
      setDraft({ name: '', mobile: '', can_print_labels: true })
      await load()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Could not add the person')
    } finally {
      setAdding(false)
    }
  }

  if (loading) return <div className="flex justify-center items-center h-64"><Spinner size="lg" /></div>

  const printers = list.filter(c => c.can_print_labels).length

  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-gray-900">Staff</h1>
        <p className="text-sm text-gray-500 mt-0.5">
          People who sign in on the Vantage Suite app. {printers} may use Label Printing.
        </p>
      </div>

      {error && <div className="bg-red-50 border border-red-200 text-red-700 text-sm px-4 py-3 rounded-lg">{error}</div>}

      <Card>
        <CardHeader>
          <h2 className="text-sm font-semibold text-gray-700">Add a person</h2>
        </CardHeader>
        <CardBody>
          <form onSubmit={add} className="flex flex-wrap items-end gap-3">
            <label className="text-xs text-gray-500">
              Name
              <input
                required
                value={draft.name}
                onChange={e => setDraft(p => ({ ...p, name: e.target.value }))}
                className="block mt-1 px-2 py-1.5 border border-gray-300 rounded-md text-sm w-52 focus:outline-none focus:ring-2 focus:ring-teal-500"
              />
            </label>
            <label className="text-xs text-gray-500">
              Mobile number
              <input
                required
                value={draft.mobile}
                placeholder="0771 234 567"
                onChange={e => setDraft(p => ({ ...p, mobile: e.target.value }))}
                className="block mt-1 px-2 py-1.5 border border-gray-300 rounded-md text-sm w-44 focus:outline-none focus:ring-2 focus:ring-teal-500"
              />
            </label>
            <label className="flex items-center gap-2 text-sm text-gray-700 pb-1.5">
              <input
                type="checkbox"
                checked={draft.can_print_labels}
                onChange={e => setDraft(p => ({ ...p, can_print_labels: e.target.checked }))}
                className="h-4 w-4 accent-teal-600"
              />
              Label printing
            </label>
            <Button type="submit" size="sm" loading={adding}>Add</Button>
          </form>
          <p className="text-xs text-gray-400 mt-2">
            A number that is already here updates that person. Counters for a stock count are still added on the count&apos;s page.
          </p>
        </CardBody>
      </Card>

      <Card>
        <CardHeader>
          <input
            value={q}
            onChange={e => { setQ(e.target.value); load(e.target.value).catch(() => {}) }}
            placeholder="Search name or number"
            className="px-2 py-1.5 border border-gray-300 rounded-md text-sm w-64 focus:outline-none focus:ring-2 focus:ring-teal-500"
          />
        </CardHeader>
        <CardBody className="p-0">
          {list.length === 0 ? (
            <Empty message="Nobody yet." />
          ) : (
            <table className="w-full text-sm">
              <thead className="bg-gray-50 border-b border-gray-100">
                <tr>
                  {['Name', 'Mobile', 'Card', 'Label printing'].map(h => (
                    <th key={h} className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wide">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50">
                {list.map(c => (
                  <tr key={c.id} className="hover:bg-gray-50">
                    <td className="px-4 py-3 font-medium text-gray-900">{c.name}</td>
                    <td className="px-4 py-3 text-gray-600">{c.mobile_number}</td>
                    <td className="px-4 py-3 text-gray-500 text-xs">
                      {c.tag_uid ? (c.tag_label || c.tag_uid) : <span className="text-gray-300">—</span>}
                    </td>
                    <td className="px-4 py-3">
                      <label className="inline-flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={!!c.can_print_labels}
                          disabled={busy === c.id}
                          onChange={() => toggleLabels(c)}
                          className="h-4 w-4 accent-teal-600"
                        />
                        {c.can_print_labels ? <Badge color="green">Allowed</Badge> : <span className="text-xs text-gray-400">No</span>}
                      </label>
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
