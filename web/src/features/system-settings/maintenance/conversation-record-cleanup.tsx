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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DateTimePicker } from '@/components/datetime-picker'
import { Button } from '@/components/ui/button'
import { deleteConversationRecords } from '@/features/usage-logs/api'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

export function ConversationRecordCleanup() {
  const { t } = useTranslation()
  const [date, setDate] = useState<Date | undefined>(
    () => new Date(Date.now() - 30 * 86400000)
  )
  const [open, setOpen] = useState(false)
  const [isCleaning, setIsCleaning] = useState(false)

  const clean = async () => {
    if (!date || isCleaning) return
    setIsCleaning(true)
    try {
      const response = await deleteConversationRecords(
        Math.floor(date.getTime() / 1000)
      )
      if (!response.success) {
        throw createServerError(response, 'Failed to clean conversation records')
      }
      const count = response.data ?? 0
      toast.success(
        count > 0
          ? t('{{count}} conversation records removed.', { count })
          : t('No conversation records matched the selected time.')
      )
      setOpen(false)
    } catch (error) {
      handleServerError(error, t('Failed to clean conversation records'))
    } finally {
      setIsCleaning(false)
    }
  }

  return (
    <section className='space-y-3' aria-label={t('Clean conversation records')}>
      <h4 className='font-medium'>{t('Clean conversation records')}</h4>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Remove conversation records created before the selected timestamp.'
        )}
      </p>
      <DateTimePicker value={date} onChange={setDate} />
      <div className='flex flex-wrap gap-3'>
        {[
          { days: 1, label: '24 hours ago' },
          { days: 7, label: '7 days ago' },
          { days: 30, label: '30 days ago' },
        ].map(({ days, label }) => (
          <Button
            key={days}
            type='button'
            variant='outline'
            onClick={() => setDate(new Date(Date.now() - days * 86400000))}
          >
            {t(label)}
          </Button>
        ))}
        <Button
          type='button'
          variant='destructive'
          disabled={!date || isCleaning}
          onClick={() => setOpen(true)}
        >
          {isCleaning ? t('Cleaning...') : t('Clean conversation records')}
        </Button>
      </div>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={t('Confirm clear conversation records')}
        desc={t(
          'This will permanently delete all conversation records before {{date}}.',
          {
            date: date
              ? formatTimestampToDate(date.getTime(), 'milliseconds')
              : '',
          }
        )}
        confirmText={t('Confirm')}
        destructive
        isLoading={isCleaning}
        handleConfirm={clean}
      />
    </section>
  )
}
