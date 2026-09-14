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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { GlobalSettingsCard } from '../global-settings-card'

const defaultValues = {
  global: {
    pass_through_request_enabled: false,
    thinking_model_blacklist: '[]',
    chat_completions_to_responses_policy: '{}',
  },
  general_setting: {
    ping_interval_enabled: false,
    ping_interval_seconds: 60,
    default_model: '',
  },
}

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false },
        },
      })
  )
  return (
    <QueryClientProvider client={client}>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <GlobalSettingsCard defaultValues={defaultValues} />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/models_enabled') {
      return { data: { success: true, data: ['grok-4.6', 'gpt-4o'] } }
    }
    throw new Error(`Unexpected request: ${url}`)
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('default model setting', () => {
  it('lists enabled models and saves the picked one', async () => {
    const user = userEvent.setup()
    render(<Fixture />)
    const input = screen.getByRole('combobox', { name: 'Default Model' })

    await user.click(input)
    expect(await screen.findByRole('option', { name: 'gpt-4o' })).toBeVisible()
    expect(screen.getByRole('option', { name: 'grok-4.6' })).toBeVisible()
    await user.click(screen.getByRole('option', { name: 'gpt-4o' }))
    await waitFor(() => expect(input).toHaveValue('gpt-4o'))

    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/api/option/', {
        key: 'general_setting.default_model',
        value: 'gpt-4o',
      })
    )
  })

  it('accepts a typed model name that is not in the enabled list', async () => {
    const user = userEvent.setup()
    render(<Fixture />)
    const input = screen.getByRole('combobox', { name: 'Default Model' })

    await user.click(input)
    await user.type(input, 'claude-sonnet-5')
    expect(screen.getByText('No model found.')).toBeVisible()
    await user.tab()

    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/api/option/', {
        key: 'general_setting.default_model',
        value: 'claude-sonnet-5',
      })
    )
  })
})
