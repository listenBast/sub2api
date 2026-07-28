import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const listUsers = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin', () => ({
  adminAPI: { users: { list: listUsers } }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

import AdminUserSearchSelect from '../AdminUserSearchSelect.vue'

const matchingUser = {
  id: 18,
  email: 'member@example.com',
  username: 'Member One',
  status: 'active',
  role: 'user',
  deleted_at: null
}

describe('AdminUserSearchSelect', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    listUsers.mockReset()
    listUsers.mockResolvedValue({ items: [matchingUser], total: 1, page: 1, pages: 1 })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('requires selecting a matching user before emitting an email', async () => {
    const wrapper = mount(AdminUserSearchSelect, {
      props: { modelValue: '', placeholder: '搜索用户' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.get('input').setValue('member')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([''])

    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(listUsers).toHaveBeenCalledWith(1, 10, { search: 'member', status: 'active', role: 'user' })
    expect(wrapper.text()).toContain('member@example.com')
    expect(wrapper.text()).toContain('Member One')

    await wrapper.get('[role="option"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['member@example.com'])
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('member@example.com')
  })

  it('filters users that are already part of the selected team', async () => {
    const wrapper = mount(AdminUserSearchSelect, {
      props: { modelValue: '', excludeUserIds: [matchingUser.id] },
      global: { stubs: { Icon: true } }
    })

    await wrapper.get('input').setValue('member')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(wrapper.find('[role="option"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('common.noOptionsFound')
  })
})
