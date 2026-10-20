import { useDeferredValue, useEffect, useRef, useState, type FormEvent } from 'react'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { formatDateTime, formatShortTime, formatTHB, type PlatformTenant } from '@mscs/shared'
import { useSearchParams } from 'react-router'
import { api, errorMessage } from '../api'

const STATUS_FILTERS = [
  { value: '', label: 'ทั้งหมด' },
  { value: 'active', label: 'ใช้งาน' },
  { value: 'suspended', label: 'ถูกระงับ' },
]

export function TenantsPage() {
  // Search and filter live in the URL (?q=&status=): reload, back and shared links keep them.
  const [params, setParams] = useSearchParams()
  const status = params.get('status') ?? ''
  const [search, setSearch] = useState(params.get('q') ?? '')
  // React renders the typed text right away and the list a moment later (no request per key).
  const q = useDeferredValue(search.trim())

  useEffect(() => {
    setParams((p) => {
      if (q) p.set('q', q)
      else p.delete('q')
      return p
    }, { replace: true })
  }, [q, setParams])

  const tenants = useQuery({
    queryKey: ['tenants', q, status],
    queryFn: () => api<{ items: PlatformTenant[] }>(`/tenants?${new URLSearchParams({ q, status })}`),
    placeholderData: keepPreviousData, // keep the old rows on screen while the new ones load
  })

  const [target, setTarget] = useState<{ tenant: PlatformTenant, action: 'suspend' | 'reactivate' } | null>(null)

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">ร้านค้าทั้งหมด</h1>

      <div className="flex flex-wrap items-center gap-3">
        <input
          type="search"
          value={search}
          onChange={e => setSearch(e.target.value)}
          placeholder="ค้นหาชื่อร้านหรือลิงก์ร้าน"
          aria-label="ค้นหาร้าน"
          className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm outline-accent sm:w-72"
        />
        <div role="group" aria-label="สถานะร้าน" className="flex rounded-md border border-line bg-surface p-0.5 text-sm">
          {STATUS_FILTERS.map(f => (
            <button
              key={f.value}
              type="button"
              aria-pressed={status === f.value}
              onClick={() => setParams((p) => {
                if (f.value) p.set('status', f.value)
                else p.delete('status')
                return p
              }, { replace: true })}
              className={`rounded px-3 py-1 ${status === f.value ? 'bg-ok-soft font-medium' : 'text-ink-muted hover:text-ink'}`}
            >
              {f.label}
            </button>
          ))}
        </div>
        {tenants.data && <span className="text-sm text-ink-muted">{tenants.data.items.length} ร้าน</span>}
      </div>

      {tenants.isError && <p className="text-sm text-danger">{errorMessage(tenants.error)}</p>}

      {/* Wide table: it scrolls inside its box on phones, the page itself never does. "relative"
          matters: without it, the absolutely positioned sr-only header escapes the scroll box and
          widens the whole page. */}
      <div className={`relative overflow-x-auto rounded-xl border border-line bg-surface ${tenants.isPlaceholderData ? 'opacity-60' : ''}`}>
        <table className="w-full min-w-[48rem] text-sm">
          <thead className="text-left text-ink-muted">
            <tr className="[&>th]:px-4 [&>th]:py-2 [&>th]:font-normal">
              <th>ร้าน</th>
              <th>สถานะ</th>
              <th className="text-right">สินค้า</th>
              <th className="text-right">ออเดอร์</th>
              <th className="text-right">ยอดขาย</th>
              <th>ออเดอร์ล่าสุด</th>
              <th>สมัครเมื่อ</th>
              <th><span className="sr-only">การจัดการ</span></th>
            </tr>
          </thead>
          <tbody>
            {tenants.isPending && (
              <tr><td colSpan={8} className="px-4 py-6 text-center text-ink-muted">กำลังโหลด…</td></tr>
            )}
            {tenants.data?.items.length === 0 && (
              <tr><td colSpan={8} className="px-4 py-6 text-center text-ink-muted">ไม่พบร้านที่ตรงกับเงื่อนไข</td></tr>
            )}
            {tenants.data?.items.map(t => (
              <tr key={t.id} className="border-t border-line align-top [&>td]:px-4 [&>td]:py-3">
                <td>
                  <p className="font-medium">{t.name}</p>
                  <p className="text-xs text-ink-muted">mscs.shop/{t.slug}</p>
                  <p className="text-xs text-ink-muted">{t.owner_email || '—'} · สมาชิก {t.member_count} คน</p>
                </td>
                <td>
                  {t.status === 'active'
                    ? <span className="whitespace-nowrap rounded bg-ok-soft px-1.5 py-0.5 text-xs">ใช้งาน</span>
                    : (
                        <>
                          <span className="whitespace-nowrap rounded bg-danger-soft px-1.5 py-0.5 text-xs text-danger">ถูกระงับ</span>
                          <p className="mt-1 max-w-48 text-xs text-ink-muted">{t.suspended_reason}</p>
                        </>
                      )}
                </td>
                <td className="text-right tabular-nums">{t.product_count}</td>
                <td className="text-right tabular-nums">{t.order_count}</td>
                <td className="text-right tabular-nums">{formatTHB(t.revenue_satang)}</td>
                <td className="text-ink-muted">{t.last_order_at ? formatShortTime(t.last_order_at) : '—'}</td>
                <td className="text-ink-muted">{formatDateTime(t.created_at)}</td>
                <td className="text-right">
                  {t.status === 'active'
                    ? (
                        <button type="button" onClick={() => setTarget({ tenant: t, action: 'suspend' })} className="rounded-md border border-line px-3 py-1 text-danger hover:bg-danger-soft">
                          ระงับ
                        </button>
                      )
                    : (
                        <button type="button" onClick={() => setTarget({ tenant: t, action: 'reactivate' })} className="rounded-md border border-line px-3 py-1 hover:bg-ok-soft">
                          เปิดใช้งาน
                        </button>
                      )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {target && <StatusDialog {...target} onClose={() => setTarget(null)} />}
    </div>
  )
}

/** Confirms a suspension (with its reason, shown to the shop) or a reactivation. */
function StatusDialog({ tenant, action, onClose }: { tenant: PlatformTenant, action: 'suspend' | 'reactivate', onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)
  const qc = useQueryClient()
  const [reason, setReason] = useState('')
  const suspend = action === 'suspend'

  // A real modal (<dialog>.showModal): focus is trapped inside and Esc closes it.
  useEffect(() => {
    ref.current?.showModal()
  }, [])

  const change = useMutation({
    mutationFn: () => api(`/tenants/${tenant.id}/${action}`, { method: 'POST', body: suspend ? { reason } : {} }),
    onSuccess: () => {
      // Every view that shows shop statuses or counts is now out of date.
      void qc.invalidateQueries({ queryKey: ['tenants'] })
      void qc.invalidateQueries({ queryKey: ['overview'] })
      void qc.invalidateQueries({ queryKey: ['audit'] })
      onClose()
    },
  })

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    change.mutate()
  }

  const tooShort = suspend && reason.trim().length < 3

  return (
    <dialog
      ref={ref}
      onClose={onClose}
      className="m-auto w-[min(28rem,calc(100vw-2rem))] rounded-xl border border-line bg-surface p-0 text-ink backdrop:bg-black/40"
    >
      <form onSubmit={onSubmit} className="space-y-4 p-5">
        <h2 className="text-lg font-semibold">
          {suspend ? 'ระงับร้าน' : 'เปิดใช้งานร้าน'} “{tenant.name}”
        </h2>
        {suspend
          ? (
              <>
                <p className="text-sm text-ink-muted">
                  หน้าร้านจะหายไปทันที และสมาชิกของร้านจะใช้งานหลังร้านไม่ได้ จนกว่าจะเปิดใช้งานอีกครั้ง
                  เหตุผลจะแสดงให้เจ้าของร้านเห็น และถูกบันทึกพร้อมชื่อของคุณ
                </p>
                <label className="block space-y-1 text-sm">
                  <span>เหตุผล</span>
                  <textarea
                    value={reason}
                    onChange={e => setReason(e.target.value)}
                    maxLength={500}
                    rows={3}
                    required
                    className="w-full rounded-md border border-line bg-surface px-3 py-2 outline-accent"
                  />
                </label>
              </>
            )
          : <p className="text-sm text-ink-muted">หน้าร้านจะกลับมาแสดง และสมาชิกของร้านจะใช้งานได้ตามปกติ</p>}

        {change.isError && <p role="alert" className="rounded-md bg-danger-soft px-3 py-2 text-sm text-danger">{errorMessage(change.error)}</p>}

        <div className="flex justify-end gap-2">
          <button type="button" onClick={() => ref.current?.close()} className="rounded-md border border-line px-4 py-2 text-sm">
            ยกเลิก
          </button>
          <button
            type="submit"
            disabled={change.isPending || tooShort}
            className={`rounded-md px-4 py-2 text-sm font-medium text-white disabled:opacity-50 ${suspend ? 'bg-danger' : 'bg-accent'}`}
          >
            {suspend ? 'ระงับร้าน' : 'เปิดใช้งาน'}
          </button>
        </div>
      </form>
    </dialog>
  )
}
