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
import { api } from '@/lib/api'

import type {
  ApiResponse,
  BatchCreateResult,
  BatchDeleteResult,
  BatchProxyItem,
  GetProxiesParams,
  GetProxiesResponse,
  Proxy,
  ProxyChannelSummary,
  ProxyExportPayload,
  ProxyFormData,
  ProxyImportResult,
  ProxyQualityResult,
  ProxyTestResult,
} from './types'

// ============================================================================
// Proxy Management
// ============================================================================

export async function getProxies(
  params: GetProxiesParams = {}
): Promise<GetProxiesResponse> {
  const {
    p = 1,
    page_size = 20,
    protocol = '',
    status = '',
    search = '',
    sort_by = '',
    sort_order = '',
  } = params
  const queryParams = new URLSearchParams()
  queryParams.set('p', String(p))
  queryParams.set('page_size', String(page_size))
  if (protocol) queryParams.set('protocol', protocol)
  if (status) queryParams.set('status', status)
  if (search) queryParams.set('search', search)
  if (sort_by) queryParams.set('sort_by', sort_by)
  if (sort_order) queryParams.set('sort_order', sort_order)
  const res = await api.get(`/api/proxy/?${queryParams.toString()}`)
  return res.data
}

export async function getAllProxies(
  withCount = false
): Promise<ApiResponse<Proxy[]>> {
  const query = withCount ? '?with_count=true' : ''
  const res = await api.get(`/api/proxy/all${query}`)
  return res.data
}

export async function getProxy(id: number): Promise<ApiResponse<Proxy>> {
  const res = await api.get(`/api/proxy/${id}`)
  return res.data
}

export async function createProxy(
  data: ProxyFormData
): Promise<ApiResponse<Proxy>> {
  const res = await api.post('/api/proxy/', data)
  return res.data
}

export async function updateProxy(
  id: number,
  data: ProxyFormData
): Promise<ApiResponse<Proxy>> {
  const res = await api.put(`/api/proxy/${id}`, data)
  return res.data
}

export async function deleteProxy(id: number): Promise<ApiResponse> {
  const res = await api.delete(`/api/proxy/${id}`)
  return res.data
}

export async function batchCreateProxies(
  proxies: BatchProxyItem[]
): Promise<ApiResponse<BatchCreateResult>> {
  const res = await api.post('/api/proxy/batch', { proxies })
  return res.data
}

export async function batchDeleteProxies(
  ids: number[]
): Promise<ApiResponse<BatchDeleteResult>> {
  const res = await api.post('/api/proxy/batch-delete', { ids })
  return res.data
}

export async function testProxy(
  id: number
): Promise<ApiResponse<ProxyTestResult>> {
  const res = await api.post(`/api/proxy/${id}/test`)
  return res.data
}

export async function checkProxyQuality(
  id: number
): Promise<ApiResponse<ProxyQualityResult>> {
  const res = await api.post(`/api/proxy/${id}/quality-check`)
  return res.data
}

export async function getProxyChannels(
  id: number
): Promise<ApiResponse<ProxyChannelSummary[]>> {
  const res = await api.get(`/api/proxy/${id}/channels`)
  return res.data
}

export async function exportProxies(params?: {
  ids?: number[]
  protocol?: string
  status?: string
  search?: string
}): Promise<ApiResponse<ProxyExportPayload>> {
  const queryParams = new URLSearchParams()
  if (params?.ids?.length) queryParams.set('ids', params.ids.join(','))
  if (params?.protocol) queryParams.set('protocol', params.protocol)
  if (params?.status) queryParams.set('status', params.status)
  if (params?.search) queryParams.set('search', params.search)
  const qs = queryParams.toString()
  const res = await api.get(`/api/proxy/data${qs ? `?${qs}` : ''}`)
  return res.data
}

export async function importProxies(
  payload: ProxyExportPayload | { proxies: unknown[] }
): Promise<ApiResponse<ProxyImportResult>> {
  const res = await api.post('/api/proxy/data', payload)
  return res.data
}
