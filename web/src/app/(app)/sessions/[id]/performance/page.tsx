'use client'

import { useEffect, useState } from 'react'
import { useParams } from 'next/navigation'
import { reporting } from '@/lib/api'
import { exportToExcel } from '@/lib/exportExcel'
import type { CounterPerformance } from '@/types'
import { Button, Card, CardBody, CardHeader, StatCard, Spinner, Empty } from '@/components/ui'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'

export default function PerformancePage() {
  const { id } = useParams<{ id: string }>()
  const [counters, setCounters] = useState<CounterPerformance[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    reporting.getCounterPerformance(id).then(setCounters).finally(() => setLoading(false))
  }, [id])

  if (loading) return <div className="flex justify-center items-center h-64"><Spinner size="lg" /></div>

  const totalItems = counters.reduce((s, c) => s + c.items_counted, 0)
  const totalBays  = counters.reduce((s, c) => s + c.bins_completed, 0)
  const avgRecount = counters.length
    ? (counters.reduce((s, c) => s + c.recount_rate_pct, 0) / counters.length).toFixed(1)
    : '0'

  const chartData = (counters ?? []).map(c => ({
    name: c.counter_name,
    items: c.items_counted,
    bays: c.bins_completed,
  }))
  const COLORS = ['#1D9E75', '#0F6E56', '#9FE1CB', '#5DCAA5', '#34C08B', '#2BA876']

  function handleExport() {
    const rows = counters.map(c => ({
      'Counter': c.counter_name,
      'Mobile': c.mobile,
      'Items Counted': c.items_counted,
      'Bins Completed': c.bins_completed,
      'Recount Rate %': c.recount_rate_pct,
      'Recounts Accepted': c.recount_accepted,
      'Recounts Rejected': c.recount_rejected,
      'Last Active': c.last_activity ? new Date(c.last_activity).toLocaleString() : '',
    }))
    exportToExcel(rows, 'Counter Performance', `performance-session-${id}`)
  }

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900">Counter Performance</h1>
          <p className="text-sm text-gray-500 mt-0.5">Activity and accuracy breakdown per counter</p>
        </div>
        {(counters?? []).length > 0 && (
          <Button variant="secondary" onClick={handleExport}>Export Excel</Button>
        )}
      </div>

      <div className="grid grid-cols-3 gap-4">
        <StatCard label="Total items counted" value={totalItems} />
        <StatCard label="Total bins completed" value={totalBays} />
        <StatCard label="Avg recount rate" value={`${avgRecount}%`} />
      </div>

      {(counters ?? []).length > 0 && (
        <Card>
          <CardHeader><h2 className="text-sm font-semibold text-gray-700">Items counted per counter</h2></CardHeader>
          <CardBody>
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={chartData} margin={{ top: 4, right: 16, bottom: 4, left: 0 }}>
                <XAxis dataKey="name" tick={{ fontSize: 12 }} />
                <YAxis tick={{ fontSize: 12 }} />
                <Tooltip />
                <Bar dataKey="items" radius={[4, 4, 0, 0]}>
                  {chartData.map((_, i) => <Cell key={i} fill={COLORS[i % COLORS.length]} />)}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </CardBody>
        </Card>
      )}

      <Card>
        <CardBody className="p-0">
          {(counters ?? []).length === 0 ? (
            <Empty message="No counter data yet." />
          ) : (
            <table className="w-full text-sm">
              <thead className="bg-gray-50 border-b border-gray-100">
                <tr>
                  {['Counter', 'Mobile', 'Items', 'Bins', 'Recount rate', 'Accepted', 'Rejected', 'Last active'].map(h => (
                    <th key={h} className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wide">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50">
                {counters.map(c => (
                  <tr key={c.counter_id} className="hover:bg-gray-50">
                    <td className="px-4 py-3 font-medium text-gray-900">{c.counter_name}</td>
                    <td className="px-4 py-3 text-gray-500 text-xs">{c.mobile}</td>
                    <td className="px-4 py-3 font-semibold text-teal-600">{c.items_counted}</td>
                    <td className="px-4 py-3 text-gray-700">{c.bins_completed}</td>
                    <td className="px-4 py-3 text-gray-700">{c.recount_rate_pct}%</td>
                    <td className="px-4 py-3 text-green-600 font-medium">{c.recount_accepted}</td>
                    <td className="px-4 py-3 text-red-500 font-medium">{c.recount_rejected}</td>
                    <td className="px-4 py-3 text-gray-400 text-xs">
                      {c.last_activity ? new Date(c.last_activity).toLocaleString() : '—'}
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