'use client'

import { useEffect, useState } from 'react'
import { useParams } from 'next/navigation'
import Link from 'next/link'
import { stores } from '@/lib/api'
import type { Store, StoreLayout, Area, Aisle, Bin } from '@/types'
import { Button, Card, CardBody, CardHeader, Spinner, Empty } from '@/components/ui'

export default function StoreLayoutPage() {
  const { id } = useParams<{ id: string }>()
  const [store, setStore] = useState<Store | null>(null)
  const [layout, setLayout] = useState<StoreLayout>({ areas: [], aisles: [], bins: [] })
  const [loading, setLoading] = useState(true)
  const [newArea, setNewArea] = useState({ area_code: '', area_name: '' })
  const [newAisle, setNewAisle] = useState({ area_id: '', aisle_code: '', aisle_name: '' })
  const [newBin, setNewBin] = useState({ aisle_id: '', bin_code: '', bin_name: '' })
  const [saving, setSaving] = useState<'area' | 'aisle' | 'bin' | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    Promise.all([stores.get(id), stores.getLayout(id)])
      .then(([s, l]) => { setStore(s); setLayout(l) })
      .finally(() => setLoading(false))
  }, [id])

  async function addArea(e: React.FormEvent) {
    e.preventDefault()
    setSaving('area')
    setError('')
    try {
      const area = await stores.createArea(id, newArea)
      setLayout(l => ({ ...l, areas: [...(l.areas ?? []), area] }))
      setNewArea({ area_code: '', area_name: '' })
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to add area')
    } finally { setSaving(null) }
  }

  async function addAisle(e: React.FormEvent) {
    e.preventDefault()
    setSaving('aisle')
    setError('')
    try {
      const aisle = await stores.createAisle(id, newAisle)
      setLayout(l => ({ ...l, aisles: [...(l.aisles ?? []), aisle] }))
      setNewAisle({ area_id: '', aisle_code: '', aisle_name: '' })
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to add aisle')
    } finally { setSaving(null) }
  }

  async function addBin(e: React.FormEvent) {
    e.preventDefault()
    setSaving('bin')
    setError('')
    try {
      const bin = await stores.createBin(id, newBin)
      setLayout(l => ({ ...l, bins: [...(l.bins ?? []), bin] }))
      setNewBin({ aisle_id: '', bin_code: '', bin_name: '' })
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to add bin')
    } finally { setSaving(null) }
  }

  if (loading) return <div className="flex justify-center items-center h-64"><Spinner size="lg" /></div>

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-xs text-gray-400 mb-1">
            <Link href="/stores" className="hover:text-teal-600">Stores</Link>
            {' / '}
            <span className="text-gray-600">{store?.store_name}</span>
          </p>
          <h1 className="text-xl font-semibold text-gray-900">{store?.store_name}</h1>
          <p className="text-sm text-gray-500 mt-0.5">
            {store?.store_code}
            {store?.ls_store_code && <span className="ml-2 text-gray-400">· LS: {store.ls_store_code}</span>}
            {store?.location_code && <span className="ml-2 text-gray-400">· Location: {store.location_code}</span>}
            {store && !store.active && <span className="ml-2 text-red-500 font-medium">· Inactive</span>}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <a href={stores.labelsUrl(id)} target="_blank" rel="noreferrer">
            <Button variant="secondary" size="sm">Download all labels</Button>
          </a>
          <Link href={`/stores/${id}/edit`}>
            <Button variant="secondary" size="sm">Edit store</Button>
          </Link>
        </div>
      </div>

      {error && <p className="text-sm text-red-600 bg-red-50 px-4 py-2 rounded-lg">{error}</p>}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Areas */}
        <Card>
          <CardHeader>
            <h2 className="text-sm font-semibold text-gray-700">Areas ({layout.areas?.length ?? 0})</h2>
          </CardHeader>
          <CardBody className="space-y-4">
            {layout.areas?.length === 0
              ? <Empty message="No areas yet." />
              : (
                <ul className="divide-y divide-gray-100">
                  {layout.areas.map((a: Area) => (
                    <li key={a.id} className="py-2 flex justify-between text-sm">
                      <span className="font-medium text-gray-900">{a.area_name}</span>
                      <span className="text-gray-400 font-mono text-xs">{a.area_code}</span>
                    </li>
                  ))}
                </ul>
              )
            }
            <form onSubmit={addArea} className="flex gap-2 pt-2 border-t border-gray-100">
              <input value={newArea.area_code} onChange={e => setNewArea(a => ({ ...a, area_code: e.target.value }))}
                placeholder="Code" className="w-24 px-2 py-1.5 border border-gray-300 rounded text-sm focus:outline-none focus:ring-2 focus:ring-teal-500" required />
              <input value={newArea.area_name} onChange={e => setNewArea(a => ({ ...a, area_name: e.target.value }))}
                placeholder="Name" className="flex-1 px-2 py-1.5 border border-gray-300 rounded text-sm focus:outline-none focus:ring-2 focus:ring-teal-500" required />
              <Button type="submit" size="sm" loading={saving === 'area'}>Add</Button>
            </form>
          </CardBody>
        </Card>

        {/* Aisles */}
        <Card>
          <CardHeader>
            <h2 className="text-sm font-semibold text-gray-700">Aisles ({layout.aisles?.length ?? 0})</h2>
          </CardHeader>
          <CardBody className="space-y-4">
            {layout.aisles?.length === 0
              ? <Empty message="No aisles yet." />
              : (
                <ul className="divide-y divide-gray-100">
                  {layout.aisles.map((a: Aisle) => (
                    <li key={a.id} className="py-2 flex justify-between text-sm">
                      <span className="font-medium text-gray-900">{a.aisle_name}</span>
                      <span className="text-gray-400 font-mono text-xs">{a.aisle_code}</span>
                    </li>
                  ))}
                </ul>
              )
            }
            <form onSubmit={addAisle} className="flex gap-2 pt-2 border-t border-gray-100">
              <select value={newAisle.area_id} onChange={e => setNewAisle(a => ({ ...a, area_id: e.target.value }))}
                className="w-28 px-2 py-1.5 border border-gray-300 rounded text-sm focus:outline-none focus:ring-2 focus:ring-teal-500" required>
                <option value="">Area…</option>
                {layout.areas.map((a: Area) => <option key={a.id} value={a.id}>{a.area_name}</option>)}
              </select>
              <input value={newAisle.aisle_code} onChange={e => setNewAisle(a => ({ ...a, aisle_code: e.target.value }))}
                placeholder="Code" className="w-20 px-2 py-1.5 border border-gray-300 rounded text-sm focus:outline-none focus:ring-2 focus:ring-teal-500" required />
              <input value={newAisle.aisle_name} onChange={e => setNewAisle(a => ({ ...a, aisle_name: e.target.value }))}
                placeholder="Name" className="flex-1 px-2 py-1.5 border border-gray-300 rounded text-sm focus:outline-none focus:ring-2 focus:ring-teal-500" required />
              <Button type="submit" size="sm" loading={saving === 'aisle'}>Add</Button>
            </form>
          </CardBody>
        </Card>
      </div>

      {/* Bins */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold text-gray-700">Bins ({layout.bins?.length ?? 0})</h2>
            <a
              href={`/api/v1/stores/${id}/layout/import`}
              className="text-xs text-teal-600 hover:text-teal-700 font-medium"
              onClick={e => { e.preventDefault(); document.getElementById('csv-import')?.click() }}
            >
              Import from CSV
            </a>
            <input id="csv-import" type="file" accept=".csv" className="hidden" onChange={async e => {
              const file = e.target.files?.[0]
              if (!file) return
              const fd = new FormData()
              fd.append('file', file)
              const token = localStorage.getItem('st_token')
              try {
                const res = await fetch(`/api/v1/stores/${id}/layout/import`, {
                  method: 'POST',
                  headers: token ? { Authorization: `Bearer ${token}` } : {},
                  body: fd,
                })
                if (!res.ok) throw new Error((await res.json()).error)
                const data = await res.json()
                alert(`Imported ${data.imported} rows. Reload to see updated layout.`)
                const [, l] = await Promise.all([stores.get(id), stores.getLayout(id)])
                setLayout(l)
              } catch (err: unknown) {
                setError(err instanceof Error ? err.message : 'Import failed')
              }
              e.target.value = ''
            }} />
          </div>
        </CardHeader>
        <CardBody className="space-y-4">
          <form onSubmit={addBin} className="flex gap-2 pb-4 border-b border-gray-100">
            <select value={newBin.aisle_id} onChange={e => setNewBin(b => ({ ...b, aisle_id: e.target.value }))}
              className="w-32 px-2 py-1.5 border border-gray-300 rounded text-sm focus:outline-none focus:ring-2 focus:ring-teal-500" required>
              <option value="">Aisle…</option>
              {layout.aisles.map((a: Aisle) => <option key={a.id} value={a.id}>{a.aisle_name}</option>)}
            </select>
            <input value={newBin.bin_code} onChange={e => setNewBin(b => ({ ...b, bin_code: e.target.value }))}
              placeholder="Bin code" className="w-28 px-2 py-1.5 border border-gray-300 rounded text-sm focus:outline-none focus:ring-2 focus:ring-teal-500" required />
            <input value={newBin.bin_name} onChange={e => setNewBin(b => ({ ...b, bin_name: e.target.value }))}
              placeholder="Bin name" className="flex-1 px-2 py-1.5 border border-gray-300 rounded text-sm focus:outline-none focus:ring-2 focus:ring-teal-500" required />
            <Button type="submit" size="sm" loading={saving === 'bin'}>Add bin</Button>
          </form>

          {layout.bins?.length === 0 ? (
            <Empty message="No bins yet. Add aisles first, then add bins or bulk import from CSV." />
          ) : (
            <table className="w-full text-sm">
              <thead className="bg-gray-50 border-b border-gray-100">
                <tr>
                  {['Bin code', 'Bin name', 'Barcode', ''].map(h => (
                    <th key={h} className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wide">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50">
                {layout.bins.map((bin: Bin) => (
                  <tr key={bin.id} className="hover:bg-gray-50">
                    <td className="px-4 py-2.5 font-mono text-xs text-gray-600">{bin.bin_code}</td>
                    <td className="px-4 py-2.5 text-gray-900">{bin.bin_name}</td>
                    <td className="px-4 py-2.5 font-mono text-xs text-gray-400">{bin.barcode}</td>
                    <td className="px-4 py-2.5">
                      <a href={stores.binLabelUrl(id, bin.id)} target="_blank" rel="noreferrer"
                        className="text-teal-600 hover:text-teal-700 text-xs font-medium">Label</a>
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
