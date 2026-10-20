import { useQuery } from '@tanstack/react-query'
import { formatDateTime, formatTHB, type PlatformAuditEntry, type PlatformOverview } from '@mscs/shared'
import { Link } from 'react-router'
import { api, errorMessage } from '../api'
import { DailyOrdersChart } from '../components/DailyOrdersChart'

function StatTile({ label, value, note }: { label: string, value: string, note?: string }) {
  return (
    <div className="rounded-xl border border-line bg-surface p-4">
      <p className="text-sm text-ink-muted">{label}</p>
      <p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p>
      {note && <p className="mt-0.5 text-xs text-ink-muted">{note}</p>}
    </div>
  )
}

const ACTION_LABEL: Record<PlatformAuditEntry['action'], string> = {
  'tenant.suspend': 'ระงับร้าน',
  'tenant.reactivate': 'เปิดใช้งานร้าน',
}

export function OverviewPage() {
  const overview = useQuery({ queryKey: ['overview'], queryFn: () => api<PlatformOverview>('/overview') })
  const audit = useQuery({ queryKey: ['audit'], queryFn: () => api<{ items: PlatformAuditEntry[] }>('/audit') })

  if (overview.isPending) return <p className="text-sm text-ink-muted">กำลังโหลด…</p>
  if (overview.isError) return <p className="text-sm text-danger">{errorMessage(overview.error)}</p>
  const o = overview.data

  return (
    <div className="space-y-6">
      <h1 className="text-xl font-semibold">ภาพรวมแพลตฟอร์ม</h1>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile
          label="ร้านค้าทั้งหมด"
          value={o.tenants.total.toLocaleString('th-TH')}
          note={`ใช้งาน ${o.tenants.active} · ถูกระงับ ${o.tenants.suspended}`}
        />
        <StatTile label="ผู้ใช้ทั้งหมด" value={o.users.toLocaleString('th-TH')} note="เจ้าของร้านและพนักงาน" />
        <StatTile label="ออเดอร์ 30 วัน" value={o.orders.toLocaleString('th-TH')} note="ทุกช่องทาง ทุกสถานะ" />
        <StatTile label="ยอดขาย 30 วัน" value={formatTHB(o.revenue_satang)} note="เฉพาะออเดอร์ที่ชำระแล้ว" />
      </div>

      <DailyOrdersChart points={o.daily} />

      <section className="rounded-xl border border-line bg-surface p-4">
        <h2 className="mb-3 font-semibold">การดำเนินการล่าสุดของผู้ดูแล</h2>
        {audit.isError && <p className="text-sm text-danger">{errorMessage(audit.error)}</p>}
        {audit.data?.items.length === 0 && <p className="text-sm text-ink-muted">ยังไม่มีการระงับหรือเปิดใช้งานร้าน</p>}
        <ul className="divide-y divide-line text-sm">
          {audit.data?.items.map(a => (
            <li key={a.id} className="flex flex-wrap items-baseline gap-x-2 py-2">
              <span className={`rounded px-1.5 text-xs ${a.action === 'tenant.suspend' ? 'bg-danger-soft text-danger' : 'bg-ok-soft'}`}>
                {ACTION_LABEL[a.action]}
              </span>
              <Link to={`/tenants?q=${encodeURIComponent(a.tenant_name)}`} className="font-medium hover:underline">{a.tenant_name}</Link>
              {a.reason && <span className="text-ink-muted">“{a.reason}”</span>}
              <span className="ml-auto text-xs text-ink-muted">{a.actor_email} · {formatDateTime(a.created_at)}</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
