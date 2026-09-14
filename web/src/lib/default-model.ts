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

/**
 * Pick the model to preselect for a user.
 *
 * The admin-configured default (`general_setting.default_model`) wins when the
 * user can actually use it; otherwise the first model in the user's list is
 * used. Returns `undefined` when the user has no models at all.
 */
export function pickDefaultModel(
  models: readonly string[],
  preferredModel: string | undefined | null
): string | undefined {
  const preferred = preferredModel?.trim()
  if (preferred && models.includes(preferred)) {
    return preferred
  }
  return models[0]
}
