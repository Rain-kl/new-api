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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DateTimePicker } from '@/components/datetime-picker'
import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { addTimeToDate } from '@/lib/time'

import {
  batchCreateProxies,
  createProxy,
  getAllProxies,
  getProxy,
  updateProxy,
} from '../api'
import {
  ERROR_MESSAGES,
  SUCCESS_MESSAGES,
  getFallbackModeOptions,
  getProxyProtocolOptions,
  getProxyStatusOptions,
} from '../constants'
import {
  PROXY_FORM_DEFAULT_VALUES,
  getProxyFormSchema,
  parseBatchProxyLines,
  transformFormDataToPayload,
  transformProxyToFormDefaults,
  type ProxyFormValues,
} from '../lib'
import type { Proxy } from '../types'
import { useProxies } from './proxies-provider'

type ProxiesMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Proxy
}

export function ProxiesMutateDrawer({
  open,
  onOpenChange,
  currentRow,
}: ProxiesMutateDrawerProps) {
  const { t } = useTranslation()
  const isUpdate = !!currentRow
  const { triggerRefresh } = useProxies()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [createMode, setCreateMode] = useState<'standard' | 'batch'>('standard')
  const [batchText, setBatchText] = useState('')

  const form = useForm<ProxyFormValues>({
    resolver: zodResolver(getProxyFormSchema(t)),
    defaultValues: PROXY_FORM_DEFAULT_VALUES,
  })

  const fallbackMode = form.watch('fallback_mode')

  const { data: allProxiesData } = useQuery({
    queryKey: ['proxies', 'all'],
    queryFn: async () => {
      const result = await getAllProxies(false)
      return result.success ? result.data || [] : []
    },
    enabled: open,
  })

  const backupOptions = useMemo(() => {
    const list = allProxiesData || []
    return list
      .filter((p) => !currentRow || p.id !== currentRow.id)
      .map((p) => ({
        value: String(p.id),
        label: `${p.name} (${p.protocol}://${p.host}:${p.port})`,
      }))
  }, [allProxiesData, currentRow])

  useEffect(() => {
    if (open && isUpdate && currentRow) {
      getProxy(currentRow.id).then((result) => {
        if (result.success && result.data) {
          form.reset(transformProxyToFormDefaults(result.data))
        }
      })
    } else if (open && !isUpdate) {
      form.reset(PROXY_FORM_DEFAULT_VALUES)
      setCreateMode('standard')
      setBatchText('')
    }
  }, [open, isUpdate, currentRow, form])

  const onSubmit = async (data: ProxyFormValues) => {
    setIsSubmitting(true)
    try {
      const payload = transformFormDataToPayload(data)

      if (isUpdate && currentRow) {
        const result = await updateProxy(currentRow.id, payload)
        if (result.success) {
          toast.success(t(SUCCESS_MESSAGES.PROXY_UPDATED))
          onOpenChange(false)
          triggerRefresh()
        } else {
          toast.error(result.message || t(ERROR_MESSAGES.UPDATE_FAILED))
        }
      } else {
        const result = await createProxy(payload)
        if (result.success) {
          toast.success(t(SUCCESS_MESSAGES.PROXY_CREATED))
          onOpenChange(false)
          triggerRefresh()
        } else {
          toast.error(result.message || t(ERROR_MESSAGES.CREATE_FAILED))
        }
      }
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleBatchSubmit = async () => {
    const { items, errors } = parseBatchProxyLines(batchText)
    if (errors.length > 0) {
      toast.error(errors.slice(0, 3).join('; '))
      return
    }
    if (items.length === 0) {
      toast.error(t(ERROR_MESSAGES.BATCH_LINES_INVALID))
      return
    }
    setIsSubmitting(true)
    try {
      const result = await batchCreateProxies(items)
      if (result.success) {
        const created = result.data?.created ?? 0
        const skipped = result.data?.skipped ?? 0
        toast.success(
          t('Created {{created}} proxy(ies), skipped {{skipped}}', {
            created,
            skipped,
          })
        )
        onOpenChange(false)
        triggerRefresh()
      } else {
        toast.error(result.message || t(ERROR_MESSAGES.BATCH_CREATE_FAILED))
      }
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    if (!isUpdate && createMode === 'batch') {
      event.preventDefault()
      void handleBatchSubmit()
      return
    }
    void form.handleSubmit(onSubmit)(event)
  }

  const handleSetExpiryDays = (days: number) => {
    if (days === 0) {
      form.setValue('expires_at', undefined)
      return
    }
    const newDate = addTimeToDate(0, days, 0)
    form.setValue('expires_at', newDate)
  }

  const protocolOptions = getProxyProtocolOptions(t)
  const fallbackOptions = getFallbackModeOptions(t)
  const statusOptions = getProxyStatusOptions(t)

  return (
    <Sheet
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) {
          form.reset()
          setBatchText('')
        }
      }}
    >
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[600px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isUpdate ? t('Update Proxy') : t('Create Proxy')}
          </SheetTitle>
          <SheetDescription>
            {isUpdate
              ? t('Update the proxy by providing necessary info.')
              : t('Add a new proxy or batch paste proxy URLs.')}{' '}
            {t('Click save when you&apos;re done.')}
          </SheetDescription>
        </SheetHeader>

        <Form {...form}>
          <form
            id='proxy-form'
            onSubmit={handleSubmit}
            className={sideDrawerFormClassName()}
          >
            {!isUpdate && (
              <Tabs
                value={createMode}
                onValueChange={(v) =>
                  setCreateMode(v as 'standard' | 'batch')
                }
                className='mb-2'
              >
                <TabsList className='grid w-full grid-cols-2'>
                  <TabsTrigger value='standard'>{t('Standard')}</TabsTrigger>
                  <TabsTrigger value='batch'>{t('Batch Add Proxies')}</TabsTrigger>
                </TabsList>
                <TabsContent value='batch' className='mt-3'>
                  <SideDrawerSection>
                    <FormItem>
                      <FormLabel>{t('Proxy URLs')}</FormLabel>
                      <Textarea
                        value={batchText}
                        onChange={(e) => setBatchText(e.target.value)}
                        placeholder={
                          'http://user:pass@host:8080\nsocks5://host:1080'
                        }
                        rows={10}
                        className='font-mono text-xs'
                      />
                      <FormDescription>
                        {t(
                          'One proxy URL per line: protocol://user:pass@host:port'
                        )}
                      </FormDescription>
                    </FormItem>
                  </SideDrawerSection>
                </TabsContent>
              </Tabs>
            )}

            {(isUpdate || createMode === 'standard') && (
              <SideDrawerSection>
                <FormField
                  control={form.control}
                  name='name'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Name')}</FormLabel>
                      <FormControl>
                        <Input {...field} placeholder={t('Enter a name')} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='protocol'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Protocol')}</FormLabel>
                      <Select
                        items={protocolOptions}
                        value={field.value}
                        onValueChange={field.onChange}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder={t('Select protocol')} />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {protocolOptions.map((opt) => (
                              <SelectItem key={opt.value} value={opt.value}>
                                {opt.label}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
                  <FormField
                    control={form.control}
                    name='host'
                    render={({ field }) => (
                      <FormItem className='sm:col-span-2'>
                        <FormLabel>{t('Host')}</FormLabel>
                        <FormControl>
                          <Input
                            {...field}
                            placeholder='proxy.example.com'
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='port'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Port')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            max={65535}
                            value={field.value}
                            onChange={(e) =>
                              field.onChange(
                                parseInt(e.target.value, 10) || 0
                              )
                            }
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>

                <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                  <FormField
                    control={form.control}
                    name='username'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Username')}</FormLabel>
                        <FormControl>
                          <Input
                            {...field}
                            value={field.value || ''}
                            placeholder={t('Optional')}
                            autoComplete='off'
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='password'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Password')}</FormLabel>
                        <FormControl>
                          <Input
                            {...field}
                            type='password'
                            value={field.value || ''}
                            placeholder={t('Optional')}
                            autoComplete='new-password'
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>

                <FormField
                  control={form.control}
                  name='expires_at'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Expiration Time')}</FormLabel>
                      <div className='flex flex-col gap-2'>
                        <FormControl>
                          <DateTimePicker
                            value={field.value}
                            onChange={field.onChange}
                            placeholder={t('Never expires')}
                          />
                        </FormControl>
                        <div className='grid grid-cols-4 gap-1.5 sm:flex sm:gap-2'>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() => handleSetExpiryDays(0)}
                          >
                            {t('Never')}
                          </Button>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() => handleSetExpiryDays(7)}
                          >
                            {t('7 Days')}
                          </Button>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() => handleSetExpiryDays(30)}
                          >
                            {t('30 Days')}
                          </Button>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() => handleSetExpiryDays(90)}
                          >
                            {t('90 Days')}
                          </Button>
                        </div>
                      </div>
                      <FormDescription>
                        {t('Leave empty for never expires')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='expiry_warn_days'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Expiry Warning Days')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={365}
                          value={field.value}
                          onChange={(e) =>
                            field.onChange(parseInt(e.target.value, 10) || 0)
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Highlight proxies this many days before expiry (default 7)'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='fallback_mode'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Fallback Mode')}</FormLabel>
                      <Select
                        items={fallbackOptions}
                        value={field.value}
                        onValueChange={field.onChange}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue
                              placeholder={t('Select fallback mode')}
                            />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {fallbackOptions.map((opt) => (
                              <SelectItem key={opt.value} value={opt.value}>
                                {opt.label}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        {t(
                          'Action when this proxy expires: none, switch to backup, or direct'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {fallbackMode === 'proxy' && (
                  <FormField
                    control={form.control}
                    name='backup_proxy_id'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Backup Proxy')}</FormLabel>
                        <Select
                          items={backupOptions}
                          value={
                            field.value && field.value > 0
                              ? String(field.value)
                              : undefined
                          }
                          onValueChange={(v) =>
                            field.onChange(v ? Number(v) : 0)
                          }
                        >
                          <FormControl>
                            <SelectTrigger>
                              <SelectValue
                                placeholder={t('Select backup proxy')}
                              />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              {backupOptions.map((opt) => (
                                <SelectItem key={opt.value} value={opt.value}>
                                  {opt.label}
                                </SelectItem>
                              ))}
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}

                {isUpdate && (
                  <FormField
                    control={form.control}
                    name='status'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Status')}</FormLabel>
                        <Select
                          items={statusOptions}
                          value={field.value}
                          onValueChange={field.onChange}
                        >
                          <FormControl>
                            <SelectTrigger>
                              <SelectValue placeholder={t('Select status')} />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              {statusOptions.map((opt) => (
                                <SelectItem key={opt.value} value={opt.value}>
                                  {opt.label}
                                </SelectItem>
                              ))}
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}
              </SideDrawerSection>
            )}
          </form>
        </Form>

        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
          <Button form='proxy-form' type='submit' disabled={isSubmitting}>
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
