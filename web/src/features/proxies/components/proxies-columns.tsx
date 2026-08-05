/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { type ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { formatTimestampToDate } from '@/lib/format'

import { PROXY_STATUSES } from '../constants'
import {
  formatProxyAddress,
  formatProxyLocation,
  isProxyExpiringSoon,
  isTimestampExpired,
  maskAuth,
} from '../lib'
import type { Proxy } from '../types'
import { DataTableRowActions } from './data-table-row-actions'
import { useProxies } from './proxies-provider'

export function useProxiesColumns(): ColumnDef<Proxy>[] {
  const { t } = useTranslation()
  const { setOpen, setCurrentRow } = useProxies()

  return [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={table.getIsAllPageRowsSelected()}
          indeterminate={table.getIsSomePageRowsSelected()}
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          aria-label={t('Select all')}
          className='translate-y-[2px]'
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label={t('Select row')}
          className='translate-y-[2px]'
        />
      ),
      enableSorting: false,
      enableHiding: false,
      size: 40,
    },
    {
      accessorKey: 'id',
      header: t('ID'),
      meta: { mobileHidden: true },
      cell: ({ row }) => (
        <TableId value={row.getValue('id') as number} className='w-[60px]' />
      ),
      size: 80,
    },
    {
      accessorKey: 'name',
      header: t('Name'),
      meta: { mobileTitle: true },
      cell: ({ row }) => (
        <span className='font-medium'>{row.getValue('name')}</span>
      ),
      size: 160,
    },
    {
      accessorKey: 'protocol',
      header: t('Protocol'),
      cell: ({ row }) => {
        const protocol = row.getValue('protocol') as string
        return (
          <StatusBadge
            label={protocol.toUpperCase()}
            variant='neutral'
            copyable={false}
            className='-ml-1.5'
          />
        )
      },
      filterFn: (row, id, value) => {
        return value.includes(String(row.getValue(id)))
      },
      size: 100,
    },
    {
      id: 'address',
      header: t('Address'),
      cell: ({ row }) => {
        const proxy = row.original
        const address = formatProxyAddress(
          proxy.protocol,
          proxy.host,
          proxy.port
        )
        return (
          <div className='flex max-w-[220px] items-center gap-1'>
            <span className='truncate font-mono text-xs' title={address}>
              {address}
            </span>
            <CopyButton
              value={address}
              size='icon'
              variant='ghost'
              className='size-7 shrink-0'
              tooltip={t('Copy address')}
            />
          </div>
        )
      },
      size: 220,
    },
    {
      id: 'auth',
      header: t('Auth'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const proxy = row.original
        const masked = maskAuth(proxy.username, proxy.password)
        if (!masked) {
          return <span className='text-muted-foreground text-sm'>-</span>
        }
        return (
          <span className='font-mono text-xs' title={masked}>
            {masked}
          </span>
        )
      },
      size: 120,
    },
    {
      id: 'location',
      header: t('Location'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const location = formatProxyLocation(row.original)
        return (
          <span className='text-muted-foreground max-w-[160px] truncate text-sm'>
            {location}
          </span>
        )
      },
      size: 160,
    },
    {
      accessorKey: 'channel_count',
      header: t('Channels'),
      cell: ({ row }) => {
        const count = (row.original.channel_count ?? 0) as number
        return (
          <Button
            variant='ghost'
            size='sm'
            className='h-7 px-2 font-mono'
            onClick={() => {
              setCurrentRow(row.original)
              setOpen('channels')
            }}
          >
            {count}
          </Button>
        )
      },
      size: 90,
    },
    {
      id: 'latency_quality',
      header: t('Latency / Quality'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const proxy = row.original
        const latency =
          proxy.latency_ms != null && proxy.latency_ms >= 0
            ? `${proxy.latency_ms} ms`
            : '-'
        const grade = proxy.quality_grade
          ? String(proxy.quality_grade).toUpperCase()
          : ''
        const score =
          proxy.quality_score != null ? String(proxy.quality_score) : ''
        const qualityLabel = grade
          ? score
            ? `${grade} (${score})`
            : grade
          : score || '-'

        return (
          <div className='flex min-w-[100px] flex-col gap-0.5 text-xs'>
            <span className='font-mono'>{latency}</span>
            <span className='text-muted-foreground'>{qualityLabel}</span>
          </div>
        )
      },
      size: 130,
    },
    {
      accessorKey: 'expires_at',
      header: t('Expires'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const expiresAt = row.getValue('expires_at') as number
        if (!expiresAt || expiresAt === 0) {
          return (
            <StatusBadge
              label={t('Never expires')}
              variant='neutral'
              copyable={false}
              className='-ml-1.5'
            />
          )
        }
        const expired = isTimestampExpired(expiresAt)
        const expiringSoon = isProxyExpiringSoon(
          expiresAt,
          row.original.expiry_warn_days ?? 7
        )
        return (
          <div
            className={`min-w-[140px] font-mono text-sm ${
              expired
                ? 'text-destructive'
                : expiringSoon
                  ? 'text-amber-600 dark:text-amber-400'
                  : ''
            }`}
          >
            {formatTimestampToDate(expiresAt)}
          </div>
        )
      },
      size: 160,
    },
    {
      accessorKey: 'status',
      header: t('Status'),
      meta: { mobileBadge: true },
      cell: ({ row }) => {
        const statusValue = row.getValue('status') as keyof typeof PROXY_STATUSES
        const statusConfig = PROXY_STATUSES[statusValue]
        if (!statusConfig) {
          return (
            <StatusBadge
              label={String(statusValue)}
              variant='neutral'
              copyable={false}
              className='-ml-1.5'
            />
          )
        }
        return (
          <StatusBadge
            label={t(statusConfig.labelKey)}
            variant={statusConfig.variant}
            copyable={false}
            className='-ml-1.5'
          />
        )
      },
      filterFn: (row, id, value) => {
        return value.includes(String(row.getValue(id)))
      },
      size: 110,
    },
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => <DataTableRowActions row={row} />,
      meta: { pinned: 'right' as const },
    },
  ]
}
