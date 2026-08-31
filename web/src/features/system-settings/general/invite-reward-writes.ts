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

/**
 * Order the per-key option writes so the invite-reward suffix list and its
 * enable toggle never violate the backend invariant during a single save:
 * - when enabling, the suffix list must be persisted before the toggle
 *   (the backend refuses to enable while the stored list is empty);
 * - when disabling, the toggle must be dropped before the list is cleared
 *   (the backend refuses to clear the list while enabled).
 *
 * Other keys keep their original relative order (Array.prototype.sort is
 * stable). Returns the ordered [key, value] entries to write in sequence.
 */
export function orderInviteRewardWrites(
  changedFields: Record<string, unknown>
): Array<[string, unknown]> {
  const enabling = changedFields[INVITE_REWARD_ENABLE_KEY] === true
  const rankOf = (key: string): number => {
    if (key === INVITE_REWARD_SUFFIXES_KEY) return enabling ? 0 : 2
    if (key === INVITE_REWARD_ENABLE_KEY) return 1
    return 1
  }
  return Object.entries(changedFields).sort(
    (a, b) => rankOf(a[0]) - rankOf(b[0])
  )
}

type OptionWriteResult = { success: boolean; message?: string }

/**
 * Persist the changed quota-section fields one key at a time, ordered by
 * {@link orderInviteRewardWrites}, converting the newline-separated suffix
 * textarea back to the comma-separated storage format.
 *
 * The option API returns HTTP 200 with `{ success: false }` for a rejected
 * write (e.g. enabling with an empty list); this throws on the first such
 * result so the caller's form is not reset as if every change had persisted.
 */
export async function writeOrderedOptionChanges(
  changedFields: Record<string, unknown>,
  writeOption: (
    key: string,
    value: string | number | boolean
  ) => Promise<OptionWriteResult>,
  failureMessage: string
): Promise<void> {
  for (const [key, value] of orderInviteRewardWrites(changedFields)) {
    let outValue = value as string | number | boolean
    if (key === INVITE_REWARD_SUFFIXES_KEY && typeof value === 'string') {
      outValue = value
        .split('\n')
        .map((suffix) => suffix.trim())
        .filter(Boolean)
        .join(',')
    }
    const result = await writeOption(key, outValue)
    if (result && result.success === false) {
      throw new Error(result.message || failureMessage)
    }
  }
}
