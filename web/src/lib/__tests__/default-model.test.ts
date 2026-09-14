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
import { describe, expect, it } from 'vitest'

import { pickDefaultModel } from '../default-model'

describe('pickDefaultModel', () => {
  it('returns the admin default when the user can use it', () => {
    expect(
      pickDefaultModel(['grok-4.6', 'claude-sonnet-5', 'gpt-4o'], 'gpt-4o')
    ).toBe('gpt-4o')
  })

  it('trims whitespace around the admin default before matching', () => {
    expect(pickDefaultModel(['grok-4.6', 'gpt-4o'], '  gpt-4o ')).toBe('gpt-4o')
  })

  it('falls back to the first available model when the admin default is not usable', () => {
    expect(pickDefaultModel(['grok-4.6', 'gpt-4o'], 'claude-sonnet-5')).toBe(
      'grok-4.6'
    )
  })

  it('falls back to the first available model when no admin default is set', () => {
    expect(pickDefaultModel(['grok-4.6', 'gpt-4o'], '')).toBe('grok-4.6')
    expect(pickDefaultModel(['grok-4.6', 'gpt-4o'], undefined)).toBe('grok-4.6')
  })

  it('returns undefined when the user has no models', () => {
    expect(pickDefaultModel([], 'gpt-4o')).toBeUndefined()
  })
})
