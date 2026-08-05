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
import { getRouteApi } from '@tanstack/react-router'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DISABLED_ROW_DESKTOP,
  DISABLED_ROW_MOBILE,
  DataTablePage,
  useDataTable,
} from '@/components/data-table'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'

import { getProxies } from '../api'
import {
  ERROR_MESSAGES,
  getProxyProtocolOptions,
  getProxyStatusOptions,
} from '../constants'
import type { Proxy } from '../types'
import { DataTableBulkActions } from './data-table-bulk-actions'
import { useProxiesColumns } from './proxies-columns'
import { useProxies } from './proxies-provider'

const route = getRouteApi('/_authenticated/proxies/')

function isDisabledProxyRow(proxy: Proxy) {
  return proxy.status !== 'active'
}

export function ProxiesTable() {
  const { t } = useTranslation()
  const columns = useProxiesColumns()
  const { refreshTrigger } = useProxies()
  const isMobile = useMediaQuery('(max-width: 640px)')

  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 10 : 20 },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [
      { columnId: 'status', searchKey: 'status', type: 'array' },
      { columnId: 'protocol', searchKey: 'protocol', type: 'array' },
    ],
  })

  const statusFilter =
    (columnFilters.find((filter) => filter.id === 'status')?.value as
      | string[]
      | undefined) ?? []
  const statusFilterValue = statusFilter[0] ?? ''

  const protocolFilter =
    (columnFilters.find((filter) => filter.id === 'protocol')?.value as
      | string[]
      | undefined) ?? []
  const protocolFilterValue = protocolFilter[0] ?? ''

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'proxies',
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
      statusFilterValue,
      protocolFilterValue,
      refreshTrigger,
    ],
    queryFn: async () => {
      const result = await getProxies({
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        search: globalFilter?.trim() || '',
        status: statusFilterValue,
        protocol: protocolFilterValue,
      })

      if (!result.success) {
        toast.error(result.message || t(ERROR_MESSAGES.LOAD_FAILED))
        return { items: [], total: 0 }
      }

      return {
        items: result.data?.items || [],
        total: result.data?.total || 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const proxies = data?.items || []

  const { table } = useDataTable({
    data: proxies,
    columns,
    enableRowSelection: true,
    columnFilters,
    globalFilter,
    pagination,
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  const statusOptions = useMemo(() => getProxyStatusOptions(t), [t])
  const protocolOptions = useMemo(() => getProxyProtocolOptions(t), [t])

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No Proxies Found')}
      emptyDescription={t(
        'No proxies available. Create your first proxy to get started.'
      )}
      skeletonKeyPrefix='proxies-skeleton'
      applyHeaderSize
      toolbarProps={{
        searchPlaceholder: t('Filter by name, host, or IP...'),
        filters: [
          {
            columnId: 'status',
            title: t('Status'),
            options: statusOptions,
            singleSelect: true,
          },
          {
            columnId: 'protocol',
            title: t('Protocol'),
            options: protocolOptions,
            singleSelect: true,
          },
        ],
      }}
      getRowClassName={(row, { isMobile: mobile }) => {
        if (!isDisabledProxyRow(row.original)) return undefined
        return mobile ? DISABLED_ROW_MOBILE : DISABLED_ROW_DESKTOP
      }}
      bulkActions={<DataTableBulkActions table={table} />}
    />
  )
}
