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
import type { Row } from '@tanstack/react-table'
import {
  Activity,
  Edit,
  Gauge,
  ListTree,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import { Button } from '@/components/ui/button'
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { checkProxyQuality, testProxy } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import type { Proxy } from '../types'
import { useProxies } from './proxies-provider'

interface DataTableRowActionsProps<TData> {
  row: Row<TData>
}

export function DataTableRowActions<TData>({
  row,
}: DataTableRowActionsProps<TData>) {
  const { t } = useTranslation()
  const proxy = row.original as Proxy
  const {
    setOpen,
    setCurrentRow,
    setQualityResult,
    triggerRefresh,
  } = useProxies()
  const [isTesting, setIsTesting] = useState(false)
  const [isChecking, setIsChecking] = useState(false)

  const handleTest = async () => {
    setIsTesting(true)
    try {
      const result = await testProxy(proxy.id)
      const data = result.data
      // Probe failure is returned as business success with data.success=false
      if (result.success && data?.success !== false) {
        const latency =
          data?.latency_ms != null ? `${data.latency_ms} ms` : undefined
        const location = [data?.city, data?.country].filter(Boolean).join(', ')
        toast.success(
          [
            t(SUCCESS_MESSAGES.TEST_SUCCESS),
            latency,
            location,
            data?.ip_address,
          ]
            .filter(Boolean)
            .join(' · ')
        )
        triggerRefresh()
      } else {
        toast.error(
          data?.message || result.message || t(ERROR_MESSAGES.TEST_FAILED)
        )
        triggerRefresh()
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.TEST_FAILED))
    } finally {
      setIsTesting(false)
    }
  }

  const handleQuality = async () => {
    setIsChecking(true)
    try {
      const result = await checkProxyQuality(proxy.id)
      if (result.success && result.data) {
        setCurrentRow(proxy)
        setQualityResult(result.data)
        setOpen('quality')
        triggerRefresh()
      } else {
        toast.error(result.message || t(ERROR_MESSAGES.QUALITY_FAILED))
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.QUALITY_FAILED))
    } finally {
      setIsChecking(false)
    }
  }

  return (
    <div className='-ml-1.5 flex items-center gap-1'>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              onClick={handleTest}
              disabled={isTesting}
              aria-label={t('Test Connection')}
            />
          }
        >
          <Activity />
        </TooltipTrigger>
        <TooltipContent>{t('Test Connection')}</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              onClick={() => {
                setCurrentRow(proxy)
                setOpen('update')
              }}
              aria-label={t('Edit')}
            />
          }
        >
          <Edit />
        </TooltipTrigger>
        <TooltipContent>{t('Edit')}</TooltipContent>
      </Tooltip>

      <DataTableRowActionMenu ariaLabel={t('Open menu')} modal={false}>
        <DropdownMenuItem onClick={handleTest} disabled={isTesting}>
          {t('Test Connection')}
          <DropdownMenuShortcut>
            <Activity size={16} />
          </DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem onClick={handleQuality} disabled={isChecking}>
          {t('Quality Check')}
          <DropdownMenuShortcut>
            <Gauge size={16} />
          </DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => {
            setCurrentRow(proxy)
            setOpen('channels')
          }}
        >
          {t('Bound Channels')}
          <DropdownMenuShortcut>
            <ListTree size={16} />
          </DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onClick={() => {
            setCurrentRow(proxy)
            setOpen('delete')
          }}
          className='text-destructive focus:text-destructive'
        >
          {t('Delete')}
          <DropdownMenuShortcut>
            <Trash2 size={16} />
          </DropdownMenuShortcut>
        </DropdownMenuItem>
      </DataTableRowActionMenu>
    </div>
  )
}
