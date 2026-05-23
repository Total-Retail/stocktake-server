'use client'

import { useEffect, useState } from 'react'
import { settings as settingsApi } from '@/lib/api'
import type { VarianceSetting } from '@/types'
import { SESSION_TYPE_LABELS } from '@/types'
import { Button, Card, CardBody, CardHeader, Spinner } from '@/components/ui'

export default function SettingsPage() {
  const [variances, setVariances] = useState<VarianceSetting[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<Record<string, number>>({})
  const [saving, setSaving] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  useEffect(() => {
    settingsApi.listVariance()
      .then(setVariances)
      .finally(() => setLoading(false))
  }, [])

  function startEdit(type: string, current: number) {
    setEditing(e => ({ ...e, [type]: current }))
  }

  async function saveVariance(type: string) {
    const pct = editing[type]
    if (pct === undefined) return
    setSaving(type)
    setError('')
    setSuccess('')
    try {
      const updated = await settingsApi.updateVariance(type, pct)
      setVariances(prev => prev.map(v => v.stock_count_type === type ? updated : v))
      setEditing(e => { const n = { ...e }; delete n[type]; return n })
      setSuccess(`Variance tolerance updated for ${SESSION_TYPE_LABELS[type] ?? type}.`)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSaving(null)
    }
  }

  if (loading) return <div className="flex justify-center items-center h-64"><Spinner size="lg" /></div>

  return (
    <div className="p-6 space-y-6 max-w-2xl">
      <div>
        <h1 className="text-xl font-semibold text-gray-900">Settings</h1>
        <p className="text-sm text-gray-500 mt-0.5">System-wide configuration</p>
      </div>

      {error   && <div className="bg-red-50 border border-red-200 text-red-700 text-sm px-4 py-3 rounded-lg">{error}</div>}
      {success && <div className="bg-teal-50 border border-teal-200 text-teal-700 text-sm px-4 py-3 rounded-lg">{success}</div>}

      <Card>
        <CardHeader>
          <div>
            <h2 className="text-sm font-semibold text-gray-700">Variance tolerances</h2>
            <p className="text-xs text-gray-400 mt-0.5">
              Items outside these thresholds appear on the variance report for review.
            </p>
          </div>
        </CardHeader>
        <CardBody className="p-0">
          <table className="w-full text-sm">
            <thead className="bg-gray-50 border-b border-gray-100">
              <tr>
                {['Stock count type', 'Tolerance %', 'Last updated', ''].map(h => (
                  <th key={h} className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wide">{h}</th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-50">
              {variances.map(v => {
                const isEditing = editing[v.stock_count_type] !== undefined
                return (
                  <tr key={v.stock_count_type} className="hover:bg-gray-50">
                    <td className="px-4 py-3 font-medium text-gray-900">
                      {SESSION_TYPE_LABELS[v.stock_count_type] ?? v.stock_count_type}
                    </td>
                    <td className="px-4 py-3">
                      {isEditing ? (
                        <input
                          type="number"
                          min={0}
                          max={100}
                          step={0.1}
                          value={editing[v.stock_count_type]}
                          onChange={e => setEditing(ed => ({ ...ed, [v.stock_count_type]: parseFloat(e.target.value) }))}
                          className="w-24 px-2 py-1 border border-teal-400 rounded text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
                          autoFocus
                        />
                      ) : (
                        <span className="font-mono text-gray-700">{v.tolerance_pct.toFixed(2)}%</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-gray-400 text-xs">
                      {v.updated_at ? new Date(v.updated_at).toLocaleDateString() : '—'}
                    </td>
                    <td className="px-4 py-3">
                      {isEditing ? (
                        <div className="flex gap-2">
                          <Button
                            size="sm"
                            loading={saving === v.stock_count_type}
                            onClick={() => saveVariance(v.stock_count_type)}
                          >
                            Save
                          </Button>
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => setEditing(e => { const n = { ...e }; delete n[v.stock_count_type]; return n })}
                          >
                            Cancel
                          </Button>
                        </div>
                      ) : (
                        <button
                          onClick={() => startEdit(v.stock_count_type, v.tolerance_pct)}
                          className="text-xs text-teal-600 hover:text-teal-700 font-medium"
                        >
                          Edit
                        </button>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </CardBody>
      </Card>
    </div>
  )
}
