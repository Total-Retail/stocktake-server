'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { sessions, stores, ls } from '@/lib/api'
import type { Store } from '@/types'
import { Button, Card, CardBody, CardHeader, Spinner } from '@/components/ui'

const SESSION_TYPES = [
  { value: 'FLOOR',      label: 'Floor'                  },
  { value: 'BAKERY',     label: 'Bakery'                 },
  { value: 'BUTCHERY',   label: 'Butchery'               },
  { value: 'FRUIT_VEG',  label: 'Fruit & Veg'            },
  { value: 'DELI_COLD',  label: 'Deli Cold'              },
  { value: 'DELI_HOT',   label: 'Deli Hot'               },
  { value: 'QSR',        label: 'QSR'                    },
  { value: 'RESTAURANT', label: 'Restaurant'             },
  { value: 'PARTIAL',    label: 'Partial (select items)' },
] as const

type SessionTypeValue = typeof SESSION_TYPES[number]['value']

type Worksheet = {
  worksheet_seq_no: number
  description: string
  store_no: string
  no_of_lines: number
}

export default function NewSessionPage() {
  const router = useRouter()
  const [storeList, setStoreList]         = useState<Store[]>([])
  const [allWorksheets, setAllWorksheets] = useState<Worksheet[]>([])
  const [wsLoading, setWsLoading]         = useState(true)
  const [wsError, setWsError]             = useState('')
  const [loading, setLoading]             = useState(false)
  const [form, setForm] = useState({
    store_id:         '',
    stock_count_date: new Date().toISOString().slice(0, 10),
    type:             'FLOOR' as SessionTypeValue,
    worksheet_seq_no: 0,
  })
  const [error, setError] = useState('')

  const selectedStore = storeList.find(s => s.id === form.store_id)
  const filteredWorksheets = allWorksheets.filter(w =>
    !selectedStore?.ls_store_code || w.store_no === selectedStore.ls_store_code
  )

  useEffect(() => {
    stores.list().then(setStoreList)
    ls.worksheets()
      .then((data: Worksheet[]) => {
        setAllWorksheets(data)
        setWsError('')
      })
      .catch((err: unknown) => {
        setWsError(err instanceof Error ? err.message : 'Could not load worksheets from LS')
      })
      .finally(() => setWsLoading(false))
  }, [])

  // Reset worksheet when store changes
  useEffect(() => {
    setForm(f => ({ ...f, worksheet_seq_no: 0 }))
  }, [form.store_id])

  function set(field: string, value: string | number) {
    setForm(f => ({ ...f, [field]: value }))
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!form.store_id) { setError('Please select a store'); return }
    setLoading(true)
    setError('')
    try {
      const payload = {
        store_id:         form.store_id,
        stock_count_date: form.stock_count_date,
        type:             form.type,
        worksheet_no: form.worksheet_seq_no > 0
          ? String(form.worksheet_seq_no)
          : undefined,
      }
      const sess = await sessions.create(payload)
      router.push(`/sessions/${sess.id}/monitor`)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to create session')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="max-w-xl mx-auto p-6 space-y-4">
      <h1 className="text-xl font-semibold text-gray-900">New Stock Count Session</h1>

      <Card>
        <CardHeader><h2 className="text-sm font-semibold text-gray-700">Session details</h2></CardHeader>
        <CardBody>
          <form onSubmit={handleSubmit} className="space-y-4">

            {/* Store */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Store</label>
              <select
                value={form.store_id}
                onChange={e => set('store_id', e.target.value)}
                required
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
              >
                <option value="">Select a store…</option>
                {storeList.map(s => (
                  <option key={s.id} value={s.id}>{s.store_name}</option>
                ))}
              </select>
            </div>

            {/* Date */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Stock count date</label>
              <input
                type="date"
                value={form.stock_count_date}
                onChange={e => set('stock_count_date', e.target.value)}
                required
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
              />
            </div>

            {/* Type */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Stock count type</label>
              <select
                value={form.type}
                onChange={e => set('type', e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
              >
                {SESSION_TYPES.map(t => (
                  <option key={t.value} value={t.value}>{t.label}</option>
                ))}
              </select>
            </div>

            {/* Worksheet */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                LS worksheet
                <span className="ml-1 font-normal text-gray-400">(optional — can be set later)</span>
              </label>

              {wsLoading ? (
                <div className="flex items-center gap-2 text-sm text-gray-400 py-2">
                  <Spinner size="sm" /> Checking LS for available worksheets…
                </div>
              ) : wsError ? (
                <p className="text-xs text-amber-600 py-1">
                  {wsError} — you can link a worksheet from the session page once LS is reachable.
                </p>
              ) : !form.store_id ? (
                <p className="text-xs text-gray-400 py-1">Select a store to see available worksheets.</p>
              ) : filteredWorksheets.length > 0 ? (
                <select
                  value={form.worksheet_seq_no}
                  onChange={e => set('worksheet_seq_no', parseInt(e.target.value, 10))}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-teal-500"
                >
                  <option value={0}>None — link later</option>
                  {filteredWorksheets.map(w => (
                    <option key={w.worksheet_seq_no} value={w.worksheet_seq_no}>
                      {w.description} ({w.no_of_lines} lines)
                    </option>
                  ))}
                </select>
              ) : (
                <p className="text-xs text-gray-400 py-1">
                  No worksheets found for this store in LS. You can link one later.
                </p>
              )}
            </div>

            {error && <p className="text-sm text-red-600">{error}</p>}

            <div className="flex gap-3 pt-2">
              <Button type="submit" loading={loading}>Create session</Button>
              <Button type="button" variant="secondary" onClick={() => router.back()}>Cancel</Button>
            </div>

          </form>
        </CardBody>
      </Card>
    </div>
  )
}