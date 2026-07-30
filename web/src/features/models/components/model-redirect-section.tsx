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
import {
  ArrowDown,
  ArrowUp,
  Pencil,
  Plus,
  Power,
  PowerOff,
  Trash2,
} from 'lucide-react'
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
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
import { parseGroupsList } from '@/features/channels/lib/channel-utils'
import { getGroups } from '@/features/users/api'

import {
  createModelRedirect,
  deleteModelRedirect,
  listModelRedirects,
  type ModelRedirect,
  type ModelRedirectInput,
  updateModelRedirect,
  updateModelRedirectStatus,
} from '../api-model-redirect'

type TargetDraft = {
  key: string
  channel_id: number
  model: string
  enabled: boolean
}

function emptyTarget(): TargetDraft {
  return {
    key: Math.random().toString(36).slice(2),
    channel_id: 0,
    model: '',
    enabled: true,
  }
}

function parseGroups(groups: string): string[] {
  return groups
    ? groups
        .split(',')
        .map((g) => g.trim())
        .filter(Boolean)
    : []
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

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ['model-redirects'] })

  const filtered = useMemo(() => {
    const q = globalFilter.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((row) => {
      const hay = `${row.name} ${row.groups} ${row.remark}`.toLowerCase()
      return hay.includes(q)
    })
  }, [rows, globalFilter])

  const pageRows = useMemo(() => {
    const start = pagination.pageIndex * pagination.pageSize
    return filtered.slice(start, start + pagination.pageSize)
  }, [filtered, pagination])

  const columns = useModelRedirectColumns({
    onEdit: openEdit,
    onChanged: invalidate,
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
      emptyTitle='暂无模型重定向'
      emptyDescription='创建虚拟模型，并按优先级配置渠道目标，用于高可用降级。'
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
}): ColumnDef<ModelRedirect>[] {
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
          const targets = [...(row.original.targets || [])].sort(
            (a, b) => a.priority - b.priority
          )
          if (targets.length === 0) {
            return <span className='text-muted-foreground text-sm'>0</span>
          }
          return (
            <div className='flex max-w-md flex-col gap-0.5'>
              {targets.slice(0, 3).map((target) => (
                <span
                  key={`${target.priority}-${target.channel_id}-${target.model}`}
                  className='text-muted-foreground truncate font-mono text-xs'
                >
                  P{target.priority}: #{target.channel_id}
                  {target.model ? ` → ${target.model}` : ' → 透传'}
                </span>
              ))}
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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- callbacks from parent section
    []
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
  const isEdit = !!props.editing

  const [name, setName] = useState('')
  const [groups, setGroups] = useState<string[]>([])
  const [remark, setRemark] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [targets, setTargets] = useState<TargetDraft[]>([emptyTarget()])

  useEffect(() => {
    if (!props.open) return
    if (props.editing) {
      setName(props.editing.name)
      setGroups(parseGroups(props.editing.groups))
      setRemark(props.editing.remark || '')
      setEnabled(props.editing.enabled)
      const ts = [...(props.editing.targets || [])].sort(
        (a, b) => a.priority - b.priority
      )
      setTargets(
        ts.length
          ? ts.map((target) => ({
              key: String(target.id ?? Math.random()),
              channel_id: target.channel_id,
              model: target.model || '',
              enabled: target.enabled,
            }))
          : [emptyTarget()]
      )
    } else {
      setName('')
      setGroups([])
      setRemark('')
      setEnabled(true)
      setTargets([emptyTarget()])
    }
  }, [props.open, props.editing])

  const { data: groupList = [] } = useQuery({
    queryKey: ['groups'],
    queryFn: async () => {
      const res = await getGroups()
      return (res.data ?? []) as string[]
    },
    enabled: props.open,
  })

  const { data: channels = [] } = useQuery({
    queryKey: ['channels-for-redirect'],
    queryFn: async () => {
      const res = await getChannels({ p: 0, page_size: 500 })
      return (res.data?.items ?? []).map((ch) => ({
        id: ch.id,
        name: ch.name,
      }))
    },
    enabled: props.open,
  })

  const groupOptions = useMemo(
    () => groupList.map((g) => ({ value: g, label: g })),
    [groupList]
  )

  const saveMutation = useMutation({
    mutationFn: async () => {
      const trimmedName = name.trim()
      if (!trimmedName) {
        throw new Error('请填写虚拟模型名称')
      }
      if (groups.length === 0) {
        throw new Error('请至少选择一个分组')
      }
      if (targets.some((target) => !target.channel_id || target.channel_id <= 0)) {
        throw new Error('每个目标都必须选择渠道')
      }
      const input: ModelRedirectInput = {
        name: trimmedName,
        groups,
        enabled,
        remark,
        targets: targets.map((target, i) => ({
          priority: i + 1,
          channel_id: target.channel_id,
          model: target.model.trim(),
          enabled: target.enabled,
        })),
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

  const move = (index: number, dir: -1 | 1) => {
    const next = [...targets]
    const j = index + dir
    if (j < 0 || j >= next.length) return
    ;[next[index], next[j]] = [next[j], next[index]]
    setTargets(next)
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-2xl')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isEdit ? '编辑模型重定向' : '创建模型重定向'}
          </SheetTitle>
          <SheetDescription>
            {isEdit
              ? '修改虚拟模型与优先级目标，完成后保存。'
              : '创建虚拟模型，并按优先级在多个渠道间降级。计费以最终成功的那一档为准。'}
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
                placeholder='model'
                className='font-mono'
              />
              <p className='text-muted-foreground text-xs'>
                客户端请求此名称，流量按下方优先级列表路由。
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
            <div className='flex items-center justify-between gap-2'>
              <div>
                <h3 className='text-sm font-semibold'>优先级目标</h3>
                <p className='text-muted-foreground text-xs'>
                  越靠上优先级越高。模型留空则透传虚拟模型名称。
                </p>
              </div>
              <Button
                type='button'
                size='sm'
                variant='outline'
                onClick={() => setTargets((prev) => [...prev, emptyTarget()])}
              >
                <Plus className='h-4 w-4' />
                添加目标
              </Button>
            </div>

            <div className='space-y-3'>
              {targets.map((target, index) => (
                <div
                  key={target.key}
                  className='border-border/60 space-y-3 rounded-lg border p-3'
                >
                  <div className='flex items-center justify-between gap-2'>
                    <span className='text-sm font-medium'>
                      优先级 {index + 1}
                    </span>
                    <div className='flex gap-1'>
                      <Button
                        type='button'
                        size='icon-sm'
                        variant='ghost'
                        onClick={() => move(index, -1)}
                        disabled={index === 0}
                        aria-label='上移'
                      >
                        <ArrowUp className='h-4 w-4' />
                      </Button>
                      <Button
                        type='button'
                        size='icon-sm'
                        variant='ghost'
                        onClick={() => move(index, 1)}
                        disabled={index === targets.length - 1}
                        aria-label='下移'
                      >
                        <ArrowDown className='h-4 w-4' />
                      </Button>
                      <Button
                        type='button'
                        size='icon-sm'
                        variant='ghost'
                        onClick={() =>
                          setTargets((prev) =>
                            prev.length <= 1
                              ? prev
                              : prev.filter((_, i) => i !== index)
                          )
                        }
                        disabled={targets.length <= 1}
                        aria-label='移除'
                      >
                        <Trash2 className='h-4 w-4' />
                      </Button>
                    </div>
                  </div>

                  <div className='space-y-2'>
                    <Label>渠道 *</Label>
                    <Select
                      value={
                        target.channel_id > 0
                          ? String(target.channel_id)
                          : undefined
                      }
                      onValueChange={(v) =>
                        setTargets((prev) =>
                          prev.map((item, i) =>
                            i === index
                              ? { ...item, channel_id: Number(v) }
                              : item
                          )
                        )
                      }
                    >
                      <SelectTrigger>
                        <SelectValue placeholder='选择渠道' />
                      </SelectTrigger>
                      <SelectContent>
                        {channels.map((ch) => (
                          <SelectItem key={ch.id} value={String(ch.id)}>
                            #{ch.id} {ch.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>

                  <div className='space-y-2'>
                    <Label>
                      模型{' '}
                      <span className='text-muted-foreground font-normal'>
                        （可选，留空则透传）
                      </span>
                    </Label>
                    <Input
                      value={target.model}
                      onChange={(e) =>
                        setTargets((prev) =>
                          prev.map((item, i) =>
                            i === index
                              ? { ...item, model: e.target.value }
                              : item
                          )
                        )
                      }
                      placeholder='留空则透传虚拟模型名'
                      className='font-mono'
                    />
                  </div>
                </div>
              ))}
            </div>
          </SideDrawerSection>
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
