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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
import { LoadingState } from '@/components/loading-state'

import { getProxyChannels } from '../api'
import { ERROR_MESSAGES } from '../constants'
import { useProxies } from './proxies-provider'

export function ProxiesChannelsDialog() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow } = useProxies()
  const isOpen = open === 'channels'
  const proxyId = currentRow?.id

  const { data, isLoading } = useQuery({
    queryKey: ['proxies', proxyId, 'channels'],
    queryFn: async () => {
      if (!proxyId) return []
      const result = await getProxyChannels(proxyId)
      if (!result.success) {
        toast.error(result.message || t(ERROR_MESSAGES.CHANNELS_FAILED))
        return []
      }
      return result.data || []
    },
    enabled: isOpen && !!proxyId,
  })

  const channels = data || []

  return (
    <Dialog
      open={isOpen}
      onOpenChange={(v) => !v && setOpen(null)}
      title={t('Bound Channels')}
      description={
        currentRow
          ? t('Channels using proxy {{name}}', { name: currentRow.name })
          : undefined
      }
      contentHeight='auto'
      bodyClassName='space-y-3'
    >
      {isLoading ? (
        <LoadingState />
      ) : channels.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No channels are bound to this proxy')}
        </p>
      ) : (
        <div className='rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('ID')}</TableHead>
                <TableHead>{t('Name')}</TableHead>
                <TableHead>{t('Type')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {channels.map((ch) => (
                <TableRow key={ch.id}>
                  <TableCell className='font-mono text-xs'>{ch.id}</TableCell>
                  <TableCell className='font-medium'>{ch.name}</TableCell>
                  <TableCell className='text-muted-foreground text-sm'>
                    {ch.type ?? '-'}
                  </TableCell>
                  <TableCell>
                    {ch.status === 1 ? (
                      <StatusBadge
                        label={t('Enabled')}
                        variant='success'
                        copyable={false}
                      />
                    ) : (
                      <StatusBadge
                        label={t('Disabled')}
                        variant='neutral'
                        copyable={false}
                      />
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Dialog>
  )
}
