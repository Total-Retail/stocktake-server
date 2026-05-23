'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { sessions, stores } from '@/lib/api'
import type { Session, Store, SessionStatus } from '@/types'
import { SESSION_TYPE_LABELS, SESSION_STATUS_LABELS, SESSION_STATUS_COLOURS } from '@/types'
import { Button, Card, CardBody, StatusBadge, Spinner, Empty } from '@/components/ui'

export default function SessionsPage() {
  const [list, setList] = useState<Session[]>([])
  const [storeMap, setStoreMap] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([sessions.list(), stores.list()]).then(([sess, st]) => {
      setList(sess)
      setStoreMap(Object.fromEntries(st.map((s: Store) => [s.id, s.store_name])))
    }).finally(() => setLoading(false))
  }, [])

  if (loading) return <div className="flex justify-center items-center h-64"><Spinner size="lg" /></div>

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900">Stock Counts</h1>
          <p className="text-sm text-gray-500 mt-0.5">Create and manage stock count sessions</p>
        </div>
        <Link href="/sessions/new">
          <Button>New stock count</Button>
        </Link>
      </div>

      <Card>
        <CardBody className="p-0">
          {list.length === 0 ? (
            <Empty message="No stock counts yet." />
          ) : (
            <table className="w-full text-sm">
              <thead className="bg-gray-50 border-b border-gray-100">
                <tr>
                  {['Store', 'Date', 'Type', 'Status', ''].map(h => (
                    <th key={h} className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wide">{h}</th>
                  ))}
                </tr>
              </thead>
                <tbody className="divide-y divide-gray-50">
                  {list.map(sess => (
                    <tr key={sess.id} className="hover:bg-gray-50">
                      <td className="px-4 py-3 font-medium text-gray-900">{storeMap[sess.store_id] ?? sess.store_id}</td>
                      <td className="px-4 py-3 text-gray-600">{sess.stock_count_date}</td>
                      <td className="px-4 py-3 text-gray-600">{SESSION_TYPE_LABELS[sess.type] ?? sess.type}</td>
                      <td className="px-4 py-3">
                        <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${SESSION_STATUS_COLOURS[sess.status as SessionStatus] ?? 'bg-gray-100 text-gray-700'}`}>
                          {SESSION_STATUS_LABELS[sess.status as SessionStatus] ?? sess.status}
                        </span>
                      </td>
                      <td className="px-4 py-3">
                        <Link href={`/sessions/${sess.id}`} className="text-teal-600 hover:text-teal-700 font-medium text-xs">
                          Open →
                        </Link>
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
