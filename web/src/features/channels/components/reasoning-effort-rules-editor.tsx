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
import { Plus, Trash2 } from 'lucide-react'
import { useId, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

export type ReasoningEffortRule = {
  model: string
  effort: string
  force?: boolean
}

export type ReasoningEffortRulesEditorProps = {
  enabled: boolean
  rules: ReasoningEffortRule[]
  onEnabledChange: (enabled: boolean) => void
  onRulesChange: (rules: ReasoningEffortRule[]) => void
  modelOptions: string[]
  disabled?: boolean
}

const EFFORT_PRESETS = [
  'none',
  'minimal',
  'low',
  'medium',
  'high',
  'xhigh',
  'max',
] as const

export function collectReasoningModelOptions(
  modelsCsv: string | undefined,
  modelMappingJson: string | undefined
): string[] {
  const set = new Set<string>()
  for (const m of (modelsCsv || '').split(',')) {
    const t = m.trim()
    if (t) set.add(t)
  }
  try {
    const map = modelMappingJson?.trim()
      ? (JSON.parse(modelMappingJson) as Record<string, unknown>)
      : {}
    for (const key of Object.keys(map || {})) {
      const t = key.trim()
      if (t) set.add(t)
    }
  } catch {
    // ignore invalid mapping while editing
  }
  return Array.from(set).sort((a, b) => a.localeCompare(b))
}

export function ReasoningEffortRulesEditor(
  props: ReasoningEffortRulesEditorProps
) {
  const { t } = useTranslation()
  const modelListId = useId()
  const effortOptions = useMemo(
    () =>
      EFFORT_PRESETS.map((value) => ({
        value,
        label: value,
      })),
    []
  )

  const rules = props.rules ?? []

  const updateRule = (
    index: number,
    patch: Partial<ReasoningEffortRule>
  ) => {
    const next = rules.map((rule, i) =>
      i === index ? { ...rule, ...patch } : rule
    )
    props.onRulesChange(next)
  }

  const removeRule = (index: number) => {
    props.onRulesChange(rules.filter((_, i) => i !== index))
  }

  const addRule = () => {
    props.onRulesChange([...rules, { model: '', effort: 'medium', force: false }])
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between gap-4 px-0 py-1'>
        <div className='space-y-0.5'>
          <Label htmlFor='reasoning-effort-rules-enabled'>
            {t('Enable custom reasoning effort')}
          </Label>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Applies only to Chat Completions, Responses, and Anthropic Messages. Inactive when request body pass-through is enabled.'
            )}
          </p>
        </div>
        <Switch
          id='reasoning-effort-rules-enabled'
          checked={props.enabled}
          onCheckedChange={props.onEnabledChange}
          disabled={props.disabled}
        />
      </div>

      {props.enabled && (
        <div className='space-y-2 rounded-md border p-3'>
          {rules.length > 0 ? (
            <div className='space-y-2'>
              <div className='text-muted-foreground grid grid-cols-[1fr_1fr_auto_auto] gap-2 text-xs font-medium'>
                <div>{t('Model')}</div>
                <div>{t('Effort')}</div>
                <div className='w-20 text-center'>{t('Force')}</div>
                <div className='w-10' />
              </div>
              {rules.map((rule, index) => (
                <div
                  key={index}
                  className='grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2'
                >
                  <Input
                    value={rule.model}
                    onChange={(e) =>
                      updateRule(index, { model: e.target.value })
                    }
                    placeholder={t('Model name')}
                    disabled={props.disabled}
                    list={modelListId}
                    aria-label={t('Model')}
                  />
                  <div
                    className={
                      props.disabled
                        ? 'pointer-events-none opacity-60'
                        : undefined
                    }
                  >
                    <Combobox
                      options={effortOptions}
                      value={rule.effort}
                      onValueChange={(value) =>
                        updateRule(index, { effort: value ?? '' })
                      }
                      placeholder={t('Effort level')}
                      searchPlaceholder={t('Select or type effort...')}
                      emptyText={t('No preset found.')}
                      allowCustomValue
                      className='w-full'
                    />
                  </div>
                  <div className='flex w-20 items-center justify-center'>
                    <Checkbox
                      checked={rule.force === true}
                      onCheckedChange={(checked) =>
                        updateRule(index, { force: checked === true })
                      }
                      disabled={props.disabled}
                      aria-label={t('Force override')}
                    />
                  </div>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    onClick={() => removeRule(index)}
                    disabled={props.disabled}
                    className='h-10 w-10'
                    aria-label={t('Delete rule')}
                  >
                    <Trash2 className='h-4 w-4' aria-hidden='true' />
                  </Button>
                </div>
              ))}
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Force override replaces the client reasoning effort when the model matches.'
                )}
              </p>
            </div>
          ) : (
            <div className='text-muted-foreground flex h-20 items-center justify-center rounded-md border border-dashed text-sm'>
              {t('No reasoning effort rules. Click "Add Rule" to get started.')}
            </div>
          )}
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={addRule}
            disabled={props.disabled}
            className='w-full'
          >
            <Plus className='mr-2 h-4 w-4' aria-hidden='true' />
            {t('Add Rule')}
          </Button>
        </div>
      )}

      {props.modelOptions.length > 0 && (
        <datalist id={modelListId}>
          {props.modelOptions.map((model) => (
            <option key={model} value={model} />
          ))}
        </datalist>
      )}
    </div>
  )
}
