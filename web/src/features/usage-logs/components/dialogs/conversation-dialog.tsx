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
import ReactJson from '@microlink/react-json-view'
import { Check, Copy, Loader2 } from 'lucide-react'
import { useTheme } from 'next-themes'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'

import { getConversationByLogId } from '../../api'

interface ConversationDialogProps {
  logId: number | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ConversationDialog({
  logId,
  open,
  onOpenChange,
}: ConversationDialogProps) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  const [loading, setLoading] = useState(false)
  const [content, setContent] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open || logId == null) {
      return
    }
    let cancelled = false
    setLoading(true)
    setContent(null)
    setError(null)
    getConversationByLogId(logId)
      .then((res) => {
        if (cancelled) return
        if (!res.success) {
          setError(res.message || t('No conversation record'))
          return
        }
        setContent(res.data?.content ?? '')
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setError(
          err instanceof Error ? err.message : t('Failed to load conversation')
        )
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [open, logId, t])

  const parsedJson = useMemo(() => {
    if (!content) return null
    try {
      const parsed = JSON.parse(content)
      if (typeof parsed === 'object' && parsed !== null) {
        return parsed
      }
      return null
    } catch {
      return null
    }
  }, [content])

  const formatted = useMemo(() => {
    if (!content) return ''
    if (parsedJson) {
      try {
        return JSON.stringify(parsedJson, null, 2)
      } catch {
        return content
      }
    }
    return content
  }, [content, parsedJson])

  const isDark = resolvedTheme === 'dark'
  const jsonTheme = isDark ? 'ocean' : 'rsuite'

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Conversation content')}
      description={t('Raw request body stored for this log entry')}
      contentClassName='sm:max-w-2xl'
      contentHeight='auto'
      bodyClassName='space-y-3'
    >
      {loading ? (
        <div className='text-muted-foreground flex items-center gap-2 py-8 text-sm'>
          <Loader2 className='size-4 animate-spin' />
          {t('Loading...')}
        </div>
      ) : error ? (
        <p className='text-muted-foreground py-4 text-sm'>{error}</p>
      ) : content ? (
        <div className='bg-muted/50 relative rounded-md border p-3'>
          <Button
            variant='ghost'
            size='sm'
            className='absolute top-2 right-2 z-10 h-8 w-8 p-0'
            onClick={() => copyToClipboard(formatted)}
            title={t('Copy to clipboard')}
          >
            {copiedText === formatted ? (
              <Check className='size-4 text-green-600' />
            ) : (
              <Copy className='size-4' />
            )}
          </Button>
          {parsedJson ? (
            <div className='max-h-[50vh] overflow-auto pr-10 text-xs'>
              <ReactJson
                src={parsedJson}
                collapsed={1}
                theme={jsonTheme}
                displayDataTypes={false}
                displayObjectSize={true}
                name={false}
                enableClipboard={false}
                style={{ backgroundColor: 'transparent' }}
              />
            </div>
          ) : (
            <pre className='max-h-[50vh] overflow-auto pr-10 text-xs leading-relaxed break-words whitespace-pre-wrap'>
              {content}
            </pre>
          )}
        </div>
      ) : (
        <p className='text-muted-foreground py-4 text-sm'>
          {t('No conversation record')}
        </p>
      )}
    </Dialog>
  )
}
