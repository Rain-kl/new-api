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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef, Row } from '@tanstack/react-table'
import { Pencil, Plus, Power, PowerOff, Trash2 } from 'lucide-react'
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTablePage, useDataTable } from '@/components/data-table'
import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
  sideDrawerSwitchItemClassName,
} from '@/components/drawer-layout'
import { GroupBadge } from '@/components/group-badge'
import { MultiSelect } from '@/components/multi-select'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Button } from '@/components/ui/button'
import {
  DropdownMenuItem,
  DropdownMenuShortcut,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  RadioGroup,
  RadioGroupItem,
} from '@/components/ui/radio-group'
import {
  Select,
  SelectContent,
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
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getChannels } from '@/features/channels/api'
import {
  parseGroupsList,
  parseModelsList,
} from '@/features/channels/lib/channel-utils'
import { getGroups } from '@/features/users/api'

import {
  createModelRedirect,
  deleteModelRedirect,
  listModelRedirects,
  MODEL_REDIRECT_MODE_MAPPING,
  MODEL_REDIRECT_MODE_REDIRECT,
  MODEL_REDIRECT_SENTINEL_CHANNEL_ID,
  type ModelRedirect,
  type ModelRedirectInput,
  type ModelRedirectMode,
  type ModelRedirectTarget,
  updateModelRedirect,
  updateModelRedirectStatus,
} from '../api-model-redirect'

type ChannelOption = {
  id: number
  name: string
  models: string
}

/** One editor row: one channel + one optional upstream model. */
type TargetDraft = {
  key: string
  channel_id: number
  /** Upstream model on this channel; empty = passthrough virtual name. */
  model: string
  /** Higher number = higher priority. Same priority load-balances across channels. */
  priority: number
  enabled: boolean
}

const PASSTHROUGH_MODEL_VALUE = '__passthrough__'

function newDraftKey(): string {
  return Math.random().toString(36).slice(2)
}

function emptyTarget(priority = 100): TargetDraft {
  return {
    key: newDraftKey(),
    channel_id: 0,
    model: '',
    priority,
    enabled: true,
  }
}

function nextDefaultPriority(targets: TargetDraft[]): number {
  if (targets.length === 0) return 100
  const max = Math.max(...targets.map((t) => t.priority || 0))
  return max + 10
}

const MAX_PRIORITY = 1_000_000
const MAX_TARGETS = 64

function clampPriority(raw: number): number {
  if (!Number.isFinite(raw)) return 0
  const n = Math.trunc(raw)
  if (n < 1) return 0
  if (n > MAX_PRIORITY) return MAX_PRIORITY
  return n
}

/** Map API targets to editor rows (1:1). */
function targetsToDrafts(targets: ModelRedirectTarget[]): TargetDraft[] {
  const sorted = [...targets].sort((a, b) => {
    if (b.priority !== a.priority) return b.priority - a.priority
    if (a.channel_id !== b.channel_id) return a.channel_id - b.channel_id
    return (a.id ?? 0) - (b.id ?? 0)
  })
  if (sorted.length === 0) return [emptyTarget()]
  return sorted.map((t) => ({
    key: String(t.id ?? newDraftKey()),
    channel_id: t.channel_id,
    model: (t.model || '').trim(),
    priority: t.priority > 0 ? clampPriority(t.priority) || 100 : 100,
    enabled: t.enabled,
  }))
}

