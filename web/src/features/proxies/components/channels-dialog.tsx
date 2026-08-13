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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { batchSetChannelProxy } from '@/features/channels/api'

import { getProxyChannels } from '../api'
import { ERROR_MESSAGES } from '../constants'
import type { ProxyChannelSummary } from '../types'
import { useProxies } from './proxies-provider'

export function ProxiesChannelsDialog() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { open, setOpen, currentRow, triggerRefresh } = useProxies()
  const isOpen = open === 'channels'
  const proxyId = currentRow?.id
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [isMutating, setIsMutating] = useState(false)

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

  const channels = useMemo(() => data || [], [data])
  const bound = useMemo(() => channels.filter((ch) => ch.bound), [channels])
  const unbound = useMemo(() => channels.filter((ch) => !ch.bound), [channels])
  const boundIds = useMemo(() => bound.map((ch) => ch.id), [bound])
  const unboundIds = useMemo(() => unbound.map((ch) => ch.id), [unbound])

  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds])
  const selectedBoundIds = useMemo(
    () => bound.filter((ch) => selectedSet.has(ch.id)).map((ch) => ch.id),
    [bound, selectedSet]
  )
  const selectedUnboundIds = useMemo(
    () => unbound.filter((ch) => selectedSet.has(ch.id)).map((ch) => ch.id),
    [unbound, selectedSet]
  )
  const allBoundSelected =
    bound.length > 0 && bound.every((ch) => selectedSet.has(ch.id))
  const allUnboundSelected =
    unbound.length > 0 && unbound.every((ch) => selectedSet.has(ch.id))

  const toggleChannel = (id: number, checked: boolean) => {
    setSelectedIds((prev) =>
      checked ? [...new Set([...prev, id])] : prev.filter((x) => x !== id)
    )
  }

  const toggleAll = (ids: number[], currentlyAll: boolean) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (currentlyAll) {
        ids.forEach((id) => next.delete(id))
      } else {
        ids.forEach((id) => next.add(id))
      }
      return [...next]
    })
  }

  const mutateSelection = async (ids: number[], bind: boolean) => {
    if (!proxyId || ids.length === 0) return
    setIsMutating(true)
    try {
      // proxy_id=0 clears the binding; proxy_id=<id> binds.
      const response = await batchSetChannelProxy(ids, bind ? proxyId : 0)
      if (response.success) {
        toast.success(
          bind
            ? t('Channels bound successfully')
            : t('Channels unbound successfully')
        )
        setSelectedIds([])
        queryClient.invalidateQueries({
          queryKey: ['proxies', proxyId, 'channels'],
        })
        triggerRefresh()
      } else {
        const fallback = bind
          ? t('Failed to bind channels')
          : t('Failed to unbind channels')
        toast.error(response.message || fallback)
      }
    } catch {
      toast.error(
        bind ? t('Failed to bind channels') : t('Failed to unbind channels')
      )
    } finally {
      setIsMutating(false)
    }
  }

  const renderRows = (rows: ProxyChannelSummary[]) =>
    rows.map((ch) => {
      const checked = selectedSet.has(ch.id)
      return (
        <TableRow
          key={ch.id}
          className={checked ? 'bg-primary/5' : undefined}
        >
          <TableCell className='w-10'>
            <Checkbox
              checked={checked}
              onCheckedChange={(value) => toggleChannel(ch.id, !!value)}
              aria-label={ch.name}
              disabled={isMutating}
            />
          </TableCell>
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
      )
    })
  const renderBody = () => {
    if (channels.length === 0) {
      return (
        <p className='text-muted-foreground text-sm'>
          {t('No channels found')}
        </p>
      )
    }
    return (
      <div className='min-h-0 flex-1 overflow-auto rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='w-10' />
              <TableHead>{t('ID')}</TableHead>
              <TableHead>{t('Name')}</TableHead>
              <TableHead>{t('Type')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {bound.length > 0 && (
              <>
                <TableRow className='hover:bg-transparent'>
                  <TableCell
                    colSpan={5}
                    className='bg-muted/50 text-muted-foreground h-auto px-3 py-1.5 text-xs font-medium'
                  >
                    {t('Bound Channels ({{count}})', { count: bound.length })}
                  </TableCell>
                </TableRow>
                {renderRows(bound)}
              </>
            )}
            {unbound.length > 0 && (
              <>
                <TableRow className='hover:bg-transparent'>
                  <TableCell
                    colSpan={5}
                    className='bg-muted/50 text-muted-foreground h-auto px-3 py-1.5 text-xs font-medium'
                  >
                    {t('Unbound Channels ({{count}})', {
                      count: unbound.length,
                    })}
                  </TableCell>
                </TableRow>
                {renderRows(unbound)}
              </>
            )}
          </TableBody>
        </Table>
      </div>
    )
  }
  return (
    <Dialog
      open={isOpen}
      onOpenChange={(v) => {
        if (!v) {
          setSelectedIds([])
          setOpen(null)
        }
      }}
      title={t('Bound Channels')}
      description={
        currentRow
          ? t('Channels using proxy {{name}}', { name: currentRow.name })
          : undefined
      }
      contentHeight='min(70vh, 560px)'
      bodyClassName='flex h-full min-h-0 flex-col gap-3 overflow-hidden'
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={unbound.length === 0 || isMutating}
            onClick={() => toggleAll(unboundIds, allUnboundSelected)}
          >
            {t('Select all unbound')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={bound.length === 0 || isMutating}
            onClick={() => toggleAll(boundIds, allBoundSelected)}
          >
            {t('Select all bound')}
          </Button>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <span className='text-muted-foreground text-xs'>
            {selectedIds.length > 0
              ? `${selectedIds.length} ${t('selected')}`
              : ''}
          </span>
          <Button
            type='button'
            size='sm'
            onClick={() => mutateSelection(selectedUnboundIds, true)}
            disabled={selectedUnboundIds.length === 0 || isMutating}
          >
            {t('Bind Selected')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={() => mutateSelection(selectedBoundIds, false)}
            disabled={selectedBoundIds.length === 0 || isMutating}
          >
            {t('Unbind Selected')}
          </Button>
        </div>
      </div>

      {isLoading ? <LoadingState /> : renderBody()}
    </Dialog>
  )
}
