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
import { describe, expect, test, vi } from 'vitest'

import {
  orderInviteRewardWrites,
  writeOrderedOptionChanges,
} from '../invite-reward-writes'

const keysOf = (changed: Record<string, unknown>) =>
  orderInviteRewardWrites(changed).map(([key]) => key)

describe('orderInviteRewardWrites', () => {
  test('persists the suffix list before the enable toggle when enabling', () => {
    const order = keysOf({
      InviteRewardEmailRestrictionEnabled: true,
      InviteRewardEmailSuffixes: 'gmail.com',
    })

    expect(order.indexOf('InviteRewardEmailSuffixes')).toBeLessThan(
      order.indexOf('InviteRewardEmailRestrictionEnabled')
    )
  })

  test('drops the enable toggle before clearing the suffix list when disabling', () => {
    const order = keysOf({
      InviteRewardEmailRestrictionEnabled: false,
      InviteRewardEmailSuffixes: '',
    })

    expect(order.indexOf('InviteRewardEmailRestrictionEnabled')).toBeLessThan(
      order.indexOf('InviteRewardEmailSuffixes')
    )
  })

  test('returns a single entry when only the suffix list changed', () => {
    const order = keysOf({ InviteRewardEmailSuffixes: 'gmail.com' })

    expect(order).toEqual(['InviteRewardEmailSuffixes'])
  })

  test('keeps unrelated keys in their original relative order', () => {
    const order = keysOf({
      QuotaForInviter: 1,
      InviteRewardEmailRestrictionEnabled: true,
      QuotaForInvitee: 2,
      InviteRewardEmailSuffixes: 'gmail.com',
    })

    // suffixes first (enabling), then the unrelated keys in original order,
    // with the enable toggle no earlier than the suffixes.
    expect(order[0]).toBe('InviteRewardEmailSuffixes')
    expect(order.indexOf('QuotaForInviter')).toBeLessThan(
      order.indexOf('QuotaForInvitee')
    )
    expect(order.indexOf('InviteRewardEmailSuffixes')).toBeLessThan(
      order.indexOf('InviteRewardEmailRestrictionEnabled')
    )
  })
})

describe('writeOrderedOptionChanges', () => {
  test('writes the suffix list (as comma-joined) before enabling the toggle', async () => {
    const calls: Array<[string, string | number | boolean]> = []
    const writeOption = vi.fn(
      async (key: string, value: string | number | boolean) => {
        calls.push([key, value])
        return { success: true }
      }
    )

    await writeOrderedOptionChanges(
      {
        InviteRewardEmailRestrictionEnabled: true,
        InviteRewardEmailSuffixes: 'gmail.com\n outlook.com \n',
      },
      writeOption,
      'failed'
    )

    expect(calls).toEqual([
      ['InviteRewardEmailSuffixes', 'gmail.com,outlook.com'],
      ['InviteRewardEmailRestrictionEnabled', true],
    ])
  })

  test('throws on the first { success: false } and stops writing further keys', async () => {
    const calls: string[] = []
    const writeOption = vi.fn(async (key: string) => {
      calls.push(key)
      return key === 'InviteRewardEmailSuffixes'
        ? { success: false, message: 'rejected' }
        : { success: true }
    })

    await expect(
      writeOrderedOptionChanges(
        {
          InviteRewardEmailRestrictionEnabled: true,
          InviteRewardEmailSuffixes: '',
        },
        writeOption,
        'failed'
      )
    ).rejects.toThrow('rejected')

    // suffix write ran first and failed, so the enable toggle is never written.
    expect(calls).toEqual(['InviteRewardEmailSuffixes'])
  })

  test('resolves without throwing when every write succeeds', async () => {
    const writeOption = vi.fn(async () => ({ success: true }))

    await expect(
      writeOrderedOptionChanges({ QuotaForInviter: 5 }, writeOption, 'failed')
    ).resolves.toBeUndefined()
    expect(writeOption).toHaveBeenCalledWith('QuotaForInviter', 5)
  })
})
