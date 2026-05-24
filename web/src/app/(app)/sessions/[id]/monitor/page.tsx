'use client'

import { useEffect, useState, useRef } from 'react'
import { useParams } from 'next/navigation'
import { sessions, reporting } from '@/lib/api'
import { useSessionSocket } from '@/lib/useSessionSocket'
import type { Session, Counter, WSEvent } from '@/types'
import { Card, CardHeader, CardBody, Badge, StatusBadge, Spinner } from '@/components/ui'

export default function MonitorPage() {
  const { id } = useParams<{ id: string }>()
  const { events, connected } = useSessionSocket(id)
  const [session, setSession] = useState<Session | null>(null)
  const [counters, setCounters] = useState<Counter[]>([])

  // Per-counter item counts seeded from the reporting API so existing counts
  // (before the admin opened this page) are visible immediately.
  const [counterItemCounts, setCounterItemCounts] = useState<Record<string, number>>({})

  // Track which WS events we've already processed so we don't double-count on re-renders.
  const processedEventCount = useRef(0)

  // Build a name lookup from the counters array so event descriptions can show
  // human-readable names instead of UUIDs.
  const counterNamesById = Object.fromEntries(counters.map(c => [c.id, c.name]))

  useEffect(() => {
    sessions.get(id).then(setSession)
    sessions.listCounters(id).then(setCounters)
    // Seed initial item-count stats from the reporting API
    reporting.getCounterPerformance(id).then(perf => {
      const initial: Record<string, number> = {}
      for (const p of (perf ?? [])) initial[p.counter_id] = p.items_counted
      setCounterItemCounts(initial)
    })
  }, [id])

  // When new WS events arrive, increment counter stats for 'count.submitted'.
  useEffect(() => {
    const newEvents = events.slice(0, events.length - processedEventCount.current)
    if (newEvents.length === 0) return
    processedEventCount.current = events.length

    const deltas: Record<string, number> = {}
    for (const e of newEvents) {
      if (e.type === 'count.submitted') {
        const cid   = (e.payload as { counter_id?: string }).counter_id ?? ''
        const count = (e.payload as { count?: number }).count ?? 0
        if (cid) deltas[cid] = (deltas[cid] ?? 0) + count
      }
    }
    if (Object.keys(deltas).length > 0) {
      setCounterItemCounts(prev => {
        const next = { ...prev }
        for (const [cid, delta] of Object.entries(deltas)) next[cid] = (next[cid] ?? 0) + delta
        return next
      })
    }
  }, [events])

  function formatEvent(e: WSEvent): string {
    const p = e.payload
    const counterName = (p.counter_id ? counterNamesById[p.counter_id as string] : null)
      ?? (p.counter_id as string ?? 'Unknown counter')
    switch (e.type) {
      case 'count.submitted':
        return `${counterName} submitted ${p.count ?? '?'} item${(p.count as number) !== 1 ? 's' : ''}`
      case 'bin.completed':
        return `${counterName} completed a bin`
      case 'counter.connected':
        return `${counterName} connected`
      case 'counter.disconnected':
        return `${counterName} disconnected`
      case 'session.status_changed':
        return `Session status → ${p.status ?? '?'}`
      default:
        return `${e.type}`
    }
  }

  if (!session) return <div className="flex justify-center items-center h-64"><Spinner size="lg" /></div>

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900">Live Monitor</h1>
          <p className="text-sm text-gray-500 mt-0.5">{session.stock_count_date} — <StatusBadge status={session.status} /></p>
        </div>
        <Badge color={connected ? 'green' : 'red'}>{connected ? '● Live' : '○ Disconnected'}</Badge>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <Card>
          <CardHeader><h2 className="text-sm font-semibold text-gray-700">Counters</h2></CardHeader>
          <CardBody className="p-0">
            <table className="w-full text-sm">
              <thead className="bg-gray-50 border-b border-gray-100">
                <tr>
                  {['Name', 'Mobile', 'Items this session'].map(h => (
                    <th key={h} className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wide">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50">
                {counters.map(c => (
                  <tr key={c.id}>
                    <td className="px-4 py-3 font-medium text-gray-900">{c.name}</td>
                    <td className="px-4 py-3 text-gray-600">{c.mobile_number}</td>
                    <td className="px-4 py-3 text-teal-600 font-semibold">{counterItemCounts[c.id] ?? 0}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardBody>
        </Card>

        <Card>
          <CardHeader><h2 className="text-sm font-semibold text-gray-700">Event feed</h2></CardHeader>
          <CardBody className="p-0 max-h-96 overflow-y-auto">
            {events.length === 0 ? (
              <p className="px-4 py-6 text-sm text-gray-400 text-center">Waiting for events…</p>
            ) : (
              <ul className="divide-y divide-gray-50">
                {events.map((e, i) => (
                  <li key={i} className="px-4 py-2.5 flex items-start gap-3">
                    <span className="flex-shrink-0 w-2 h-2 rounded-full bg-teal-400 mt-1.5" />
                    <div>
                      <p className="text-xs font-medium text-gray-700">{formatEvent(e)}</p>
                      <p className="text-xs text-gray-400">{e.type}</p>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </CardBody>
        </Card>
      </div>
    </div>
  )
}
