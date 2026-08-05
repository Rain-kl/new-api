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
import type { Table } from '@tanstack/react-table'
import { Activity, Gauge, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { checkProxyQuality, testProxy } from '../api'
import { ERROR_MESSAGES } from '../constants'
import type { Proxy } from '../types'
import { useProxies } from './proxies-provider'

type DataTableBulkActionsProps<TData> = {
  table: Table<TData>
}

export function DataTableBulkActions<TData>({
  table,
}: DataTableBulkActionsProps<TData>) {
  const { t } = useTranslation()
  const { setOpen, setSelectedIds, triggerRefresh } = useProxies()
  const [isTesting, setIsTesting] = useState(false)
  const [isChecking, setIsChecking] = useState(false)

  const selectedRows = table.getFilteredSelectedRowModel().rows
  const selectedIds = selectedRows.reduce<number[]>((ids, row) => {
    const id = (row.original as Proxy).id
    if (typeof id === 'number') ids.push(id)
    return ids
  }, [])

  const handleBulkDelete = () => {
    setSelectedIds(selectedIds)
    setOpen('bulk-delete')
  }

  const handleBulkTest = async () => {
    if (selectedIds.length === 0) return
    setIsTesting(true)
    let ok = 0
    let fail = 0
    try {
      for (const id of selectedIds) {
        try {
          const result = await testProxy(id)
          if (result.success && result.data?.success !== false) {
            ok += 1
          } else {
            fail += 1
          }
        } catch {
          fail += 1
        }
      }
      toast.success(
        t('Batch test finished: {{ok}} succeeded, {{fail}} failed', {
          ok,
          fail,
        })
      )
      triggerRefresh()
    } catch {
      toast.error(t(ERROR_MESSAGES.TEST_FAILED))
    } finally {
      setIsTesting(false)
    }
  }

  const handleBulkQuality = async () => {
    if (selectedIds.length === 0) return
    setIsChecking(true)
    let ok = 0
    let fail = 0
    try {
      for (const id of selectedIds) {
        try {
          const result = await checkProxyQuality(id)
          if (result.success) ok += 1
          else fail += 1
        } catch {
          fail += 1
        }
      }
      toast.success(
        t('Batch quality check finished: {{ok}} succeeded, {{fail}} failed', {
          ok,
          fail,
        })
      )
      triggerRefresh()
    } catch {
      toast.error(t(ERROR_MESSAGES.QUALITY_FAILED))
    } finally {
      setIsChecking(false)
    }
  }

  return (
    <BulkActionsToolbar table={table} entityName={t('proxy')}>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='outline'
              size='icon'
              onClick={handleBulkTest}
              disabled={isTesting || selectedIds.length === 0}
              className='size-8'
              aria-label={t('Test Connection')}
            />
          }
        >
          <Activity />
        </TooltipTrigger>
        <TooltipContent>
          <p>{t('Test Connection')}</p>
        </TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='outline'
              size='icon'
              onClick={handleBulkQuality}
              disabled={isChecking || selectedIds.length === 0}
              className='size-8'
              aria-label={t('Quality Check')}
            />
          }
        >
          <Gauge />
        </TooltipTrigger>
        <TooltipContent>
          <p>{t('Quality Check')}</p>
        </TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='destructive'
              size='icon'
              onClick={handleBulkDelete}
              disabled={selectedIds.length === 0}
              className='size-8'
              aria-label={t('Delete')}
            />
          }
        >
          <Trash2 />
        </TooltipTrigger>
        <TooltipContent>
          <p>{t('Delete')}</p>
        </TooltipContent>
      </Tooltip>
    </BulkActionsToolbar>
  )
}
