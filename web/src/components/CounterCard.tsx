'use client'

// The NFC card cell of a counter row: shows the linked card, links one, moves or unlinks it.
//
// Linking from the portal works with a desktop USB NFC reader in keyboard mode: click
// "Link card", tap the card on the reader and it types the UID followed by Enter. The
// reader must be set to output hexadecimal (a 10-digit decimal number is refused).
// Supervisors can also link cards on the counting device itself (Supervisor → Link cards).

import { useState } from 'react'
import { counters as countersApi, ApiError } from '@/lib/api'
import type { Counter } from '@/types'
import { Button } from '@/components/ui'

export function shortUid(uid: string) {
  return uid.length > 8 ? `…${uid.slice(-6)}` : uid
}

export function CounterCard({ counter, onChanged }: { counter: Counter; onChanged: (c: Counter) => void }) {
  const [open, setOpen]         = useState(false)
  const [uid, setUid]           = useState('')
  const [label, setLabel]       = useState('')
  const [busy, setBusy]         = useState(false)
  const [error, setError]       = useState('')
  const [holder, setHolder]     = useState<string | null>(null) // who holds the card now (409)

  function start() {
    setOpen(true)
    setUid('')
    setLabel(counter.tag_label ?? '')
    setError('')
    setHolder(null)
  }

  async function save(force = false) {
    if (!uid.trim()) return
    setBusy(true)
    setError('')
    try {
      const linked = await countersApi.linkTag(counter.id, uid.trim(), label.trim(), force)
      onChanged({ ...counter, ...linked, id: counter.id })
      setOpen(false)
      setHolder(null)
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 409 && err.body.code === 'tag_in_use') {
        setHolder(String(err.body.counter_name ?? 'another counter'))
      } else {
        setError(err instanceof Error ? err.message : 'Could not link the card')
      }
    } finally {
      setBusy(false)
    }
  }

  async function unlink() {
    if (!window.confirm(`Unlink ${counter.name}'s card? They will not be able to tap in until a card is linked again.`)) return
    setBusy(true)
    setError('')
    try {
      await countersApi.unlinkTag(counter.id)
      onChanged({ ...counter, tag_uid: null, tag_label: null, tag_assigned_at: null, tag_last_used_at: null })
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Could not unlink the card')
    } finally {
      setBusy(false)
    }
  }

  if (open) {
    return (
      <form
        onSubmit={e => { e.preventDefault(); save(false) }}
        className="space-y-2 min-w-[16rem]"
      >
        <input
          autoFocus
          value={uid}
          onChange={e => { setUid(e.target.value); setHolder(null) }}
          placeholder="Tap the card on the reader, or type its UID"
          className="w-full px-2 py-1.5 border border-gray-300 rounded-md text-xs font-mono focus:outline-none focus:ring-2 focus:ring-teal-500"
        />
        <input
          value={label}
          onChange={e => setLabel(e.target.value)}
          placeholder="Label printed on the card (optional)"
          className="w-full px-2 py-1.5 border border-gray-300 rounded-md text-xs focus:outline-none focus:ring-2 focus:ring-teal-500"
        />
        {holder && (
          <div className="text-xs text-amber-800 bg-amber-50 border border-amber-200 rounded-md px-2 py-1.5">
            This card is linked to <strong>{holder}</strong>.{' '}
            <button type="button" onClick={() => save(true)} className="underline font-medium" disabled={busy}>
              Move it to {counter.name}
            </button>
          </div>
        )}
        {error && <p className="text-xs text-red-600">{error}</p>}
        <div className="flex gap-2">
          <Button type="submit" size="sm" loading={busy} disabled={!uid.trim()}>Save</Button>
          <Button type="button" size="sm" variant="secondary" onClick={() => setOpen(false)}>Cancel</Button>
        </div>
      </form>
    )
  }

  return (
    <div className="flex items-center gap-2 flex-wrap">
      {counter.tag_uid ? (
        <span
          className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-teal-50 text-teal-700"
          title={`UID ${counter.tag_uid}${counter.tag_last_used_at ? ` · last tapped ${new Date(counter.tag_last_used_at).toLocaleString()}` : ' · not tapped yet'}`}
        >
          {counter.tag_label ? `${counter.tag_label} · ` : ''}{shortUid(counter.tag_uid)}
        </span>
      ) : (
        <span className="text-xs text-gray-400">No card</span>
      )}
      <button onClick={start} disabled={busy} className="text-xs text-teal-600 hover:text-teal-800 disabled:opacity-50">
        {counter.tag_uid ? 'Change' : 'Link card'}
      </button>
      {counter.tag_uid && (
        <button onClick={unlink} disabled={busy} className="text-xs text-red-500 hover:text-red-700 disabled:opacity-50">
          Unlink
        </button>
      )}
      {error && <span className="text-xs text-red-600">{error}</span>}
    </div>
  )
}
