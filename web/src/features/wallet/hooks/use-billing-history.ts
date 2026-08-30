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
import i18next from 'i18next'
import { useState, useEffect, useCallback, useRef } from 'react'
import { toast } from 'sonner'

import { useIsAdmin } from '@/hooks/use-admin'
import { useDebounce } from '@/hooks/use-debounce'

import {
  getUserBillingHistory,
  getAllBillingHistory,
  completeOrder,
  refundOrder,
  queryRefundOrder,
  isApiSuccess,
} from '../api'
import type { TopupRecord } from '../types'

// Poll a processing refund until the gateway confirms it (or fails). The admin
// initiated the action and is watching, so bound the wait rather than looping
// forever: ~2s interval, up to 60s total.
const REFUND_POLL_INTERVAL_MS = 2000
const REFUND_POLL_MAX_ATTEMPTS = 30

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

// ============================================================================
// Billing History Hook
// ============================================================================

interface UseBillingHistoryOptions {
  /** Initial page number */
  initialPage?: number
  /** Initial page size */
  initialPageSize?: number
}

export function useBillingHistory(options: UseBillingHistoryOptions = {}) {
  const { initialPage = 1, initialPageSize = 10 } = options
  const isAdmin = useIsAdmin()

  const [records, setRecords] = useState<TopupRecord[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(initialPage)
  const [pageSize, setPageSize] = useState(initialPageSize)
  const [keyword, setKeyword] = useState('')
  const debouncedKeyword = useDebounce(keyword)
  const requestIdRef = useRef(0)
  const [loading, setLoading] = useState(false)
  const [completing, setCompleting] = useState(false)
  const [refunding, setRefunding] = useState(false)

  /**
   * Fetch billing history
   */
  const fetchBillingHistory = useCallback(async () => {
    const requestId = ++requestIdRef.current
    setLoading(true)
    try {
      const response = isAdmin
        ? await getAllBillingHistory(page, pageSize, debouncedKeyword)
        : await getUserBillingHistory(page, pageSize, debouncedKeyword)

      if (requestId !== requestIdRef.current) return

      if (isApiSuccess(response) && response.data) {
        setRecords(response.data.items || [])
        setTotal(response.data.total || 0)
      } else {
        toast.error(
          response.message || i18next.t('Failed to load billing history')
        )
        setRecords([])
        setTotal(0)
      }
    } catch (error) {
      if (requestId !== requestIdRef.current) return

      // eslint-disable-next-line no-console
      console.error('Failed to fetch billing history:', error)
      toast.error(i18next.t('Failed to load billing history'))
      setRecords([])
      setTotal(0)
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false)
      }
    }
  }, [debouncedKeyword, isAdmin, page, pageSize])

  /**
   * Complete a pending order (admin only)
   */
  const handleCompleteOrder = useCallback(
    async (tradeNo: string) => {
      if (!isAdmin) {
        toast.error(i18next.t('Admin access required'))
        return false
      }

      setCompleting(true)
      try {
        const response = await completeOrder({ trade_no: tradeNo })
        if (isApiSuccess(response)) {
          toast.success(i18next.t('Order completed successfully'))
          // Refresh the list
          await fetchBillingHistory()
          return true
        } else {
          toast.error(response.message || i18next.t('Failed to complete order'))
          return false
        }
      } catch (error) {
        // eslint-disable-next-line no-console
        console.error('Failed to complete order:', error)
        toast.error(i18next.t('Failed to complete order'))
        return false
      } finally {
        setCompleting(false)
      }
    },
    [isAdmin, fetchBillingHistory]
  )

  /**
   * Refund a WeChat/Alipay native QR order (admin only)
   */
  const handleRefundOrder = useCallback(
    async (tradeNo: string) => {
      if (!isAdmin) {
        toast.error(i18next.t('Admin access required'))
        return false
      }

      setRefunding(true)
      try {
        const response = await refundOrder({ trade_no: tradeNo })
        if (!isApiSuccess(response)) {
          toast.error(response.message || i18next.t('Failed to refund order'))
          return false
        }

        if (response.data?.status === 'refunded') {
          toast.success(i18next.t('Order refunded successfully'))
          await fetchBillingHistory()
          return true
        }

        // Gateway is still processing (typically WeChat async refunds). Quota is
        // NOT rolled back yet; poll until the gateway confirms success or fails.
        toast.info(i18next.t('Refund is processing, confirming with the payment provider...'))
        await fetchBillingHistory()
        for (let attempt = 0; attempt < REFUND_POLL_MAX_ATTEMPTS; attempt++) {
          await sleep(REFUND_POLL_INTERVAL_MS)
          const poll = await queryRefundOrder(tradeNo)
          if (!isApiSuccess(poll)) continue
          const status = poll.data?.status
          if (status === 'refunded') {
            toast.success(i18next.t('Order refunded successfully'))
            await fetchBillingHistory()
            return true
          }
          if (status === 'success') {
            // Reverted: the gateway ultimately rejected the refund.
            toast.error(i18next.t('Refund failed, please verify in the merchant console'))
            await fetchBillingHistory()
            return false
          }
        }
        toast.info(i18next.t('Refund is still processing, please check again later'))
        await fetchBillingHistory()
        return false
      } catch (error) {
        // eslint-disable-next-line no-console
        console.error('Failed to refund order:', error)
        toast.error(i18next.t('Failed to refund order'))
        return false
      } finally {
        setRefunding(false)
      }
    },
    [isAdmin, fetchBillingHistory]
  )

  /**
   * Change page
   */
  const handlePageChange = useCallback((newPage: number) => {
    setPage(newPage)
  }, [])

  /**
   * Change page size
   */
  const handlePageSizeChange = useCallback((newPageSize: number) => {
    setPageSize(newPageSize)
    setPage(1) // Reset to first page when changing page size
  }, [])

  /**
   * Search by keyword
   */
  const handleSearch = useCallback((newKeyword: string) => {
    requestIdRef.current += 1
    setKeyword(newKeyword)
    setPage(1) // Reset to first page when searching
  }, [])

  // Fetch data after the search draft has settled.
  useEffect(() => {
    if (keyword !== debouncedKeyword) return

    fetchBillingHistory()
  }, [debouncedKeyword, fetchBillingHistory, keyword])

  return {
    records,
    total,
    page,
    pageSize,
    keyword,
    loading,
    completing,
    refunding,
    isAdmin,
    handlePageChange,
    handlePageSizeChange,
    handleSearch,
    handleCompleteOrder,
    handleRefundOrder,
    refresh: fetchBillingHistory,
  }
}