/** Map editor rows to API targets (1:1). */
function draftsToInputTargets(
  drafts: TargetDraft[]
): ModelRedirectInput['targets'] {
  const out: ModelRedirectInput['targets'] = []
  const seen = new Set<string>()
  for (const draft of drafts) {
    const priority = clampPriority(draft.priority)
    if (priority <= 0) continue
    const isNested = draft.channel_id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID
    if (!isNested && draft.channel_id <= 0) continue
    if (isNested && !draft.model.trim()) continue
    const model = draft.model.trim().slice(0, 128)
    const key = `${priority}|${draft.channel_id}|${model}|${draft.enabled}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push({
      priority,
      // weight reserved for future weighted LB; equal share for now
      weight: 0,
      channel_id: draft.channel_id,
      model,
      enabled: draft.enabled,
    })
  }
  return out
}

function channelDisplayName(
  channels: ChannelOption[],
  channelId: number
): string {
  if (
    channelId !== MODEL_REDIRECT_SENTINEL_CHANNEL_ID &&
    channelId <= 0
  ) {
    return ''
  }
  const ch = channels.find((c) => c.id === channelId)
  if (ch?.name?.trim()) return ch.name.trim()
  if (channelId === MODEL_REDIRECT_SENTINEL_CHANNEL_ID) return 'Custom redirect'
  return `#${channelId}`
}

function isSelectedChannelId(channelId: number): boolean {
  return (
    channelId === MODEL_REDIRECT_SENTINEL_CHANNEL_ID || channelId > 0
  )
}

function sortChannelsByName(channels: ChannelOption[]): ChannelOption[] {
  return [...channels].sort((a, b) => {
    const byName = a.name.localeCompare(b.name, undefined, {
      sensitivity: 'base',
    })
    if (byName !== 0) return byName
    return a.id - b.id
  })
}

type ModelRedirectUIContextValue = {
  openCreate: () => void
  openEdit: (row: ModelRedirect) => void
}

const ModelRedirectUIContext =
  createContext<ModelRedirectUIContextValue | null>(null)

function useModelRedirectUI() {
  const ctx = useContext(ModelRedirectUIContext)
  if (!ctx) {
    throw new Error('useModelRedirectUI must be used within ModelRedirectProvider')
  }
  return ctx
}

/** Wraps table + drawer; page Actions buttons share state via context. */
export function ModelRedirectProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<ModelRedirect | null>(null)

  const invalidate = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ['model-redirects'] })
  }, [queryClient])

  const openCreate = useCallback(() => {
    setEditing(null)
    setDrawerOpen(true)
  }, [])

  const openEdit = useCallback((row: ModelRedirect) => {
    setEditing(row)
    setDrawerOpen(true)
  }, [])

  const value = useMemo(
    () => ({ openCreate, openEdit }),
    [openCreate, openEdit]
  )

  return (
    <ModelRedirectUIContext.Provider value={value}>
      {children}
      <ModelRedirectDrawer
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        editing={editing}
        onSaved={() => {
          setDrawerOpen(false)
          invalidate()
        }}
      />
    </ModelRedirectUIContext.Provider>
  )
}

/** 页头主按钮，对齐 ModelsPrimaryButtons */
export function ModelRedirectPrimaryButtons() {
  const { openCreate } = useModelRedirectUI()
  return (
    <div className='flex items-center gap-2'>
      <Button onClick={openCreate} size='sm'>
        <Plus className='h-4 w-4' />
        添加模型重定向
      </Button>
    </div>
  )
}

