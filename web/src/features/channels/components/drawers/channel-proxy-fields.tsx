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
import { Activity } from 'lucide-react'
import { useMemo, useState } from 'react'
import { type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { getAllProxies, testProxy } from '@/features/proxies/api'
import { buildProxyURL } from '@/features/proxies/lib'
import type { Proxy } from '@/features/proxies/types'

import type { ChannelFormValues } from '../../lib'

type ChannelProxyFieldsProps = {
  form: UseFormReturn<ChannelFormValues>
}

export function ChannelProxyFields({ form }: ChannelProxyFieldsProps) {
  const { t } = useTranslation()
  const [isTesting, setIsTesting] = useState(false)
  const proxyId = form.watch('proxy_id') || 0

  const { data: proxies = [] } = useQuery({
    queryKey: ['proxies', 'all', 'channel-form'],
    queryFn: async () => {
      const result = await getAllProxies(false)
      return result.success ? result.data || [] : []
    },
  })

  const proxyOptions = useMemo(() => {
    const managed = (proxies as Proxy[]).map((p) => ({
      value: String(p.id),
      label: `${p.name} (${p.protocol}://${p.host}:${p.port})`,
    }))
    return [{ value: '0', label: t('No proxy') }, ...managed]
  }, [proxies, t])

  const selectedProxy = useMemo(
    () => (proxies as Proxy[]).find((p) => p.id === proxyId),
    [proxies, proxyId]
  )

  const handleProxySelect = (value: string | null) => {
    const nextId = Number(value || '0') || 0
    form.setValue('proxy_id', nextId, { shouldDirty: true, shouldValidate: true })
    if (nextId > 0) {
      const found = (proxies as Proxy[]).find((p) => p.id === nextId)
      if (found) {
        form.setValue('proxy', buildProxyURL(found), {
          shouldDirty: true,
          shouldValidate: true,
        })
      }
    } else {
      // Switching to custom / no pool — clear resolved managed URL if it matches previous selection
      form.setValue('proxy', '', { shouldDirty: true, shouldValidate: true })
    }
  }

  const handleTest = async () => {
    if (proxyId <= 0) return
    setIsTesting(true)
    try {
      const result = await testProxy(proxyId)
      const data = result.data
      if (result.success && data?.success !== false) {
        const latency =
          data?.latency_ms != null ? `${data.latency_ms} ms` : undefined
        const location = [data?.city, data?.country].filter(Boolean).join(', ')
        toast.success(
          [t('Proxy connection test succeeded'), latency, location]
            .filter(Boolean)
            .join(' · ')
        )
      } else {
        toast.error(data?.message || result.message || t('Failed to test proxy'))
      }
    } catch {
      toast.error(t('Failed to test proxy'))
    } finally {
      setIsTesting(false)
    }
  }

  return (
    <div className='space-y-3'>
      <FormField
        control={form.control}
        name='proxy_id'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Managed Proxy')}</FormLabel>
            <FormControl>
              <Combobox
                options={proxyOptions}
                value={String(field.value || 0)}
                onValueChange={handleProxySelect}
                placeholder={t('Select managed proxy')}
                emptyText={t('No proxies found')}
              />
            </FormControl>
            <FormDescription>
              {t(
                'Pick a proxy from the pool, or choose No proxy to use a custom URL'
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {proxyId > 0 ? (
        <FormItem>
          <div className='flex items-center justify-between gap-2'>
            <FormLabel>{t('Resolved Proxy URL')}</FormLabel>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={handleTest}
              disabled={isTesting}
            >
              <Activity className='h-3.5 w-3.5' />
              {isTesting ? t('Testing...') : t('Test Connection')}
            </Button>
          </div>
          <Input
            readOnly
            value={
              selectedProxy
                ? buildProxyURL(selectedProxy)
                : form.getValues('proxy') || ''
            }
            className='bg-muted/40 font-mono text-xs'
          />
          <FormDescription>
            {t('Managed by IP Management; URL updates when the pool entry changes')}
          </FormDescription>
        </FormItem>
      ) : (
        <FormField
          control={form.control}
          name='proxy'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Custom Proxy URL')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('socks5://user:pass@host:port')}
                  {...field}
                  value={field.value || ''}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Network proxy for this channel (supports HTTP, HTTPS, SOCKS5, and SOCKS5H)'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
    </div>
  )
}
