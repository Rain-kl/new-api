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
import { Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

import type { UpstreamChannel } from '../types'
import {
  CHANNEL_STATUS_CONFIG,
  DEFAULT_ENDPOINT,
  ENDPOINT_OPTIONS,
  MODELS_DEV_PRESET_ID,
  OFFICIAL_CHANNEL_ID,
} from './constants'

type ChannelSelectorDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channels: UpstreamChannel[]
  selectedChannelIds: number[]
  onSelectedChannelIdsChange: (ids: number[]) => void
  channelEndpoints: Record<number, string>
  onChannelEndpointsChange: (endpoints: Record<number, string>) => void
  onConfirm: (selectedIds: number[]) => void
  isLoadingChannels?: boolean
}

// Synthesized presets from `controller/ratio_sync.go` always carry stable
// negative IDs, so matching by ID alone is reliable and self-documenting.
function isOfficialChannel(channel: UpstreamChannel): boolean {
  return (
    channel.id === OFFICIAL_CHANNEL_ID || channel.id === MODELS_DEV_PRESET_ID
  )
}

function getEndpointType(endpoint: string): string {
  const option = ENDPOINT_OPTIONS.find((opt) => opt.value === endpoint)
  return option ? endpoint : 'custom'
}

/**
 * Lightweight channel picker.
 *
 * Intentionally avoids DataTable + Base UI Select-in-Dialog:
 * nested Select portals fight the dialog focus trap and have frozen the
 * main thread when this dialog opens. A plain list + native <select> keeps
 * the same UX without that interaction surface.
 */