export function ModelRedirectSection() {
  const queryClient = useQueryClient()
  const { openCreate, openEdit } = useModelRedirectUI()
  const [globalFilter, setGlobalFilter] = useState('')
  const [pagination, setPagination] = useState({
    pageIndex: 0,
    pageSize: 20,
  })

  const {
    data: rows = [],
    isLoading,
    isFetching,
  } = useQuery({
    queryKey: ['model-redirects'],
    queryFn: listModelRedirects,
  })

  const { data: channels = [] } = useQuery({
    queryKey: ['channels-for-redirect'],
    queryFn: async () => {
      const res = await getChannels({ p: 0, page_size: 500 })
      const items = res.data?.items
      if (!Array.isArray(items)) return [] as ChannelOption[]
      return items.map((ch) => ({
        id: ch.id,
        name: ch.name,
        models: ch.models || '',
      }))
    },
  })

  const channelNameById = useMemo(() => {
    const map = new Map<number, string>()
    for (const ch of channels) {
      map.set(ch.id, ch.name)
    }
    return map
  }, [channels])

  const invalidate = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ['model-redirects'] })
  }, [queryClient])

  const filtered = useMemo(() => {
    const q = globalFilter.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((row) => {
      const hay = `${row.name} ${row.groups} ${row.remark} ${row.mode ?? ''} ${row.mapping_target ?? ''}`.toLowerCase()
      return hay.includes(q)
    })
  }, [rows, globalFilter])

  const pageRows = useMemo(() => {
    const start = pagination.pageIndex * pagination.pageSize
    return filtered.slice(start, start + pagination.pageSize)
  }, [filtered, pagination])

  const onChanged = useCallback(() => {
    invalidate()
  }, [invalidate])

  const columns = useModelRedirectColumns({
    onEdit: openEdit,
    onChanged,
    channelNameById,
  })

  const { table } = useDataTable({
    data: pageRows,
    columns,
    totalCount: filtered.length,
    pagination,
    globalFilter,
    // DataTableToolbar reads columnFilters.length — must not be undefined.
    columnFilters: [],
    onPaginationChange: setPagination,
    onGlobalFilterChange: setGlobalFilter,
    manualPagination: true,
    manualFiltering: true,
    enableRowSelection: false,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle='暂无模型重定向/映射'
      emptyDescription='创建虚拟模型：配置为模型映射（路由到目标模型名，由渠道层自动选渠道）或按优先级重定向到指定渠道。'
      emptyAction={
        <Button size='sm' onClick={openCreate}>
          <Plus className='h-4 w-4' />
          添加模型重定向
        </Button>
      }
      skeletonKeyPrefix='model-redirect-skeleton'
      applyHeaderSize
      toolbarProps={{
        searchPlaceholder: '按虚拟模型名称筛选...',
        filters: [],
        hideViewOptions: true,
      }}
    />
  )
}

function useModelRedirectColumns(opts: {
  onEdit: (row: ModelRedirect) => void
  onChanged: () => void
  channelNameById: Map<number, string>
}): ColumnDef<ModelRedirect>[] {
  const { t } = useTranslation()
  return useMemo(
    () => [
      {
        accessorKey: 'id',
        header: 'ID',
        meta: { mobileHidden: true },
        size: 72,
        cell: ({ row }) => <TableId value={row.original.id} />,
      },
      {
        accessorKey: 'name',
        header: '虚拟模型',
        cell: ({ row }) => (
          <span className='font-mono text-sm font-medium'>
            {row.original.name}
          </span>
        ),
      },
      {
        id: 'mode',
        header: '模式',
        size: 90,
        cell: ({ row }) => {
          const mode = row.original.mode || MODEL_REDIRECT_MODE_REDIRECT
          return mode === MODEL_REDIRECT_MODE_MAPPING ? (
            <StatusBadge
              label='映射'
              variant='info'
              size='sm'
              copyable={false}
            />
          ) : (
            <StatusBadge
              label='重定向'
              variant='neutral'
              size='sm'
              copyable={false}
            />
          )
        },
      },
      {
        accessorKey: 'groups',
        header: '分组',
        cell: ({ row }) => {
          const groups = parseGroupsList(row.original.groups)
          if (groups.length === 0) {
            return <span className='text-muted-foreground text-sm'>-</span>
          }
          return (
            <div className='flex flex-wrap gap-1'>
              {groups.map((g) => (
                <GroupBadge key={g} group={g} />
              ))}
            </div>
          )
        },
      },
      {
        id: 'targets',
        header: '目标',
        cell: ({ row }) => {
          const original = row.original
          if (
            (original.mode || MODEL_REDIRECT_MODE_REDIRECT) ===
            MODEL_REDIRECT_MODE_MAPPING
          ) {
            if (!original.mapping_target) {
              return <span className='text-muted-foreground text-sm'>-</span>
            }
            return (
              <span className='text-muted-foreground truncate text-xs'>
                <span className='font-mono'>→</span>{' '}
                <span className='text-foreground/80 font-mono font-medium'>
                  {original.mapping_target}
                </span>
              </span>
            )
          }
          const targets = [...(row.original.targets || [])].sort((a, b) => {
            if (b.priority !== a.priority) return b.priority - a.priority
            return a.channel_id - b.channel_id
          })
          if (targets.length === 0) {
            return <span className='text-muted-foreground text-sm'>0</span>
          }
          return (
            <div className='flex max-w-md flex-col gap-0.5'>
              {targets.slice(0, 3).map((target) => {
                const isNested =
                  target.channel_id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID
                const chName = isNested
                  ? t('Custom redirect')
                  : opts.channelNameById.get(target.channel_id) ||
                    `#${target.channel_id}`
                return (
                  <span
                    key={`${target.id ?? 0}-${target.priority}-${target.channel_id}-${target.model}`}
                    className='text-muted-foreground truncate text-xs'
                  >
                    <span className='font-mono'>P{target.priority}</span>
                    {': '}
                    <span className='font-medium text-foreground/80'>
                      {chName}
                    </span>
                    {isNested ? (
                      <span className='font-mono'>
                        {' '}
                        → {target.model || '?'}
                      </span>
                    ) : target.model ? (
                      <span className='font-mono'> → {target.model}</span>
                    ) : (
                      ' → 透传'
                    )}
                  </span>
                )
              })}
              {targets.length > 3 && (
                <span className='text-muted-foreground text-xs'>
                  +{targets.length - 3} 项
                </span>
              )}
            </div>
          )
        },
      },
      {
        accessorKey: 'enabled',
        header: '状态',
        size: 100,
        cell: ({ row }) =>
          row.original.enabled ? (
            <StatusBadge
              label='已启用'
              variant='success'
              size='sm'
              copyable={false}
            />
          ) : (
            <StatusBadge
              label='已禁用'
              variant='neutral'
              size='sm'
              copyable={false}
            />
          ),
      },
      {
        id: 'actions',
        header: '',
        size: 96,
        enableSorting: false,
        enableHiding: false,
        cell: ({ row }) => (
          <ModelRedirectRowActions
            row={row}
            onEdit={() => opts.onEdit(row.original)}
            onChanged={opts.onChanged}
          />
        ),
      },
    ],
    [opts.channelNameById, opts.onEdit, opts.onChanged, t]
  )
}

