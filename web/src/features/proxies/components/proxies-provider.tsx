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
import React, { useState } from 'react'

import useDialogState from '@/hooks/use-dialog'

import type {
  Proxy,
  ProxiesDialogType,
  ProxyQualityResult,
} from '../types'

type ProxiesContextType = {
  open: ProxiesDialogType | null
  setOpen: (str: ProxiesDialogType | null) => void
  currentRow: Proxy | null
  setCurrentRow: React.Dispatch<React.SetStateAction<Proxy | null>>
  selectedIds: number[]
  setSelectedIds: React.Dispatch<React.SetStateAction<number[]>>
  qualityResult: ProxyQualityResult | null
  setQualityResult: React.Dispatch<
    React.SetStateAction<ProxyQualityResult | null>
  >
  refreshTrigger: number
  triggerRefresh: () => void
}

const ProxiesContext = React.createContext<ProxiesContextType | null>(null)

export function ProxiesProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useDialogState<ProxiesDialogType>(null)
  const [currentRow, setCurrentRow] = useState<Proxy | null>(null)
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [qualityResult, setQualityResult] = useState<ProxyQualityResult | null>(
    null
  )
  const [refreshTrigger, setRefreshTrigger] = useState(0)

  const triggerRefresh = () => setRefreshTrigger((prev) => prev + 1)

  return (
    <ProxiesContext
      value={{
        open,
        setOpen,
        currentRow,
        setCurrentRow,
        selectedIds,
        setSelectedIds,
        qualityResult,
        setQualityResult,
        refreshTrigger,
        triggerRefresh,
      }}
    >
      {children}
    </ProxiesContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useProxies = () => {
  const proxiesContext = React.useContext(ProxiesContext)

  if (!proxiesContext) {
    throw new Error('useProxies has to be used within <ProxiesProvider>')
  }

  return proxiesContext
}
