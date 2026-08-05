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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import {
  FALLBACK_MODE_VALUES,
  PROXY_PROTOCOL_VALUES,
  PROXY_STATUS_VALUES,
  PROXY_VALIDATION,
  getProxyFormErrorMessages,
} from '../constants'
import type { Proxy, ProxyFormData } from '../types'

export function getProxyFormSchema(t: TFunction) {
  const msg = getProxyFormErrorMessages(t)
  return z
    .object({
      name: z
        .string()
        .min(PROXY_VALIDATION.NAME_MIN_LENGTH, msg.NAME_REQUIRED)
        .max(PROXY_VALIDATION.NAME_MAX_LENGTH, msg.NAME_REQUIRED),
      protocol: z.enum(PROXY_PROTOCOL_VALUES),
      host: z
        .string()
        .min(PROXY_VALIDATION.HOST_MIN_LENGTH, msg.HOST_REQUIRED)
        .max(PROXY_VALIDATION.HOST_MAX_LENGTH, msg.HOST_REQUIRED),
      port: z
        .number()
        .int()
        .min(PROXY_VALIDATION.PORT_MIN, msg.PORT_INVALID)
        .max(PROXY_VALIDATION.PORT_MAX, msg.PORT_INVALID),
      username: z.string().optional(),
      password: z.string().optional(),
      expires_at: z.date().optional(),
      fallback_mode: z.enum(FALLBACK_MODE_VALUES),
      backup_proxy_id: z.number().optional(),
      expiry_warn_days: z
        .number()
        .int()
        .min(PROXY_VALIDATION.EXPIRY_WARN_DAYS_MIN)
        .max(PROXY_VALIDATION.EXPIRY_WARN_DAYS_MAX),
      status: z.enum(PROXY_STATUS_VALUES).optional(),
    })
    .superRefine((data, ctx) => {
      if (data.fallback_mode === 'proxy' && !data.backup_proxy_id) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: msg.BACKUP_REQUIRED,
          path: ['backup_proxy_id'],
        })
      }
    })
}

export type ProxyFormValues = {
  name: string
  protocol: (typeof PROXY_PROTOCOL_VALUES)[number]
  host: string
  port: number
  username?: string
  password?: string
  expires_at?: Date
  fallback_mode: (typeof FALLBACK_MODE_VALUES)[number]
  backup_proxy_id?: number
  expiry_warn_days: number
  status?: (typeof PROXY_STATUS_VALUES)[number]
}

export const PROXY_FORM_DEFAULT_VALUES: ProxyFormValues = {
  name: '',
  protocol: 'http',
  host: '',
  port: 8080,
  username: '',
  password: '',
  expires_at: undefined,
  fallback_mode: 'none',
  backup_proxy_id: 0,
  expiry_warn_days: PROXY_VALIDATION.EXPIRY_WARN_DAYS_DEFAULT,
  status: 'active',
}

export function transformFormDataToPayload(
  data: ProxyFormValues
): ProxyFormData {
  return {
    name: data.name.trim(),
    protocol: data.protocol,
    host: data.host.trim(),
    port: data.port,
    username: data.username?.trim() || '',
    password: data.password?.trim() || '',
    expires_at: data.expires_at
      ? Math.floor(data.expires_at.getTime() / 1000)
      : 0,
    fallback_mode: data.fallback_mode,
    backup_proxy_id:
      data.fallback_mode === 'proxy' ? data.backup_proxy_id || 0 : 0,
    expiry_warn_days:
      data.expiry_warn_days ?? PROXY_VALIDATION.EXPIRY_WARN_DAYS_DEFAULT,
    status: data.status,
  }
}

export function transformProxyToFormDefaults(proxy: Proxy): ProxyFormValues {
  return {
    name: proxy.name,
    protocol: proxy.protocol,
    host: proxy.host,
    port: proxy.port,
    username: proxy.username || '',
    password: proxy.password || '',
    expires_at:
      proxy.expires_at > 0 ? new Date(proxy.expires_at * 1000) : undefined,
    fallback_mode: proxy.fallback_mode || 'none',
    backup_proxy_id: proxy.backup_proxy_id || 0,
    expiry_warn_days:
      proxy.expiry_warn_days ?? PROXY_VALIDATION.EXPIRY_WARN_DAYS_DEFAULT,
    status: proxy.status,
  }
}
