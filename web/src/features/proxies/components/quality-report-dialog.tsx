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
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { useProxies } from './proxies-provider'

function qualityVariant(
  status?: string
): 'success' | 'warning' | 'danger' | 'neutral' {
  const s = (status || '').toLowerCase()
  if (s === 'pass' || s === 'ok' || s === 'success') return 'success'
  if (s === 'warn' || s === 'warning' || s === 'challenge') return 'warning'
  if (s === 'fail' || s === 'error') return 'danger'
  return 'neutral'
}

export function QualityReportDialog() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow, qualityResult, setQualityResult } =
    useProxies()

  const isOpen = open === 'quality'
  const items = qualityResult?.items || []

  return (
    <Dialog
      open={isOpen}
      onOpenChange={(v) => {
        if (!v) {
          setOpen(null)
          setQualityResult(null)
        }
      }}
      title={t('Quality Check')}
      description={
        currentRow
          ? t('Quality report for {{name}}', { name: currentRow.name })
          : undefined
      }
      contentHeight='auto'
      bodyClassName='space-y-4'
    >
      <div className='flex flex-wrap items-center gap-3'>
        {qualityResult?.grade && (
          <StatusBadge
            label={`${t('Grade')}: ${String(qualityResult.grade).toUpperCase()}`}
            variant='neutral'
            copyable={false}
          />
        )}
        {qualityResult?.score != null && (
          <StatusBadge
            label={`${t('Score')}: ${qualityResult.score}`}
            variant='neutral'
            copyable={false}
          />
        )}
        {qualityResult?.status && (
          <StatusBadge
            label={String(qualityResult.status)}
            variant={qualityVariant(qualityResult.status)}
            copyable={false}
          />
        )}
      </div>

      {qualityResult?.summary && (
        <p className='text-muted-foreground text-sm'>{qualityResult.summary}</p>
      )}

      {items.length > 0 ? (
        <div className='rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Target')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Latency')}</TableHead>
                <TableHead>{t('Message')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item, index) => (
                <TableRow key={`${item.target || item.name || index}`}>
                  <TableCell className='font-medium'>
                    {item.name || item.target || '-'}
                  </TableCell>
                  <TableCell>
                    <StatusBadge
                      label={String(item.status || '-')}
                      variant={qualityVariant(item.status)}
                      copyable={false}
                    />
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {item.latency_ms != null ? `${item.latency_ms} ms` : '-'}
                  </TableCell>
                  <TableCell className='text-muted-foreground max-w-[200px] truncate text-xs'>
                    {item.message || '-'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        <p className='text-muted-foreground text-sm'>
          {qualityResult?.message || t('No quality details available')}
        </p>
      )}
    </Dialog>
  )
}
