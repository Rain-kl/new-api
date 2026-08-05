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
import type { BatchProxyItem, Proxy, ProxyProtocol } from '../types'

/**
 * Build a proxy URL string (without credentials) for display/copy.
 */
export function formatProxyAddress(
  protocol: string,
  host: string,
  port: number
): string {
  return `${protocol}://${host}:${port}`
}

/**
 * Build full proxy URL with optional credentials.
 */
export function buildProxyURL(proxy: {
  protocol: string
  host: string
  port: number
  username?: string
  password?: string
}): string {
  const { protocol, host, port, username = '', password = '' } = proxy
  if (username && password) {
    return `${protocol}://${encodeURIComponent(username)}:${encodeURIComponent(password)}@${host}:${port}`
  }
  return `${protocol}://${host}:${port}`
}

/**
 * Mask username for table display.
 */
export function maskAuth(username?: string, password?: string): string {
  const user = username?.trim() || ''
  const pass = password?.trim() || ''
  if (!user && !pass) return ''
  if (user && pass) return `${user}:***`
  if (user) return user
  return '***'
}

/**
 * Location label from geo fields.
 */
export function formatProxyLocation(proxy: Proxy): string {
  const parts = [proxy.city, proxy.region, proxy.country].filter(
    (part) => part && part.trim()
  )
  if (parts.length === 0 && proxy.ip_address) {
    return proxy.ip_address
  }
  if (proxy.ip_address && parts.length > 0) {
    return `${parts.join(', ')} (${proxy.ip_address})`
  }
  return parts.join(', ') || '-'
}

/**
 * Parse batch paste lines: protocol://user:pass@host:port or protocol://host:port
 */
export function parseBatchProxyLines(text: string): {
  items: BatchProxyItem[]
  errors: string[]
} {
  const items: BatchProxyItem[] = []
  const errors: string[] = []
  const lines = text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)

  lines.forEach((line, index) => {
    try {
      const parsed = new URL(line)
      const protocol = parsed.protocol.replace(':', '') as ProxyProtocol
      if (!['http', 'https', 'socks5', 'socks5h'].includes(protocol)) {
        errors.push(`Line ${index + 1}: unsupported protocol`)
        return
      }
      const host = parsed.hostname
      const port = parsed.port
        ? Number(parsed.port)
        : protocol === 'https'
          ? 443
          : protocol === 'http'
            ? 80
            : 0
      if (!host || !port || port < 1 || port > 65535) {
        errors.push(`Line ${index + 1}: invalid host or port`)
        return
      }
      items.push({
        protocol,
        host,
        port,
        username: parsed.username
          ? decodeURIComponent(parsed.username)
          : undefined,
        password: parsed.password
          ? decodeURIComponent(parsed.password)
          : undefined,
        name: `${protocol}://${host}:${port}`,
      })
    } catch {
      errors.push(`Line ${index + 1}: invalid URL`)
    }
  })

  return { items, errors }
}

export function isTimestampExpired(timestamp: number): boolean {
  if (timestamp === 0) return false
  return timestamp < Date.now() / 1000
}

export function isProxyExpiringSoon(
  expiresAt: number,
  warnDays: number
): boolean {
  if (expiresAt === 0) return false
  const now = Date.now() / 1000
  if (expiresAt <= now) return true
  const warnSeconds = Math.max(0, warnDays) * 24 * 60 * 60
  return expiresAt - now <= warnSeconds
}
