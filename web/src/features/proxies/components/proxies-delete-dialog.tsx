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

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

import { batchDeleteProxies, deleteProxy } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import { useProxies } from './proxies-provider'

export function ProxiesDeleteDialog() {
  const { t } = useTranslation()
  const {
    open,
    setOpen,
    currentRow,
    selectedIds,
    setSelectedIds,
    triggerRefresh,
  } = useProxies()
  const [isDeleting, setIsDeleting] = useState(false)

  const isBulk = open === 'bulk-delete'
  const isOpen = open === 'delete' || isBulk

  const handleDelete = async () => {
    setIsDeleting(true)
    try {
      if (isBulk) {
        if (selectedIds.length === 0) {
          toast.error(t('No proxies selected'))
          return
        }
        const result = await batchDeleteProxies(selectedIds)
        if (result.success) {
          const deleted = result.data?.deleted_ids?.length || 0
          const skipped = result.data?.skipped || []
          toast.success(
            t('Deleted {{count}} proxy(ies)', { count: deleted }) ||
              t(SUCCESS_MESSAGES.BATCH_DELETED)
          )
          if (skipped.length > 0) {
            const reasons = skipped
              .map((s) => `#${s.id}: ${s.reason}`)
              .join('; ')
            toast.warning(
              t('Skipped {{count}} proxy(ies): {{reasons}}', {
                count: skipped.length,
                reasons,
              })
            )
          }
          setSelectedIds([])
          setOpen(null)
          triggerRefresh()
        } else {
          toast.error(result.message || t(ERROR_MESSAGES.BATCH_DELETE_FAILED))
        }
        return
      }

      if (!currentRow) return
      const result = await deleteProxy(currentRow.id)
      if (result.success) {
        toast.success(t(SUCCESS_MESSAGES.PROXY_DELETED))
        setOpen(null)
        triggerRefresh()
      } else {
        toast.error(
          result.message ||
            t(ERROR_MESSAGES.DELETE_FAILED) ||
            t('Proxy is in use')
        )
      }
    } finally {
      setIsDeleting(false)
    }
  }

  return (
    <AlertDialog open={isOpen} onOpenChange={(v) => !v && setOpen(null)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {isBulk ? t('Delete Selected Proxies?') : t('Are you sure?')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {isBulk ? (
              <>
                {t('Are you sure you want to delete')} {selectedIds.length}{' '}
                {t('selected proxy(ies)? Proxies in use will be skipped.')}
              </>
            ) : (
              <>
                {t('This will permanently delete proxy')}{' '}
                <span className='font-semibold'>{currentRow?.name}</span>
                {t('. This action cannot be undone.')}
                {(currentRow?.channel_count || 0) > 0 && (
                  <>
                    <br />
                    <span className='text-destructive'>
                      {t(
                        'This proxy is bound to {{count}} channel(s) and cannot be deleted until unbound.',
                        { count: currentRow?.channel_count }
                      )}
                    </span>
                  </>
                )}
              </>
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isDeleting}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            onClick={handleDelete}
            disabled={isDeleting}
            variant='destructive'
          >
            {isDeleting ? t('Deleting...') : t('Delete')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
