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
import { z } from 'zod'

// ============================================================================
// Proxy Schema & Types
// ============================================================================

export const PROXY_PROTOCOLS = ['http', 'https', 'socks5', 'socks5h'] as const
export type ProxyProtocol = (typeof PROXY_PROTOCOLS)[number]

export const PROXY_STATUSES = ['active', 'inactive', 'expired'] as const
export type ProxyStatus = (typeof PROXY_STATUSES)[number]

export const FALLBACK_MODES = ['none', 'proxy', 'direct'] as const
export type FallbackMode = (typeof FALLBACK_MODES)[number]

export const proxySchema = z.object({
  id: z.number(),
  name: z.string(),
  protocol: z.enum(PROXY_PROTOCOLS),
  host: z.string(),
  port: z.number(),
  username: z.string().optional().default(''),
  password: z.string().optional().default(''),
  status: z.enum(PROXY_STATUSES),
  expires_at: z.number(),
  fallback_mode: z.enum(FALLBACK_MODES),
  backup_proxy_id: z.number(),
  expiry_warn_days: z.number(),
  channel_count: z.number().optional(),
  latency_ms: z.number().nullable().optional(),
  latency_status: z.string().optional(),
  latency_message: z.string().optional(),
  ip_address: z.string().optional(),
  country: z.string().optional(),
  country_code: z.string().optional(),
  region: z.string().optional(),
  city: z.string().optional(),
  quality_status: z.string().optional(),
  quality_score: z.number().optional(),
  quality_grade: z.string().optional(),
  quality_summary: z.string().optional(),
  quality_checked: z.number().optional(),
  created_time: z.number(),
  updated_time: z.number(),
})

export type Proxy = z.infer<typeof proxySchema>

// ============================================================================
// API Request/Response Types
// ============================================================================

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface GetProxiesParams {
  p?: number
  page_size?: number
  protocol?: string
  status?: string
  search?: string
  sort_by?: string
  sort_order?: string
}

export interface GetProxiesResponse {
  success: boolean
  message?: string
  data?: {
    items: Proxy[]
    total: number
    page: number
    page_size: number
  }
}

export interface ProxyFormData {
  id?: number
  name: string
  protocol: ProxyProtocol
  host: string
  port: number
  username?: string
  password?: string
  status?: ProxyStatus
  expires_at?: number
  fallback_mode?: FallbackMode
  backup_proxy_id?: number
  expiry_warn_days?: number
}

export interface BatchProxyItem {
  protocol: ProxyProtocol | string
  host: string
  port: number
  username?: string
  password?: string
  name?: string
}

export interface BatchCreateResult {
  created: number
  skipped: number
  items?: Proxy[]
}

export interface BatchDeleteSkipped {
  id: number
  reason: string
}

export interface BatchDeleteResult {
  deleted_ids: number[]
  skipped: BatchDeleteSkipped[]
}

export interface ProxyTestResult {
  success: boolean
  message?: string
  latency_ms?: number
  ip_address?: string
  city?: string
  region?: string
  country?: string
  country_code?: string
}

export interface ProxyQualityItem {
  name?: string
  target?: string
  status?: string
  latency_ms?: number
  message?: string
  score?: number
}

export interface ProxyQualityResult {
  success?: boolean
  message?: string
  score?: number
  grade?: string
  status?: string
  summary?: string
  items?: ProxyQualityItem[]
  latency_ms?: number
  ip_address?: string
  city?: string
  region?: string
  country?: string
  country_code?: string
}

export interface ProxyChannelSummary {
  id: number
  name: string
  type?: number
  status?: number
  group?: string
  bound?: boolean
}

export interface ProxyExportPayload {
  type: string
  version: number
  exported_at?: string
  proxies: Array<Record<string, unknown>>
}

export interface ProxyImportResult {
  created?: number
  reused?: number
  errors?: Array<{ name?: string; reason?: string; message?: string }>
  skipped?: number
}

// ============================================================================
// Dialog Types
// ============================================================================

export type ProxiesDialogType =
  | 'create'
  | 'update'
  | 'delete'
  | 'bulk-delete'
  | 'quality'
  | 'channels'
  | 'import'
  | 'export'
