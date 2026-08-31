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

export const INVITE_REWARD_ENABLE_KEY = 'InviteRewardEmailRestrictionEnabled'
export const INVITE_REWARD_SUFFIXES_KEY = 'InviteRewardEmailSuffixes'

export type InviteRewardConfigWrite = { enabled: boolean; suffixes: string }

/** Convert the newline-separated suffix textarea to the comma-separated
 * storage format (trimmed, blanks dropped). Server-side validation rejects
 * any remaining non-domain entries. */
export function suffixesToStorage(value: string): string {
  return value
    .split('\n')
    .map((suffix) => suffix.trim())
    .filter(Boolean)
    .join(',')
}

/**
 * Split the changed quota-section fields into the atomic invite-reward pair and
 * the remaining per-key option writes. The invite-reward enable toggle and its
 * suffix list are interdependent, so whenever either changed they are committed
 * together in one request — carrying BOTH current values from `formValues` so
 * the backend can validate and persist the complete pair atomically (which holds
 * the invariant across a multi-instance deployment). Other keys are written
 * individually as before.
 */
export function splitQuotaSettingWrites(
  changedFields: Record<string, unknown>,
  formValues: Record<string, unknown>
): {
  inviteReward: InviteRewardConfigWrite | null
  otherWrites: Array<[string, unknown]>
} {
  let inviteRewardChanged = false
  const otherWrites: Array<[string, unknown]> = []
  for (const [key, value] of Object.entries(changedFields)) {
    if (
      key === INVITE_REWARD_ENABLE_KEY ||
      key === INVITE_REWARD_SUFFIXES_KEY
    ) {
      inviteRewardChanged = true
      continue
    }
    otherWrites.push([key, value])
  }

  const inviteReward: InviteRewardConfigWrite | null = inviteRewardChanged
    ? {
        enabled: formValues[INVITE_REWARD_ENABLE_KEY] === true,
        suffixes: suffixesToStorage(
          String(formValues[INVITE_REWARD_SUFFIXES_KEY] ?? '')
        ),
      }
    : null

  return { inviteReward, otherWrites }
}
