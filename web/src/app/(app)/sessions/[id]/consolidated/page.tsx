'use client'

import { useEffect, useState } from 'react'
import { useParams } from 'next/navigation'
import { variance as varianceApi } from '@/lib/api'
import { exportToExcel } from '@/lib/exportExcel'
import type { ConsolidatedLine } from '@/types'
import { Button, Card, CardBody, Badge, Spinner, Empty } from '@/components/ui'
import { clsx } from 'clsx'

export default function ConsolidatedPage() {
  const { id } = useParams<{ id: string }>()
  const [lines, setLines] = useState<ConsolidatedLine[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [flagging, setFlagging] = useState(false)

  useEffect(() => {
    varianceApi.getConsolidated(id).then(setLines).finally(() => setLoading(false))
  }, [id])

  const filtered = filter
    ? (lines ?? []).filter(l =>
        l.item_no.toLowerCase().includes(filter.toLowerCase()) ||
        l.description.toLowerCase().includes(filter.toLowerCase()),
      )
    : (lines ?? [])

  function toggleSelect(itemNo: string) {
    setSelected(prev => {
      const next = new Set(prev)
      next.has(itemNo) ? next.delete(itemNo) : next.add(itemNo)
      return next
    })
  }

  async function flagSelected() {
    if (selected.size === 0) return
    setFlagging(true)
    try {
      await varianceApi.flagItems(id, Array.from(selected))
      setLines(prev => (prev ?? []).map(l => selected.has(l.item_no) ? { ...l, flagged: true } : l))
      setSelected(new Set())
    } finally {
      setFlagging(false)
    }
  }

  function handleExport() {
    const rows = filtered.map(l => ({
      'Item No.': l.item_no,
      'Barcode': l.barcode,
      'Description': l.description,
      'Counted Qty': l.counted_qty,
      'Theoretical Qty': l.theoretical_qty,
      'Variance': l.variance,
      'Variance %': l.variance_pct,
      'Unit Cost': l.unit_cost,
      'Variance Cost': l.variance_cost,
      'Flagged': l.flagged ? 'Yes' : 'No',
    }))
    exportToExcel(rows, 'Consolidated', `consolidated-session-${id}`)
  }

  if (loading) return <div className="flex justify-center items-center h-64"><Spinner size="lg" /></div>

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between gap-4 flex-wrap">
        <div>
          <h1 className="text-xl font-semibold text-gray-900">Consolidated View</h1>
          <p className="text-sm text-gray-500 mt-0.5">Counted totals vs theoretical stock</p>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <input
            type="search"
            placeholder="Search item…"
            value={filter}
            onChange={e => setFilter(e.target.value)}
            className="px-3 py-2 border border-gray-300 rounded-lg text-sm w-48 focus:outline-none focus:ring-2 focus:ring-teal-500"
          />
          {filtered.length > 0 && (
            <Button variant="secondary" onClick={handleExport}>Export Excel</Button>
          )}
          {selected.size > 0 && (
            <Button onClick={flagSelected} loading={flagging} variant="danger">
              Flag {selected.size} item{selected.size > 1 ? 's' : ''} for recount
            </Button>
          )}
        </div>
      </div>

      <Card>
        <CardBody className="p-0">
          {filtered.length === 0 ? (
            <Empty message={filter ? 'No matching items.' : 'No items counted yet.'} />
          ) : (
            <table className="w-full text-sm">
              <thead className="bg-gray-50 border-b border-gray-100">
                <tr>
                  <th className="px-4 py-3 w-8" />
                  {['Item no.', 'Barcode', 'Description', 'Counted', 'Theoretical', 'Variance', 'Var %', 'Cost Variance', 'Status'].map(h => (
                    <th key={h} className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wide">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50">
                {filtered.map(line => (
                  <tr key={line.item_no} className={clsx('hover:bg-gray-50', selected.has(line.item_no) && 'bg-red-50')}>
                    <td className="px-4 py-3">
                      <input
                        type="checkbox"
                        checked={selected.has(line.item_no)}
                        disabled={!!line.flagged}
                        onChange={() => toggleSelect(line.item_no)}
                        className="rounded border-gray-300 text-teal-600 focus:ring-teal-500"
                      />
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-gray-600">{line.item_no}</td>
                    <td className="px-4 py-3 font-mono text-xs text-gray-500">{line.barcode}</td>
                    <td className="px-4 py-3 text-gray-900">{line.description}</td>
                    <td className="px-4 py-3 font-semibold text-teal-700">{line.counted_qty}</td>
                    <td className="px-4 py-3 text-gray-600">{line.theoretical_qty}</td>
                    <td className={clsx('px-4 py-3 font-medium', line.variance !== 0 ? 'text-red-600' : 'text-gray-500')}>
                      {line.variance > 0 ? '+' : ''}{line.variance}
                    </td>
                    <td className={clsx('px-4 py-3 font-medium text-sm',
                      Math.abs(line.variance_pct) > 10 ? 'text-red-600' : Math.abs(line.variance_pct) > 0 ? 'text-yellow-600' : 'text-gray-400')}>
                      {line.variance_pct > 0 ? '+' : ''}{line.variance_pct?.toFixed(1)}%
                    </td>
                    <td className={clsx('px-4 py-3 font-medium text-sm',
                      line.variance_cost < 0 ? 'text-red-600' : line.variance_cost > 0 ? 'text-yellow-600' : 'text-gray-400')}>
                      {line.variance_cost > 0 ? '+' : ''}{line.variance_cost?.toFixed(2)}
                    </td>
                    <td className="px-4 py-3">
                      {line.flagged
                        ? <Badge color="yellow">Flagged</Badge>
                        : line.variance === 0
                        ? <Badge color="green">OK</Badge>
                        : null}
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