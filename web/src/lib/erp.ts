'use client'

// The ERP this server talks to (ERP_BACKEND=ls|vantage), so the portal names it
// correctly: "LS store code" on an LS Central deployment, "Vantage store code" on a
// Vantage one. Read once from the public GET /api/v1/system/info.

import { useEffect, useState } from 'react'

export interface Erp {
  backend: 'ls' | 'vantage' | ''
  /** "LS" | "Vantage" — before a noun: "LS store code", "Submit to Vantage". */
  short: string
  /** "LS Central" | "Vantage Retail" */
  label: string
  /** Where store codes come from: "LS Commerce Service" | "Vantage Retail". */
  storeSource: string
  /** The ERP document a session counts against: "worksheet" (LS) | "stock count" (Vantage). */
  worksheet: string
  /** Same, capitalised for headings. */
  Worksheet: string
}

const LS: Erp = {
  backend: 'ls', short: 'LS', label: 'LS Central', storeSource: 'LS Commerce Service',
  worksheet: 'worksheet', Worksheet: 'Worksheet',
}
const VANTAGE: Erp = {
  backend: 'vantage', short: 'Vantage', label: 'Vantage Retail', storeSource: 'Vantage Retail',
  worksheet: 'stock count', Worksheet: 'Stock count',
}
/** Until the server has answered: neutral wording, never the wrong ERP's name. */
const UNKNOWN: Erp = {
  backend: '', short: 'ERP', label: 'the ERP', storeSource: 'the ERP',
  worksheet: 'worksheet', Worksheet: 'Worksheet',
}

let cached: Erp | null = null
let pending: Promise<Erp> | null = null

function load(): Promise<Erp> {
  if (cached) return Promise.resolve(cached)
  if (!pending) {
    pending = fetch('/api/v1/system/info')
      .then(r => (r.ok ? r.json() : null))
      .then(info => {
        cached = info?.erp_backend === 'vantage' ? VANTAGE : info?.erp_backend === 'ls' ? LS : UNKNOWN
        return cached
      })
      .catch(() => UNKNOWN)
      .finally(() => { pending = null })
  }
  return pending
}

export function useErp(): Erp {
  const [erp, setErp] = useState<Erp>(cached ?? UNKNOWN)
  useEffect(() => {
    let live = true
    load().then(e => { if (live) setErp(e) })
    return () => { live = false }
  }, [])
  return erp
}
