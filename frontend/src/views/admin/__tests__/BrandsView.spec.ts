import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import BrandsView from '../BrandsView.vue'
import { brandAPI, type Brand, type BrandDomain } from '@/api/brand'

const appStore = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))

vi.mock('@/api/brand', () => ({
	brandAPI: {
		list: vi.fn(),
		domains: vi.fn(),
		grants: vi.fn(),
	},
}))
vi.mock('@/stores/app', () => ({
	useAppStore: () => appStore,
}))
vi.mock('vue-i18n', async importOriginal => ({
	...(await importOriginal<typeof import('vue-i18n')>()),
	useI18n: () => ({ t: (key: string) => key }),
}))

const brands: Brand[] = [
	{ id: 1, code: 'llmp', name: 'LLMP', status: 'active', registration_enabled: true, max_concurrent: 1, rpm_limit: 10 },
	{ id: 2, code: 'mues', name: 'MUES', status: 'active', registration_enabled: true, max_concurrent: 2, rpm_limit: 20 },
]
const domain = (id: number, brandID: number, hostname: string): BrandDomain => ({
	id,
	brand_id: brandID,
	hostname,
	enabled: true,
	primary_flag: true,
	public_enabled: true,
	gateway_enabled: true,
	overrides: {},
	metadata: { owner: hostname },
})

function deferred<T>() {
	let resolve!: (value: T) => void
	const promise = new Promise<T>(done => { resolve = done })
	return { promise, resolve }
}

describe('BrandsView selection', () => {
	beforeEach(() => {
		vi.clearAllMocks()
		vi.mocked(brandAPI.list).mockResolvedValue(brands)
		vi.mocked(brandAPI.domains).mockResolvedValue([domain(11, 1, 'llmp.org')])
		vi.mocked(brandAPI.grants).mockResolvedValue([])
	})

	it('ignores stale brand detail responses and resets the domain editor', async () => {
		const wrapper = mount(BrandsView, {
			global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } },
		})
		await flushPromises()
		await flushPromises()
		expect(appStore.showError).not.toHaveBeenCalled()
		expect(brandAPI.list).toHaveBeenCalledTimes(1)
		expect(brandAPI.domains).toHaveBeenCalledWith(1)
		expect(brandAPI.grants).toHaveBeenCalledWith(1)
		expect((wrapper.get('[data-testid="brand-scope"]').element as HTMLSelectElement).value).toBe('1')
		expect((wrapper.findAll('input')[0].element as HTMLInputElement).value).toBe('LLMP')
		await wrapper.get('section .border-b button').trigger('click')
		expect((wrapper.get('[data-testid="domain-hostname"]').element as HTMLInputElement).value).toBe('llmp.org')

		const staleDomains = deferred<BrandDomain[]>()
		const staleGrants = deferred<{ user_id: number; role: 'support' }[]>()
		vi.mocked(brandAPI.domains).mockImplementation(id => id === 2 ? staleDomains.promise : Promise.resolve([domain(12, 1, 'current.llmp.org')]))
		vi.mocked(brandAPI.grants).mockImplementation(id => id === 2 ? staleGrants.promise : Promise.resolve([]))

		await wrapper.get('[data-testid="brand-scope"]').setValue('2')
		expect((wrapper.get('[data-testid="domain-hostname"]').element as HTMLInputElement).value).toBe('')
		await wrapper.get('[data-testid="brand-scope"]').setValue('1')
		await flushPromises()
		expect(wrapper.text()).toContain('current.llmp.org')

		staleDomains.resolve([domain(21, 2, 'stale.mues.cc')])
		staleGrants.resolve([{ user_id: 99, role: 'support' }])
		await flushPromises()
		expect(wrapper.text()).not.toContain('stale.mues.cc')
		expect(wrapper.text()).not.toContain('99 · brand.support')

		await wrapper.get('section .border-b button').trigger('click')
		await wrapper.get('button.btn-secondary').trigger('click')
		expect(wrapper.find('[data-testid="domain-hostname"]').exists()).toBe(false)
		await wrapper.get('[data-testid="brand-scope"]').setValue('1')
		await flushPromises()
		expect((wrapper.get('[data-testid="domain-hostname"]').element as HTMLInputElement).value).toBe('')
	})
})
