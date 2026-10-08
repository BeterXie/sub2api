import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { brandAPI, type BrandConfig, type BrandAdminRole } from '@/api/brand'
import { useAppStore } from './app'
import { themes } from '@/brands/themes'

declare global {
  interface Window { __BRAND_CONFIG__?: BrandConfig }
}

export const useBrandStore = defineStore('brand', () => {
  const config = ref<BrandConfig>({ enabled: false })
  const role = ref<BrandAdminRole>('')
  const selectedBrandID = ref<number | null>(null)
  const enabled = computed(() => config.value.enabled)
  const scope = computed(() => config.value.brand)
  const code = computed(() => scope.value?.brand_code || 'llmp')
  const settings = computed(() => config.value.settings || {})
  const theme = computed(() => themes[code.value] || themes.llmp)
  const name = computed(() => String(settings.value.site_name || scope.value?.name || 'LLMP'))
  const apiOrigin = computed(() => scope.value?.canonical_api_origin || window.location.origin)
  const isPlatformAdmin = computed(() => role.value === 'super_admin')
  const canEditContent = computed(() => ['super_admin', 'owner', 'operator'].includes(role.value))
  const canEditSettings = computed(() => ['super_admin', 'owner'].includes(role.value))
  let accessUserToken = ''
  let accessRequest: Promise<void> | null = null
	function sessionIdentity() {
		const raw = localStorage.getItem('auth_user')
		if (!raw) return ''
		try {
			const user = JSON.parse(raw) as { id?: unknown; user_id?: unknown; email?: unknown }
			return String(user.id ?? user.user_id ?? user.email ?? '')
		} catch {
			return ''
		}
	}

  function apply(value: BrandConfig) {
    config.value = value
    if (!value.enabled) return
    document.documentElement.dataset.brand = code.value
    const color = String(settings.value.theme_color || theme.value.accent)
    if (/^#[0-9a-f]{6}$/i.test(color)) document.documentElement.style.setProperty('--brand-accent', color)
    if (value.public_settings) useAppStore().applySettings(value.public_settings)
    const title = settings.value.seo_title
    if (typeof title === 'string' && title) document.title = title
  }
  async function initialize() {
    if (window.__BRAND_CONFIG__) apply(window.__BRAND_CONFIG__)
    const fresh = await brandAPI.config()
    apply(fresh)
  }
  async function loadAccess(force = false): Promise<void> {
    const token = localStorage.getItem('auth_token') || ''
    if (!enabled.value || !token) { resetAccess(); return }
    if (accessRequest) {
      await accessRequest
      return loadAccess(force)
    }
    if (!force && token === accessUserToken) return
		const userIdentity = sessionIdentity()
		let retryForRotatedToken = false
    accessRequest = brandAPI.access().then(value => {
			const currentToken = localStorage.getItem('auth_token') || ''
			if (!currentToken || sessionIdentity() !== userIdentity) return
			if (currentToken !== token) {
				retryForRotatedToken = userIdentity !== ''
				return
			}
      role.value = value.role || ''
      accessUserToken = token
      if (!isPlatformAdmin.value) selectBrand(null)
      else selectedBrandID.value = Number(sessionStorage.getItem('admin_selected_brand')) || null
		}).catch(() => {
			const currentToken = localStorage.getItem('auth_token') || ''
			if (currentToken && sessionIdentity() === userIdentity && currentToken !== token && userIdentity !== '') {
				retryForRotatedToken = true
				return
			}
			if (currentToken === token && sessionIdentity() === userIdentity) {
				role.value = ''
				accessUserToken = ''
			}
		}).finally(() => { accessRequest = null })
		await accessRequest
		if (retryForRotatedToken) await loadAccess(true)
  }
  function selectBrand(id: number | null) {
    selectedBrandID.value = id
    if (id) sessionStorage.setItem('admin_selected_brand', String(id))
    else sessionStorage.removeItem('admin_selected_brand')
  }
  function resetAccess() { role.value = ''; accessUserToken = ''; selectBrand(null) }
  function canVisit(path: string) {
    if (!enabled.value || isPlatformAdmin.value) return true
    return ['/admin/brand-content', '/admin/users', '/admin/groups', '/admin/dashboard',
      '/admin/subscriptions', '/admin/announcements', '/admin/redeem', '/admin/promo-codes',
			'/admin/affiliates', '/admin/orders', '/admin/usage', '/admin/audit-logs']
      .some(prefix => path === prefix || path.startsWith(prefix + '/'))
  }
  return { config, role, enabled, scope, code, name, settings, theme, apiOrigin, isPlatformAdmin,
    selectedBrandID, canEditContent, canEditSettings, apply, initialize, loadAccess, selectBrand, resetAccess, canVisit }
})
