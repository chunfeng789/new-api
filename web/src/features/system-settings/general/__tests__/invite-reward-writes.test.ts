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
import { describe, expect, test } from 'vitest'

import {
  splitQuotaSettingWrites,
  suffixesToStorage,
} from '../invite-reward-writes'

describe('suffixesToStorage', () => {
  test('converts newlines to a trimmed, blank-free comma list', () => {
    expect(suffixesToStorage('gmail.com\n outlook.com \n\n')).toBe(
      'gmail.com,outlook.com'
    )
  })
})

describe('splitQuotaSettingWrites', () => {
  test('bundles both invite-reward values when only the toggle changed', () => {
    const { inviteReward, otherWrites } = splitQuotaSettingWrites(
      { InviteRewardEmailRestrictionEnabled: true },
      {
        InviteRewardEmailRestrictionEnabled: true,
        InviteRewardEmailSuffixes: 'gmail.com\noutlook.com',
      }
    )

    expect(inviteReward).toEqual({
      enabled: true,
      suffixes: 'gmail.com,outlook.com',
    })
    expect(otherWrites).toEqual([])
  })

  test('bundles both invite-reward values when only the suffix list changed', () => {
    const { inviteReward, otherWrites } = splitQuotaSettingWrites(
      { InviteRewardEmailSuffixes: 'gmail.com' },
      {
        InviteRewardEmailRestrictionEnabled: false,
        InviteRewardEmailSuffixes: 'gmail.com',
      }
    )

    expect(inviteReward).toEqual({ enabled: false, suffixes: 'gmail.com' })
    expect(otherWrites).toEqual([])
  })

  test('separates unrelated keys into per-key writes and omits the pair', () => {
    const { inviteReward, otherWrites } = splitQuotaSettingWrites(
      { QuotaForInviter: 5, TopUpLink: 'https://x' },
      {}
    )

    expect(inviteReward).toBeNull()
    expect(otherWrites).toEqual([
      ['QuotaForInviter', 5],
      ['TopUpLink', 'https://x'],
    ])
  })

  test('returns both the pair and the remaining per-key writes together', () => {
    const { inviteReward, otherWrites } = splitQuotaSettingWrites(
      { QuotaForInviter: 5, InviteRewardEmailRestrictionEnabled: true },
      {
        QuotaForInviter: 5,
        InviteRewardEmailRestrictionEnabled: true,
        InviteRewardEmailSuffixes: 'gmail.com',
      }
    )

    expect(inviteReward).toEqual({ enabled: true, suffixes: 'gmail.com' })
    expect(otherWrites).toEqual([['QuotaForInviter', 5]])
  })
})
