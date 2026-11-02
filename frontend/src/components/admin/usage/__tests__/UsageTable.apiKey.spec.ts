import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('@/utils/ipGeoLookup', () => ({ getEntry: vi.fn(() => ({ status: 'idle' })), fetchOne: vi.fn(), fetchBatch: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        key === 'usage.apiKeyDeleted' ? `${params?.name} (deleted)` : key
    })
  }
})

import UsageTable from '../UsageTable.vue'

const DataTableStub = {
  props: ['data'],
  template: `<div><div v-for="row in data" :key="row.request_id" :data-row="row.request_id"><slot name="cell-api_key" :row="row" /></div></div>`
}

function render(rows: Array<Record<string, unknown>>) {
  return mount(UsageTable, {
    props: { data: rows as never, loading: false, columns: [] },
    global: { stubs: { DataTable: DataTableStub, EmptyState: true, Icon: true, Teleport: true } }
  })
}

describe('UsageTable API key column', () => {
  it('shows a deleted key as its original name plus the deleted marker, never the key value', () => {
    const wrapper = render([
      { request_id: 'r1', api_key_id: 1, api_key: { id: 1, name: 'gzt', key: '', deleted_at: '2026-11-01T08:00:00Z' } },
      { request_id: 'r2', api_key_id: 2, api_key: { id: 2, name: 'prod', key: 'sk-live' } },
      { request_id: 'r3', api_key_id: 3 }
    ])
    const cells = wrapper.findAll('[data-row]').map((row) => row.text())
    expect(cells).toEqual(['gzt (deleted)', 'prod', '-'])
    expect(wrapper.text()).not.toContain('__deleted__')
  })
})