function ModelRedirectRowActions(props: {
  row: Row<ModelRedirect>
  onEdit: () => void
  onChanged: () => void
}) {
  const item = props.row.original
  const [deleteOpen, setDeleteOpen] = useState(false)

  const statusMutation = useMutation({
    mutationFn: () => updateModelRedirectStatus(item.id, !item.enabled),
    onSuccess: () => {
      toast.success('已保存')
      props.onChanged()
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const deleteMutation = useMutation({
    mutationFn: () => deleteModelRedirect(item.id),
    onSuccess: () => {
      toast.success('已删除')
      setDeleteOpen(false)
      props.onChanged()
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const toggleLabel = item.enabled ? '禁用' : '启用'

  return (
    <div className='-ml-1.5 flex items-center gap-1'>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              onClick={props.onEdit}
              aria-label='编辑'
            />
          }
        >
          <Pencil />
        </TooltipTrigger>
        <TooltipContent>编辑</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon-sm'
              onClick={() => statusMutation.mutate()}
              aria-label={toggleLabel}
              className={
                item.enabled
                  ? 'text-destructive hover:text-destructive'
                  : 'text-success hover:text-success'
              }
            />
          }
        >
          {item.enabled ? <PowerOff /> : <Power />}
        </TooltipTrigger>
        <TooltipContent>{toggleLabel}</TooltipContent>
      </Tooltip>

      <DataTableRowActionMenu>
        <DropdownMenuItem onClick={props.onEdit}>
          编辑
          <DropdownMenuShortcut>
            <Pencil className='h-4 w-4' />
          </DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => statusMutation.mutate()}>
          {toggleLabel}
          <DropdownMenuShortcut>
            {item.enabled ? (
              <PowerOff className='h-4 w-4' />
            ) : (
              <Power className='h-4 w-4' />
            )}
          </DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem
          variant='destructive'
          onClick={() => setDeleteOpen(true)}
        >
          删除
          <DropdownMenuShortcut>
            <Trash2 className='h-4 w-4' />
          </DropdownMenuShortcut>
        </DropdownMenuItem>
      </DataTableRowActionMenu>

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title='删除模型重定向'
        desc={`将永久删除虚拟模型「${item.name}」及其全部优先级目标。`}
        confirmText='删除'
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() => deleteMutation.mutate()}
      />
    </div>
  )
}

function ModelRedirectDrawer(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing: ModelRedirect | null
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const isEdit = !!props.editing

  const [name, setName] = useState('')
  const [groups, setGroups] = useState<string[]>([])
  const [remark, setRemark] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [mode, setMode] = useState<ModelRedirectMode>('redirect')
  const [mappingTarget, setMappingTarget] = useState('')
  const [targets, setTargets] = useState<TargetDraft[]>([emptyTarget()])

  useEffect(() => {
    if (!props.open) return
    if (props.editing) {
      setName(props.editing.name)
      setGroups(parseGroupsList(props.editing.groups))
      setRemark(props.editing.remark || '')
      setEnabled(props.editing.enabled)
      setMode(props.editing.mode || MODEL_REDIRECT_MODE_REDIRECT)
      setMappingTarget(props.editing.mapping_target || '')
      setTargets(targetsToDrafts(props.editing.targets || []))
    } else {
      setName('')
      setGroups([])
      setRemark('')
      setEnabled(true)
      setMode(MODEL_REDIRECT_MODE_REDIRECT)
      setMappingTarget('')
      setTargets([emptyTarget(100)])
    }
  }, [props.open, props.editing])

  // Must match other pages: queryFn returns ApiResponse, not unwrapped array.
  // Shared key ['groups'] may already be cached as ApiResponse from channels/users.
  const { data: groupsData } = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
    enabled: props.open,
  })

  const { data: channelsRaw = [] } = useQuery({
    queryKey: ['channels-for-redirect'],
    queryFn: async () => {
      const res = await getChannels({ p: 0, page_size: 500 })
      const items = res.data?.items
      if (!Array.isArray(items)) return [] as ChannelOption[]
      return items.map((ch) => ({
        id: ch.id,
        name: ch.name,
        models: ch.models || '',
      }))
    },
    enabled: props.open,
  })

  // Reuse table cache for nested virtual-name options.
  const { data: allRedirects = [] } = useQuery({
    queryKey: ['model-redirects'],
    queryFn: listModelRedirects,
    enabled: props.open,
  })

  const channelOptions = useMemo(() => {
    const custom: ChannelOption = {
      id: MODEL_REDIRECT_SENTINEL_CHANNEL_ID,
      name: t('Custom redirect'),
      models: '',
    }
    return [custom, ...sortChannelsByName(channelsRaw)]
  }, [channelsRaw, t])

  const nestedModelOptions = useMemo(() => {
    return (allRedirects || [])
      .filter((r) => r.enabled && r.name !== name.trim())
      .map((r) => r.name)
      .sort((a, b) => a.localeCompare(b))
  }, [allRedirects, name])

  const groupList = useMemo(() => {
    const raw = groupsData?.data
    return Array.isArray(raw) ? raw : []
  }, [groupsData])

  const groupOptions = useMemo(
    () => groupList.map((g) => ({ value: String(g), label: String(g) })),
    [groupList]
  )

  const channelModelsById = useMemo(() => {
    const map = new Map<number, string[]>()
    for (const ch of channelOptions) {
      if (ch.id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID) continue
      map.set(ch.id, parseModelsList(ch.models))
    }
    return map
  }, [channelOptions])

  const updateTarget = useCallback(
    (index: number, patch: Partial<TargetDraft>) => {
      setTargets((prev) =>
        prev.map((item, i) => {
          if (i !== index) return item
          const next = { ...item, ...patch }
          if (patch.priority !== undefined) {
            next.priority = clampPriority(patch.priority)
          }
          return next
        })
      )
    },
    []
  )

  const saveMutation = useMutation({
    mutationFn: async () => {
      const trimmedName = name.trim()
      if (!trimmedName) {
        throw new Error('请填写虚拟模型名称')
      }
      if (/[,\n\r\t]/.test(trimmedName)) {
        throw new Error('虚拟模型名称不能包含逗号或空白控制字符')
      }
      if (groups.length === 0) {
        throw new Error('请至少选择一个分组')
      }
      const trimmedMappingTarget = mappingTarget.trim()
      if (mode === MODEL_REDIRECT_MODE_MAPPING) {
        if (!trimmedMappingTarget) {
          throw new Error('请填写映射目标模型名')
        }
        if (/[,\n\r\t]/.test(trimmedMappingTarget)) {
          throw new Error('映射目标不能包含逗号或空白控制字符')
        }
      } else {
        if (targets.length > MAX_TARGETS) {
          throw new Error(`目标数量过多（最多 ${MAX_TARGETS}）`)
        }
        if (
          targets.some((target) => {
            if (target.channel_id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID) {
              return !target.model.trim()
            }
            return !target.channel_id || target.channel_id <= 0
          })
        ) {
          const missingNested = targets.some(
            (target) =>
              target.channel_id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID &&
              !target.model.trim()
          )
          throw new Error(
            missingNested
              ? t('Select a redirect model')
              : '每个目标都必须选择渠道'
          )
        }
        if (
          targets.some((target) => clampPriority(target.priority) <= 0)
        ) {
          throw new Error('优先级必须为正整数（数值越大越优先）')
        }
        if (!targets.some((target) => target.enabled)) {
          throw new Error('请至少启用一个目标')
        }
      }
      const targetInputs =
        mode === MODEL_REDIRECT_MODE_MAPPING
          ? []
          : draftsToInputTargets(targets)
      const input: ModelRedirectInput = {
        name: trimmedName,
        groups,
        enabled,
        remark: remark.slice(0, 255),
        mode,
        mapping_target: trimmedMappingTarget,
        targets: targetInputs,
      }
      if (isEdit && props.editing) {
        return updateModelRedirect(props.editing.id, input)
      }
      return createModelRedirect(input)
    },
    onSuccess: () => {
      toast.success('已保存')
      props.onSaved()
    },
    onError: (e: Error) => toast.error(e.message),
  })

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-2xl')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isEdit ? '编辑模型重定向' : '创建模型重定向'}
          </SheetTitle>
          <SheetDescription>
            {isEdit
              ? '修改虚拟模型配置（映射或优先级重定向）。'
              : '创建虚拟模型：可配置为模型映射（路由到目标模型名）或优先级重定向（路由到指定渠道）。'}
          </SheetDescription>
        </SheetHeader>

        <div className={sideDrawerFormClassName()}>


          <SideDrawerSection>
            <h3 className='text-sm font-semibold'>基本信息</h3>

            <div className='space-y-2'>
              <Label>虚拟模型 *</Label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder='输入模型'
                className='font-mono'
              />
              <p className='text-muted-foreground text-xs'>
                客户端请求此名称，流量按下方优先级目标路由。
              </p>
            </div>

            <div className='space-y-2'>
              <Label>分组 *</Label>
              <MultiSelect
                options={groupOptions}
                selected={groups}
                onChange={setGroups}
                placeholder='选择分组'
              />
            </div>

            <div className={sideDrawerSwitchItemClassName()}>
              <div className='space-y-0.5'>
                <Label>启用</Label>
                <p className='text-muted-foreground text-xs'>
                  禁用后该规则不会参与路由。
                </p>
              </div>
              <Switch checked={enabled} onCheckedChange={setEnabled} />
            </div>

            <div className='space-y-2'>
              <Label>备注</Label>
              <Input
                value={remark}
                onChange={(e) => setRemark(e.target.value)}
                placeholder='可选说明'
              />
            </div>
          </SideDrawerSection>
          <SideDrawerSection>
            <div className='space-y-2'>
              <Label>模式</Label>
              <RadioGroup
                  value={mode}
                  onValueChange={(v) => setMode(v as ModelRedirectMode)}
                  className='grid grid-cols-2 gap-4'
              >
                <div className='flex items-center space-x-2'>
                  <RadioGroupItem
                      value={MODEL_REDIRECT_MODE_MAPPING}
                      id='mode-mapping'
                  />
                  <Label htmlFor='mode-mapping'>模型映射</Label>
                </div>
                <div className='flex items-center space-x-2'>
                  <RadioGroupItem
                      value={MODEL_REDIRECT_MODE_REDIRECT}
                      id='mode-redirect'
                  />
                  <Label htmlFor='mode-redirect'>模型重定向</Label>
                </div>
              </RadioGroup>
              <p className='text-muted-foreground text-xs'>
                {mode === MODEL_REDIRECT_MODE_MAPPING
                    ? '将虚拟模型名映射为目标模型名，由渠道层自动路由。'
                    : '按优先级将虚拟模型分发到指定渠道。'}
              </p>
            </div>
          </SideDrawerSection>

          {mode === MODEL_REDIRECT_MODE_MAPPING && (
              <SideDrawerSection>
                <div className='space-y-2'>
                  <Label>映射到（目标模型名）*</Label>
                  <Input
                      value={mappingTarget}
                      onChange={(e) => setMappingTarget(e.target.value)}
                      placeholder='输入映射名'
                      className='font-mono'
                  />
                  <p className='text-muted-foreground text-xs'>
                    客户端请求此虚拟模型名时，流量按该目标模型名自动路由到提供此模型的渠道；也可填写另一个虚拟模型名以继续解析。
                  </p>
                </div>
              </SideDrawerSection>
          )}
          {mode === MODEL_REDIRECT_MODE_REDIRECT && (
            <SideDrawerSection>
              <div className='flex items-center justify-between gap-2'>
                <div>
                  <h3 className='text-sm font-semibold'>优先级目标</h3>
              </div>
              <Button
                type='button'
                size='sm'
                variant='outline'
                disabled={targets.length >= MAX_TARGETS}
                onClick={() =>
                  setTargets((prev) => {
                    if (prev.length >= MAX_TARGETS) return prev
                    return [...prev, emptyTarget(nextDefaultPriority(prev))]
                  })
                }
              >
                <Plus className='h-4 w-4' />
                添加目标
              </Button>
            </div>

            <div className='space-y-3'>
              {targets.map((target, index) => (
                <RedirectTargetCard
                  key={target.key}
                  index={index}
                  target={target}
                  canRemove={targets.length > 1}
                  channels={channelOptions}
                  channelModels={
                    target.channel_id > 0
                      ? (channelModelsById.get(target.channel_id) ?? [])
                      : []
                  }
                  nestedModelOptions={nestedModelOptions}
                  onChange={(patch) => updateTarget(index, patch)}
                  onChannelChange={(channelId) => {
                    if (channelId === MODEL_REDIRECT_SENTINEL_CHANNEL_ID) {
                      updateTarget(index, {
                        channel_id: channelId,
                        model: '',
                      })
                      return
                    }
                    const wasNested =
                      target.channel_id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID
                    const known = channelModelsById.get(channelId) ?? []
                    // Keep current model only if still on the new real channel.
                    const keepModel =
                      !wasNested &&
                      target.model &&
                      known.includes(target.model)
                        ? target.model
                        : ''
                    updateTarget(index, {
                      channel_id: channelId,
                      model: keepModel,
                    })
                  }}
                  onRemove={() =>
                    setTargets((prev) =>
                      prev.length <= 1
                        ? prev
                        : prev.filter((_, i) => i !== index)
                    )
                  }
                />
              ))}
            </div>
          </SideDrawerSection>
          )}
        </div>

        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' type='button' />}>
            取消
          </SheetClose>
          <Button
            type='button'
            disabled={saveMutation.isPending}
            onClick={() => saveMutation.mutate()}
          >
            保存
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

function RedirectTargetCard(props: {
  index: number
  target: TargetDraft
  canRemove: boolean
  channels: ChannelOption[]
  channelModels: string[]
  nestedModelOptions: string[]
  onChange: (patch: Partial<TargetDraft>) => void
  onChannelChange: (channelId: number) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const { target, index } = props
  const isNested =
    target.channel_id === MODEL_REDIRECT_SENTINEL_CHANNEL_ID
  const channelSelected = isSelectedChannelId(target.channel_id)

  const modelSelectValue = isNested
    ? target.model || undefined
    : target.model
      ? target.model
      : PASSTHROUGH_MODEL_VALUE

  const modelItems = useMemo(() => {
    const list = isNested
      ? [...props.nestedModelOptions]
      : [...props.channelModels]
    if (target.model && !list.includes(target.model)) {
      list.push(target.model)
    }
    return list.sort((a, b) => a.localeCompare(b))
  }, [isNested, props.channelModels, props.nestedModelOptions, target.model])

  const modelSelectDisabled = isNested
    ? modelItems.length === 0
    : !channelSelected

  return (
    <div className='border-border/60 space-y-3 rounded-lg border p-3'>
      <div className='flex items-center justify-between gap-2'>
        <span className='text-sm font-medium'>目标 {index + 1}</span>
        <Button
          type='button'
          size='icon-sm'
          variant='ghost'
          onClick={props.onRemove}
          disabled={!props.canRemove}
          aria-label='移除'
        >
          <Trash2 className='h-4 w-4' />
        </Button>
      </div>

      <div className='grid gap-3 sm:grid-cols-2'>
        <div className='space-y-2'>
          <Label>优先级 *</Label>
          <Input
            type='number'
            min={1}
            max={MAX_PRIORITY}
            step={1}
            value={target.priority || ''}
            onChange={(e) => {
              const raw = e.target.value
              if (raw === '') {
                props.onChange({ priority: 0 })
                return
              }
              const n = Number.parseInt(raw, 10)
              props.onChange({
                priority: Number.isFinite(n) ? n : 0,
              })
            }}
          />
        </div>

        <div className='space-y-2'>
          <Label>渠道 *</Label>
          <Select
            value={
              channelSelected ? String(target.channel_id) : undefined
            }
            onValueChange={(v) => props.onChannelChange(Number(v))}
          >
            <SelectTrigger>
              <SelectValue placeholder='选择渠道'>
                {channelSelected
                  ? channelDisplayName(props.channels, target.channel_id)
                  : undefined}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {props.channels.map((ch) => (
                <SelectItem key={ch.id} value={String(ch.id)}>
                  {ch.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className='space-y-2'>
        {isNested ? (
          <Label>{t('Redirect model')} *</Label>
        ) : (
          <Label>
            模型{' '}
            <span className='text-muted-foreground font-normal'>
              （可选，不选则透传）
            </span>
          </Label>
        )}
        <Select
          value={modelSelectValue}
          onValueChange={(v) =>
            props.onChange({
              model:
                !isNested && v === PASSTHROUGH_MODEL_VALUE ? '' : v,
            })
          }
          disabled={modelSelectDisabled}
        >
          <SelectTrigger>
            <SelectValue
              placeholder={
                isNested
                  ? t('Select a redirect model')
                  : channelSelected
                    ? '选择该渠道上的模型'
                    : '请先选择渠道'
              }
            >
              {isNested
                ? target.model || undefined
                : target.model
                  ? target.model
                  : channelSelected
                    ? '透传虚拟模型名'
                    : undefined}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {!isNested && (
              <SelectItem value={PASSTHROUGH_MODEL_VALUE}>
                透传虚拟模型名
              </SelectItem>
            )}
            {modelItems.map((m) => (
              <SelectItem key={m} value={m}>
                {m}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className={sideDrawerSwitchItemClassName()}>
        <div className='space-y-0.5'>
          <Label>启用此目标</Label>
          <p className='text-muted-foreground text-xs'>
            禁用后不会参与路由与负载均衡。
          </p>
        </div>
        <Switch
          checked={target.enabled}
          onCheckedChange={(v) => props.onChange({ enabled: v })}
        />
      </div>
    </div>
  )
}
