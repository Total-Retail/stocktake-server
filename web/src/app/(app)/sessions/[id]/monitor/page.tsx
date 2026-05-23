'use client'

import { useEffect, useState } from 'react'
import { useParams } from 'next/navigation'
import { sessions } from '@/lib/api'
import { useSessionSocket } from '@/lib/useSessionSocket'
import type { Session, Counter } from '@/types'
import { Card, CardHeader, CardBody, Badge, StatusBadge, Spinner } from '@/components/ui'

export default function MonitorPage() {
  const { id } = useParams<{ id: string }>()
  const { events, connected } = useSessionSocket(id)
  const [session, setSession] = useState<Session | null>(null)
  const [counters, setCounters] = useState<Counter[]>([])

  useEffect(() => {
    sessions.get(id).then(setSession)
    sessions.listCounters(id).then(setCounters)
  }, [id])

  // Derive per-counter stats from WS events
  const countsByCounter: Record<string, number> = {}
  for (const e of events) {
    if (e.type === 'count.submitted') {
      const cid = (e.payload as { counter_id?: string }).counter_id ?? 'unknown'
      countsByCounter[cid] = (countsByCounter[cid] ?? 0) + ((e.payload as { count?: number }).count ?? 0)
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
                    <td className="px-4 py-3 text-teal-600 font-semibold">{countsByCounter[c.id] ?? 0}</td>
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
                    <span className="mt-0.5 flex-shrink-0 w-2 h-2 rounded-full bg-teal-400 mt-1.5" />
                    <div>
                      <p className="text-xs font-medium text-gray-700">{e.type}</p>
                      <p className="text-xs text-gray-400 font-mono">{JSON.stringify(e.payload)}</p>
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
