'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { stores, ls } from '@/lib/api'
import { Button, Card, CardBody, CardHeader, Spinner } from '@/components/ui'

type LSStore = { code: string; name: string }

export default function NewStorePage() {
  const router = useRouter()
  const [lsStores, setLsStores]   = useState<LSStore[]>([])
  const [lsLoading, setLsLoading] = useState(true)
  const [form, setForm] = useState({
    store_name:    '',
    store_code:    '',
    ls_store_code: '',
    location_code: '',
  })
  const [loading, setLoading] = useState(false)
  const [error, setError]     = useState('')

  useEffect(() => {
    ls.stores()
      .then((data: LSStore[]) => setLsStores(data))
      .catch(() => setLsStores([]))
      .finally(() => setLsLoading(false))
  }, [])

  function handleLSStoreSelect(code: string) {
    const picked = lsStores.find(s => s.code === code)
    if (!picked) {
      setForm(f => ({ ...f, ls_store_code: code }))
      return
    }
    setForm({
      ls_store_code: picked.code,
      store_name:    picked.name,
      store_code:    picked.code.replace(/\s+/g, '').toUpperCase(),
    })
  }

  function set(field: string, value: string) {
    setForm(f => ({ ...f, [field]: value }))
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      const store = await stores.create(form)
      router.push(`/stores/${store.id}`)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to create store')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="p-6 max-w-lg">
      <div className="mb-6">
        <h1 className="text-xl font-semibold text-gray-900">Add store</h1>
        <p className="text-sm text-gray-500 mt-0.5">Create a new store record</p>
      </div>
      <Card>
        <CardHeader><h2 className="text-sm font-semibold text-gray-700">Store details</h2></CardHeader>
        <CardBody>
          <form onSubmit={handleSubmit} className="space-y-4">

            {/* LS Store picker */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                LS store
                <span className="ml-1 font-normal text-gray-400">(select to auto-fill)</span>
              </label>
              {lsLoading ? (
                <div className="flex items-center gap-2 text-sm text-gray-400 py-2">
                  <Spinner size="sm" /> Loading stores from LS…
                </div>
              ) : lsStores.length > 0 ? (
                <select
                  value={form.ls_store_code}
                  onChange={e => handleLSStoreSelect(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
                >
                  <option value="">Select from LS…</option>
                  {lsStores.map(s => (
                    <option key={s.code} value={s.code}>
                      {s.name} ({s.code})
                    </option>
                  ))}
                </select>
              ) : (
                <p className="text-xs text-amber-600 py-1">
                  Could not load stores from LS. Enter the LS store code manually below.
                </p>
              )}
            </div>

            {/* Store name */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Store name</label>
              <input
                type="text"
                value={form.store_name}
                onChange={e => set('store_name', e.target.value)}
                placeholder="e.g. SPAR Borrowdale"
                required
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
              />
            </div>

            {/* Store code */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Store code</label>
              <input
                type="text"
                value={form.store_code}
                onChange={e => set('store_code', e.target.value)}
                placeholder="e.g. 1001"
                required
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
              />
              <p className="text-xs text-gray-400 mt-1">Internal short code used in reports</p>
            </div>

            {/* LS store code */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">LS store code</label>
              <input
                type="text"
                value={form.ls_store_code}
                onChange={e => set('ls_store_code', e.target.value)}
                placeholder="Must match the store code in LS"
                required
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
              />
              <p className="text-xs text-gray-400 mt-1">Must match the store code in LS Commerce Service</p>
            </div>

            {/* Location code */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Location code
                <span className="ml-1 font-normal text-gray-400">(optional)</span>
              </label>
              <input
                type="text"
                value={form.location_code}
                onChange={e => set('location_code', e.target.value)}
                placeholder="e.g. HQ-001"
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
              />
              <p className="text-xs text-gray-400 mt-1">LS location code if applicable</p>
            </div>

            {error && <p className="text-sm text-red-600">{error}</p>}

            <div className="flex gap-3 pt-2">
              <Button type="submit" loading={loading}>Save store</Button>
              <Button type="button" variant="secondary" onClick={() => router.back()}>Cancel</Button>
            </div>
          </form>
        </CardBody>
      </Card>
    </div>
  )
}