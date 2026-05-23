'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { clsx } from 'clsx'
import { LayoutDashboard, Store, ClipboardList, Users, Settings, LogOut } from 'lucide-react'
import { useAuth } from '@/lib/auth'

const NAV = [
  { href: '/dashboard',    label: 'Dashboard',    icon: LayoutDashboard },
  { href: '/stores',       label: 'Stores',       icon: Store },
  { href: '/sessions',     label: 'Stock Counts', icon: ClipboardList },
  { href: '/admin/users',  label: 'Admin users',  icon: Users },
  { href: '/settings',     label: 'Settings',     icon: Settings },
]

export function Sidebar() {
  const pathname = usePathname()
  const { logout } = useAuth()

  return (
    <aside className="w-56 min-h-screen bg-gray-900 flex flex-col">
      <div className="px-5 py-6 border-b border-gray-800">
        <span className="text-teal-400 font-bold text-sm tracking-wide uppercase">Total Retail</span>
        <p className="text-white font-semibold text-lg leading-tight mt-0.5">StockCount</p>
      </div>

      <nav className="flex-1 px-3 py-4 space-y-0.5">
        {NAV.map(({ href, label, icon: Icon }) => (
          <Link
            key={href}
            href={href}
            className={clsx(
              'flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm transition-colors',
              pathname.startsWith(href)
                ? 'bg-teal-600 text-white'
                : 'text-gray-400 hover:bg-gray-800 hover:text-white',
            )}
          >
            <Icon size={16} />
            {label}
          </Link>
        ))}
      </nav>

      <button
        onClick={logout}
        className="mx-3 mb-4 flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm text-gray-400 hover:bg-gray-800 hover:text-white transition-colors"
      >
        <LogOut size={16} />
        Sign out
      </button>
    </aside>
  )
}
