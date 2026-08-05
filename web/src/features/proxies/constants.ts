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

import type { StatusBadgeProps } from '@/components/status-badge'

import type { FallbackMode, ProxyProtocol, ProxyStatus } from './types'

// ============================================================================
// Protocol Configuration
// ============================================================================

export const PROXY_PROTOCOL_VALUES = [
  'http',
  'https',
  'socks5',
  'socks5h',
] as const satisfies readonly ProxyProtocol[]

export function getProxyProtocolOptions(_t?: TFunction) {
  return PROXY_PROTOCOL_VALUES.map((value) => ({
    label: value.toUpperCase(),
    value,
  }))
}

// ============================================================================
// Status Configuration
// ============================================================================

export const PROXY_STATUS_VALUES = [
  'active',
  'inactive',
  'expired',
] as const satisfies readonly ProxyStatus[]

export const PROXY_STATUSES: Record<
  ProxyStatus,
  Pick<StatusBadgeProps, 'variant'> & {
    labelKey: string
    value: ProxyStatus
  }
> = {
  active: {
    labelKey: 'Active',
    variant: 'success',
    value: 'active',
  },
  inactive: {
    labelKey: 'Inactive',
    variant: 'neutral',
    value: 'inactive',
  },
  expired: {
    labelKey: 'Expired',
    variant: 'warning',
    value: 'expired',
  },
}

export function getProxyStatusOptions(t: TFunction) {
  return Object.values(PROXY_STATUSES).map((config) => ({
    label: t(config.labelKey),
    value: config.value,
  }))
}

// ============================================================================
// Fallback Mode
// ============================================================================

export const FALLBACK_MODE_VALUES = [
  'none',
  'proxy',
  'direct',
] as const satisfies readonly FallbackMode[]

export function getFallbackModeOptions(t: TFunction) {
  return [
    { label: t('None'), value: 'none' as const },
    { label: t('Backup Proxy'), value: 'proxy' as const },
    { label: t('Direct Connection'), value: 'direct' as const },
  ]
}

// ============================================================================
// Validation
// ============================================================================

export const PROXY_VALIDATION = {
  NAME_MIN_LENGTH: 1,
  NAME_MAX_LENGTH: 100,
  HOST_MIN_LENGTH: 1,
  HOST_MAX_LENGTH: 255,
  PORT_MIN: 1,
  PORT_MAX: 65535,
  EXPIRY_WARN_DAYS_MIN: 0,
  EXPIRY_WARN_DAYS_MAX: 365,
  EXPIRY_WARN_DAYS_DEFAULT: 7,
} as const

// ============================================================================
// Error Messages (i18n keys)
// ============================================================================

export const ERROR_MESSAGES = {
  UNEXPECTED: 'An unexpected error occurred',
  LOAD_FAILED: 'Failed to load proxies',
  CREATE_FAILED: 'Failed to create proxy',
  UPDATE_FAILED: 'Failed to update proxy',
  DELETE_FAILED: 'Failed to delete proxy',
  BATCH_CREATE_FAILED: 'Failed to batch create proxies',
  BATCH_DELETE_FAILED: 'Failed to batch delete proxies',
  TEST_FAILED: 'Failed to test proxy',
  QUALITY_FAILED: 'Failed to run quality check',
  CHANNELS_FAILED: 'Failed to load proxy channels',
  EXPORT_FAILED: 'Failed to export proxies',
  IMPORT_FAILED: 'Failed to import proxies',
  NAME_REQUIRED: 'Name is required',
  HOST_REQUIRED: 'Host is required',
  PORT_INVALID: 'Port must be between 1 and 65535',
  BACKUP_REQUIRED: 'Backup proxy is required when fallback mode is proxy',
  BATCH_LINES_INVALID: 'Invalid proxy URL line format',
} as const

export function getProxyFormErrorMessages(t: TFunction) {
  return {
    NAME_REQUIRED: t(ERROR_MESSAGES.NAME_REQUIRED),
    HOST_REQUIRED: t(ERROR_MESSAGES.HOST_REQUIRED),
    PORT_INVALID: t(ERROR_MESSAGES.PORT_INVALID),
    BACKUP_REQUIRED: t(ERROR_MESSAGES.BACKUP_REQUIRED),
  } as const
}

// ============================================================================
// Success Messages (i18n keys)
// ============================================================================

export const SUCCESS_MESSAGES = {
  PROXY_CREATED: 'Proxy created successfully',
  PROXY_UPDATED: 'Proxy updated successfully',
  PROXY_DELETED: 'Proxy deleted successfully',
  BATCH_CREATED: 'Batch create completed',
  BATCH_DELETED: 'Batch delete completed',
  TEST_SUCCESS: 'Proxy connection test succeeded',
  QUALITY_SUCCESS: 'Quality check completed',
  IMPORT_SUCCESS: 'Proxies imported successfully',
  EXPORT_SUCCESS: 'Proxies exported successfully',
  COPY_SUCCESS: 'Copied to clipboard',
} as const
