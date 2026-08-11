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
import { useMemo, useState } from 'react'

/** Hosts with no real upstream favicon; skip rather than hit favicon.im. */
const FAVICON_SKIP_HOSTS = new Set(['localhost', '127.0.0.1', '0.0.0.0', '::1'])

function isIpLiteral(host: string): boolean {
  return /^(\d{1,3}\.){3}\d{1,3}$/.test(host)
}

/**
 * Extract the hostname from a channel base_url for favicon lookups.
 * Returns null when the URL can't be parsed or the host has no public favicon.
 */
function extractFaviconDomain(
  baseUrl: string | null | undefined
): string | null {
  if (!baseUrl) return null
  const raw = baseUrl.trim()
  if (!raw) return null
  try {
    const url = new URL(raw.includes('://') ? raw : `https://${raw}`)
    const host = url.hostname
    if (!host || FAVICON_SKIP_HOSTS.has(host) || isIpLiteral(host)) {
      return null
    }
    return host
  } catch {
    return null
  }
}

/**
 * Upstream-site favicon shown next to the channel name.
 * Uses https://favicon.im; hides itself on load failure or when the channel has
 * no parseable public base_url.
 */
export function ChannelFavicon({
  baseUrl,
  size = 16,
}: {
  baseUrl?: string | null
  size?: number
}) {
  const [failed, setFailed] = useState(false)
  const domain = useMemo(() => extractFaviconDomain(baseUrl), [baseUrl])

  if (!domain || failed) {
    return null
  }

  return (
    <img
      src={`https://a.favicon.im/${domain}`}
      alt={`${domain} favicon`}
      loading='lazy'
      onError={() => setFailed(true)}
      className='flex-shrink-0 rounded-sm'
      style={{ width: size, height: size }}
    />
  )
}
