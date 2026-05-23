'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { stores } from '@/lib/api'
import type { Store } from '@/types'
import { Button, Card, CardBody, Spinner, Empty } from '@/components/ui'

export default function StoresPage() {
  const [list, setList] = useState<Store[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    stores.list().then(setList).finally(() => setLoading(false))
  }, [])

  if (loading) return <div className="flex justify-center items-center h-64"><Spinner size="lg" /></div>

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900">Stores</h1>
          <p className="text-sm text-gray-500 mt-0.5">Manage stores and their layout configuration</p>
        </div>
        <Link href="/stores/new">
          <Button>Add store</Button>
        </Link>
      </div>

      <Card>
        <CardBody className="p-0">
          {list.length === 0 ? (
            <Empty message="No stores yet." />
          ) : (
            <table className="w-full text-sm">
              <thead className="bg-gray-50 border-b border-gray-100">
                <tr>
                  {['Name', 'Store code', 'LS store code', 'Layout', ''].map(h => (
                    <th key={h} className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wide">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50">
                {list.map(s => (
                  <tr key={s.id} className="hover:bg-gray-50">
                    <td className="px-4 py-3 font-medium text-gray-900">{s.store_name}</td>
                    <td className="px-4 py-3 text-gray-600 font-mono text-xs">{s.store_code}</td>
                    <td className="px-4 py-3 text-gray-500 font-mono text-xs">{s.ls_store_code || '—'}</td>
                    <td className="px-4 py-3">
                      <Link href={`/stores/${s.id}`}
                        className="text-xs text-teal-600 hover:text-teal-700 font-medium">
                        View layout →
                      </Link>
                    </td>
                    <td className="px-4 py-3">
                      <Link href={`/stores/${s.id}/edit`}
                        className="text-xs text-gray-500 hover:text-gray-700 font-medium">
                        Edit
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