export function ChannelSelectorDialog({
  open,
  onOpenChange,
  channels,
  selectedChannelIds,
  onSelectedChannelIdsChange,
  channelEndpoints,
  onChannelEndpointsChange,
  onConfirm,
  isLoadingChannels = false,
}: ChannelSelectorDialogProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [pageIndex, setPageIndex] = useState(0)
  const pageSize = 20
  // Parent only mounts this dialog while open, so initial state is enough —
  // no need to sync selectedChannelIds on every parent re-render.
  const [localSelectedIds, setLocalSelectedIds] = useState<number[]>(
    () => selectedChannelIds
  )

  const selectedIdSet = useMemo(
    () => new Set(localSelectedIds),
    [localSelectedIds]
  )

  const sortedChannels = useMemo(() => {
    const searchLower = search.trim().toLowerCase()
    const filtered = searchLower
      ? channels.filter(
          (ch) =>
            ch.name.toLowerCase().includes(searchLower) ||
            ch.base_url.toLowerCase().includes(searchLower)
        )
      : channels

    return [...filtered].sort((a, b) => {
      const aIsOfficial = isOfficialChannel(a)
      const bIsOfficial = isOfficialChannel(b)
      if (aIsOfficial && !bIsOfficial) return -1
      if (!aIsOfficial && bIsOfficial) return 1
      return a.name.localeCompare(b.name)
    })
  }, [channels, search])

  const pageCount = Math.max(1, Math.ceil(sortedChannels.length / pageSize))
  const safePageIndex = Math.min(pageIndex, pageCount - 1)
  const pagedChannels = useMemo(() => {
    const start = safePageIndex * pageSize
    return sortedChannels.slice(start, start + pageSize)
  }, [sortedChannels, safePageIndex, pageSize])

  const allVisibleSelected =
    pagedChannels.length > 0 &&
    pagedChannels.every((ch) => selectedIdSet.has(ch.id))
  const someVisibleSelected =
    !allVisibleSelected && pagedChannels.some((ch) => selectedIdSet.has(ch.id))

  const toggleChannel = (channelId: number, checked: boolean) => {
    setLocalSelectedIds((prev) => {
      if (checked) {
        if (prev.includes(channelId)) return prev
        return [...prev, channelId]
      }
      return prev.filter((id) => id !== channelId)
    })
  }

  const toggleAllVisible = (checked: boolean) => {
    const visibleIds = pagedChannels.map((ch) => ch.id)
    setLocalSelectedIds((prev) => {
      if (checked) {
        const next = new Set(prev)
        visibleIds.forEach((id) => next.add(id))
        return [...next]
      }
      const drop = new Set(visibleIds)
      return prev.filter((id) => !drop.has(id))
    })
  }

  const handleSearchChange = (value: string) => {
    setSearch(value)
    setPageIndex(0)
  }

  const updateEndpoint = (channelId: number, endpoint: string) => {
    onChannelEndpointsChange({
      ...channelEndpoints,
      [channelId]: endpoint,
    })
  }

  const handleEndpointTypeChange = (channelId: number, value: string) => {
    if (value === 'custom') {
      updateEndpoint(channelId, '')
    } else {
      updateEndpoint(channelId, value)
    }
  }

  const handleConfirm = () => {
    onSelectedChannelIdsChange(localSelectedIds)
    onOpenChange(false)
    onConfirm(localSelectedIds)
  }

  const renderChannelListBody = () => {
    if (isLoadingChannels) {
      return (
        <div className='text-muted-foreground flex h-40 items-center justify-center text-sm'>
          {t('Loading...')}
        </div>
      )
    }

    if (sortedChannels.length === 0) {
      return (
        <div className='text-muted-foreground flex h-40 items-center justify-center text-sm'>
          {t('No channels found')}
        </div>
      )
    }

    return (
      <ul className='divide-y'>
        {pagedChannels.map((channel) => {
          const checked = selectedIdSet.has(channel.id)
          const isOfficial = isOfficialChannel(channel)
          const currentEndpoint =
            channelEndpoints[channel.id] || DEFAULT_ENDPOINT
          const endpointType = getEndpointType(currentEndpoint)
          const statusConfig =
            CHANNEL_STATUS_CONFIG[
              channel.status as keyof typeof CHANNEL_STATUS_CONFIG
            ]

          return (
            <li
              key={channel.id}
              className={cn(
                'flex flex-col gap-2 px-3 py-2.5 sm:flex-row sm:items-center sm:gap-3',
                checked && 'bg-primary/5'
              )}
            >
              <div className='flex min-w-0 flex-1 items-start gap-2.5'>
                <Checkbox
                  checked={checked}
                  onCheckedChange={(value) =>
                    toggleChannel(channel.id, !!value)
                  }
                  className='mt-0.5'
                  aria-label={channel.name}
                />
                <div className='min-w-0 flex-1'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='font-medium'>{channel.name}</span>
                    {isOfficial && (
                      <StatusBadge
                        label={t('Official')}
                        variant='success'
                        size='sm'
                        copyable={false}
                      />
                    )}
                    {statusConfig ? (
                      <StatusBadge
                        label={t(statusConfig.label)}
                        variant={statusConfig.variant}
                        size='sm'
                        copyable={false}
                      />
                    ) : null}
                  </div>
                  <p
                    className='text-muted-foreground mt-0.5 truncate font-mono text-xs'
                    title={channel.base_url}
                  >
                    {channel.base_url}
                  </p>
                </div>
              </div>

              <div className='flex min-w-0 items-center gap-2 ps-7 sm:w-[min(100%,28rem)] sm:ps-0'>
                {/* Native select: no portal, no focus-trap fight with Dialog */}
                <select
                  className='border-input bg-background h-8 w-32 shrink-0 rounded-md border px-2 text-sm'
                  value={endpointType}
                  onChange={(e) =>
                    handleEndpointTypeChange(channel.id, e.target.value)
                  }
                  aria-label={t('Sync Endpoint')}
                >
                  {ENDPOINT_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
                {endpointType === 'custom' && (
                  <Input
                    value={currentEndpoint}
                    onChange={(e) =>
                      updateEndpoint(channel.id, e.target.value)
                    }
                    placeholder={t('/your/endpoint')}
                    className='h-8 min-w-0 flex-1 font-mono text-xs'
                  />
                )}
              </div>
            </li>
          )
        })}
      </ul>
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Select Sync Channels')}
      description={t(
        'Choose channels to sync upstream ratio configurations from'
      )}
      contentClassName='flex max-h-[90vh] max-w-[calc(100%-2rem)] flex-col sm:max-w-[90vw] xl:max-w-[1100px]'
      contentHeight='min(72vh, 720px)'
      bodyClassName='flex h-full min-h-0 flex-col overflow-hidden'
      footer={
        <>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleConfirm} disabled={isLoadingChannels}>
            {t('Confirm Selection')}
          </Button>
        </>
      }
    >
      <div className='flex h-full min-h-0 flex-col gap-3 overflow-hidden'>
        <div className='flex shrink-0 items-center gap-3'>
          <div className='relative min-w-0 flex-1'>
            <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2' />
            <Input
              placeholder={t('Search by name or URL...')}
              value={search}
              onChange={(e) => handleSearchChange(e.target.value)}
              className='ps-9'
            />
          </div>
          <label className='text-muted-foreground flex shrink-0 items-center gap-2 text-sm'>
            <Checkbox
              checked={allVisibleSelected}
              indeterminate={someVisibleSelected}
              onCheckedChange={(value) => toggleAllVisible(!!value)}
              disabled={pagedChannels.length === 0}
              aria-label={t('Select all')}
            />
            {t('Select all')}
          </label>
        </div>

        <div className='min-h-0 flex-1 overflow-auto rounded-md border'>
          {renderChannelListBody()}
        </div>

        <div className='text-muted-foreground flex shrink-0 items-center justify-between gap-2 text-xs'>
          <span>
            {t('Total:')} {sortedChannels.length}
            {localSelectedIds.length > 0
              ? ` · ${localSelectedIds.length} ${t('selected')}`
              : null}
          </span>
          {pageCount > 1 ? (
            <div className='flex items-center gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='h-7 px-2'
                disabled={safePageIndex <= 0}
                onClick={() => setPageIndex(Math.max(0, safePageIndex - 1))}
              >
                {t('Previous')}
              </Button>
              <span className='tabular-nums'>
                {safePageIndex + 1} / {pageCount}
              </span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='h-7 px-2'
                disabled={safePageIndex >= pageCount - 1}
                onClick={() =>
                  setPageIndex(Math.min(pageCount - 1, safePageIndex + 1))
                }
              >
                {t('Next')}
              </Button>
            </div>
          ) : null}
        </div>
      </div>
    </Dialog>
  )
}
