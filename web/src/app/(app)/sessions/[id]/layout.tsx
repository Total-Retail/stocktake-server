'use client'

import { useEffect, useState } from 'react'
import { useParams, usePathname } from 'next/navigation'
import Link from 'next/link'
import { sessions } from '@/lib/api'
import type { Session, SessionStatus } from '@/types'
import { SESSION_TYPE_LABELS, SESSION_STATUS_COLOURS } from '@/types'
import { StatusBadge } from '@/components/ui'
import { clsx } from 'clsx'

const TABS = [
  { label: 'Overview', path: '' },
  { label: 'Monitor', path: '/monitor' },
  { label: 'Consolidated', path: '/consolidated' },
  { label: 'Audit', path: '/audit' },
  { label: 'Variance', path: '/variance' },
  { label: 'Performance', path: '/performance' },
]

export default function SessionLayout({ children }: { children: React.ReactNode }) {
  const { id } = useParams<{ id: string }>()
  const pathname = usePathname()
  const [session, setSession] = useState<Session | null>(null)

  useEffect(() => {
    sessions.get(id).then(setSession).catch(() => {})
  }, [id])

  return (
    <div className="flex flex-col min-h-screen">
      <div className="bg-white border-b border-gray-200 px-6 pt-5 pb-0">
        <div className="flex items-center justify-between mb-3">
          <div>
            <p className="text-xs text-gray-400 mb-0.5">
              <Link href="/sessions" className="hover:text-teal-600">Stock Counts</Link>
              {' / '}
              <span className="text-gray-600">{session?.stock_count_date ?? '...'}</span>
            </p>
            <div className="flex items-center gap-3">
              <h1 className="text-lg font-semibold text-gray-900">
                {session?.stock_count_date ?? 'Loading...'}
              </h1>
              {session && (
                <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${SESSION_STATUS_COLOURS[session.status as SessionStatus] ?? 'bg-gray-100 text-gray-700'}`}>
                  {session.status}
                </span>
              )}
              {session && (
                <span className="text-xs text-gray-400 bg-gray-100 px-2 py-0.5 rounded">
                  {SESSION_TYPE_LABELS[session.type] ?? session.type}
                </span>
              )}
            </div>
          </div>
        </div>

        <nav className="flex gap-0 -mb-px">
          {TABS.map(tab => {
            const href = `/sessions/${id}${tab.path}`
            const active = tab.path === ''
              ? pathname === `/sessions/${id}`
              : pathname.startsWith(`/sessions/${id}${tab.path}`)
            return (
              <Link
                key={tab.path}
                href={href}
                className={clsx(
                  'px-4 py-2.5 text-sm border-b-2 transition-colors whitespace-nowrap',
                  active
                    ? 'border-teal-500 text-teal-600 font-medium'
                    : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300',
                )}
              >
                {tab.label}
              </Link>
            )
          })}
        </nav>
      </div>

      <div className="flex-1 bg-gray-50">
        {children}
      </div>
    </div>
  )
}