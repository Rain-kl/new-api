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

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import { exportProxies, importProxies } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import type { ProxyExportPayload } from '../types'
import { useProxies } from './proxies-provider'

function downloadJson(filename: string, data: unknown) {
  const blob = new Blob([JSON.stringify(data, null, 2)], {
    type: 'application/json',
  })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

export function ProxiesImportExportDialog() {
  const { t } = useTranslation()
  const { open, setOpen, triggerRefresh } = useProxies()
  const isImport = open === 'import'
  const isExport = open === 'export'
  const isOpen = isImport || isExport

  const [importText, setImportText] = useState('')
  const [isWorking, setIsWorking] = useState(false)

  const handleExport = async () => {
    setIsWorking(true)
    try {
      const result = await exportProxies()
      if (result.success && result.data) {
        const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')
        downloadJson(`proxies-export-${stamp}.json`, result.data)
        toast.success(t(SUCCESS_MESSAGES.EXPORT_SUCCESS))
        setOpen(null)
      } else {
        toast.error(result.message || t(ERROR_MESSAGES.EXPORT_FAILED))
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.EXPORT_FAILED))
    } finally {
      setIsWorking(false)
    }
  }

  const handleImport = async () => {
    setIsWorking(true)
    try {
      let payload: ProxyExportPayload | { proxies: unknown[] }
      try {
        payload = JSON.parse(importText) as ProxyExportPayload
      } catch {
        toast.error(t('Invalid JSON'))
        return
      }
      if (
        !payload ||
        typeof payload !== 'object' ||
        !('proxies' in payload) ||
        !Array.isArray(payload.proxies)
      ) {
        toast.error(t('Import payload must include a proxies array'))
        return
      }
      const result = await importProxies(payload)
      if (result.success) {
        const created = result.data?.created ?? 0
        const reused = result.data?.reused ?? 0
        const errors = result.data?.errors || []
        toast.success(
          t('Import finished: {{created}} created, {{reused}} reused', {
            created,
            reused,
          })
        )
        if (errors.length > 0) {
          toast.warning(
            t('{{count}} import warning(s)', { count: errors.length })
          )
        }
        setImportText('')
        setOpen(null)
        triggerRefresh()
      } else {
        toast.error(result.message || t(ERROR_MESSAGES.IMPORT_FAILED))
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.IMPORT_FAILED))
    } finally {
      setIsWorking(false)
    }
  }

  const handleFile = async (file: File | null) => {
    if (!file) return
    try {
      const text = await file.text()
      setImportText(text)
    } catch {
      toast.error(t('Failed to read file'))
    }
  }

  return (
    <Dialog
      open={isOpen}
      onOpenChange={(v) => {
        if (!v) {
          setOpen(null)
          setImportText('')
        }
      }}
      title={isImport ? t('Import Proxies') : t('Export Proxies')}
      description={
        isImport
          ? t('Paste or upload a proxies export JSON file')
          : t('Download all proxies as a JSON file')
      }
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            variant='outline'
            onClick={() => {
              setOpen(null)
              setImportText('')
            }}
            disabled={isWorking}
          >
            {t('Cancel')}
          </Button>
          {isImport ? (
            <Button onClick={handleImport} disabled={isWorking || !importText.trim()}>
              {isWorking ? t('Importing...') : t('Import')}
            </Button>
          ) : (
            <Button onClick={handleExport} disabled={isWorking}>
              {isWorking ? t('Exporting...') : t('Export')}
            </Button>
          )}
        </>
      }
    >
      {isImport && (
        <div className='space-y-3'>
          <div className='grid gap-2'>
            <Label htmlFor='proxy-import-file'>{t('JSON File')}</Label>
            <input
              id='proxy-import-file'
              type='file'
              accept='application/json,.json'
              className='text-sm'
              onChange={(e) => handleFile(e.target.files?.[0] || null)}
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='proxy-import-text'>{t('JSON Content')}</Label>
            <Textarea
              id='proxy-import-text'
              value={importText}
              onChange={(e) => setImportText(e.target.value)}
              rows={12}
              className='font-mono text-xs'
              placeholder='{"type":"new-api-proxies","version":1,"proxies":[...]}'
            />
          </div>
        </div>
      )}
      {isExport && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Export includes names, connection fields, expiry, and fallback settings. Credentials are included for restore.'
          )}
        </p>
      )}
    </Dialog>
  )
}
