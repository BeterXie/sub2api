import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { brandAPI } from '@/api/brand'
import { useBrandStore } from './brand'

vi.mock('@/api/brand', () => ({ brandAPI: { config: vi.fn(), access: vi.fn() } }))
vi.mock('./app', () => ({ useAppStore: () => ({ applySettings: vi.fn() }) }))
const config = {
  enabled: true,
  brand: { brand_id: 2, brand_code: 'mues', name: 'MUES', domain_id: 5, hostname: 'mues.cc', canonical_api_origin: 'https://mues.cc', registration_enabled: false },
  settings: { site_name: 'MUES', theme_color: '#a84320' }
}
describe('trusted brand bootstrap and operator selection', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear(); sessionStorage.clear(); vi.clearAllMocks()
    delete window.__BRAND_CONFIG__
  })
  it('refreshes stale injected branding before using the site', async () => {
    window.__BRAND_CONFIG__ = { ...config, brand: { ...config.brand, brand_id: 1, brand_code: 'llmp' } }
    vi.mocked(brandAPI.config).mockResolvedValue(config)
    const brand = useBrandStore()
    await brand.initialize()
    expect(brand.code).toBe('mues')
    expect(brand.apiOrigin).toBe('https://mues.cc')
    expect(document.documentElement.dataset.brand).toBe('mues')
  })
  it('clears platform selection when loading a tenant operator', async () => {
    const brand = useBrandStore(); brand.apply(config)
    sessionStorage.setItem('admin_selected_brand', '1'); localStorage.setItem('auth_token', 'operator-token')
    vi.mocked(brandAPI.access).mockResolvedValue({ role: 'support', brand_id: 2 })
    await brand.loadAccess()
    expect(brand.role).toBe('support')
    expect(brand.selectedBrandID).toBeNull()
    expect(brand.canEditContent).toBe(false)
    expect(brand.canVisit('/admin/accounts')).toBe(false)
    expect(brand.canVisit('/admin/brand-content')).toBe(true)
		expect(brand.canVisit('/admin/orders')).toBe(true)
		expect(brand.canVisit('/admin/orders/dashboard')).toBe(true)
  })
  it('ignores an access response for a user who has signed out', async () => {
    const brand = useBrandStore(); brand.apply(config)
    localStorage.setItem('auth_token', 'old-token')
    let resolve!: (value: { role: 'super_admin'; brand_id: number }) => void
    vi.mocked(brandAPI.access).mockReturnValue(new Promise(r => { resolve = r }))
    const pending = brand.loadAccess()
    localStorage.removeItem('auth_token'); brand.resetAccess()
    resolve({ role: 'super_admin', brand_id: 1 }); await pending
    expect(brand.isPlatformAdmin).toBe(false)
  })
	it('reloads brand access after an in-session access token rotation', async () => {
		const brand = useBrandStore(); brand.apply(config)
		localStorage.setItem('auth_token', 'old-token')
		localStorage.setItem('auth_user', JSON.stringify({ id: 42, email: 'operator@example.com' }))
		let resolveFirst!: (value: { role: 'support'; brand_id: number }) => void
		vi.mocked(brandAPI.access)
			.mockReturnValueOnce(new Promise(resolve => { resolveFirst = resolve }))
			.mockResolvedValueOnce({ role: 'support', brand_id: 2 })

		const pending = brand.loadAccess()
		localStorage.setItem('auth_token', 'rotated-token')
		resolveFirst({ role: 'support', brand_id: 2 })
		await pending

		expect(brandAPI.access).toHaveBeenCalledTimes(2)
		expect(brand.role).toBe('support')
	})
	it('does not reuse an access response after switching users', async () => {
		const brand = useBrandStore(); brand.apply(config)
		localStorage.setItem('auth_token', 'old-token')
		localStorage.setItem('auth_user', JSON.stringify({ id: 42 }))
		let resolve!: (value: { role: 'super_admin'; brand_id: number }) => void
		vi.mocked(brandAPI.access).mockReturnValue(new Promise(r => { resolve = r }))

		const pending = brand.loadAccess()
		localStorage.setItem('auth_token', 'other-user-token')
		localStorage.setItem('auth_user', JSON.stringify({ id: 84 }))
		resolve({ role: 'super_admin', brand_id: 1 })
		await pending

		expect(brandAPI.access).toHaveBeenCalledOnce()
		expect(brand.role).toBe('')
	})
})
