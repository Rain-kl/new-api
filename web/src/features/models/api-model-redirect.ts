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

export type ModelRedirectTarget = {
  id?: number
  redirect_id?: number
  priority: number
  /** Reserved for future weighted LB among same priority. 0 = equal share. */
  weight?: number
  channel_id: number
  model: string
  enabled: boolean
}

export type ModelRedirect = {
  id: number
  name: string
  groups: string
  enabled: boolean
  remark: string
  created_at: number
  updated_at: number
  targets?: ModelRedirectTarget[]
}

export type ModelRedirectInput = {
  name: string
  groups: string[]
  enabled?: boolean
  remark?: string
  targets: Array<{
    priority: number
    /** Reserved for future weighted LB. Omit or 0 = equal share. */
    weight?: number
    channel_id: number
    model?: string
    enabled?: boolean
  }>
}

type ApiResult<T> = {
  success: boolean
  message?: string
  data?: T
}

export async function listModelRedirects(): Promise<ModelRedirect[]> {
  const res = await api.get('/api/model_redirect/')
  const body = res.data as ApiResult<ModelRedirect[]>
  if (!body.success) throw new Error(body.message || 'Failed to load')
  return body.data ?? []
}

export async function createModelRedirect(
  input: ModelRedirectInput
): Promise<ModelRedirect> {
  const res = await api.post('/api/model_redirect/', input)
  const body = res.data as ApiResult<ModelRedirect>
  if (!body.success) throw new Error(body.message || 'Failed to create')
  return body.data!
}

export async function updateModelRedirect(
  id: number,
  input: ModelRedirectInput
): Promise<ModelRedirect> {
  const res = await api.put('/api/model_redirect/', { id, ...input })
  const body = res.data as ApiResult<ModelRedirect>
  if (!body.success) throw new Error(body.message || 'Failed to update')
  return body.data!
}

export async function deleteModelRedirect(id: number): Promise<void> {
  const res = await api.delete(`/api/model_redirect/${id}`)
  const body = res.data as ApiResult<unknown>
  if (!body.success) throw new Error(body.message || 'Failed to delete')
}

export async function updateModelRedirectStatus(
  id: number,
  enabled: boolean
): Promise<void> {
  const res = await api.put(`/api/model_redirect/${id}/status`, { enabled })
  const body = res.data as ApiResult<unknown>
  if (!body.success) throw new Error(body.message || 'Failed to update status')
}
