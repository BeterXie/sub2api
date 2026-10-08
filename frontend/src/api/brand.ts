import apiClient from './client'
import type { PublicSettings } from '@/types'

export interface Brand {
  id: number
  code: string
  name: string
  status: 'active' | 'disabled'
  registration_enabled: boolean
  max_concurrent: number
  rpm_limit: number
}
export interface BrandScope {
  brand_id: number
  brand_code: string
  name: string
  domain_id: number
  hostname: string
  canonical_api_origin: string
  registration_enabled: boolean
}
export interface BrandConfig {
  enabled: boolean
  brand?: BrandScope
  settings?: Record<string, unknown>
  public_settings?: PublicSettings
}
export interface BrandDomain {
  id: number
  brand_id: number
  hostname: string
  enabled: boolean
  primary_flag: boolean
  public_enabled: boolean
  gateway_enabled: boolean
  overrides: Record<string, unknown>
  metadata?: Record<string, unknown>
}
export interface BrandPage {
  id: number
  brand_id: number
  slug: string
  locale: string
  status: 'draft' | 'published'
  title: string
  category: string
  sort_order: number
  content_md: string
  revision: number
}
export type BrandAdminRole = '' | 'super_admin' | 'owner' | 'operator' | 'support'

export const brandAPI = {
  config: async () => (await apiClient.get<BrandConfig>('/public/brand-config')).data,
  access: async () => (await apiClient.get<{ role: BrandAdminRole; brand_id: number }>('/user/brand-access')).data,
  list: async () => (await apiClient.get<Brand[]>('/admin/brands')).data,
  save: async (value: Brand) => (await apiClient[value.id ? 'put' : 'post']<Brand>(value.id ? `/admin/brands/${value.id}` : '/admin/brands', value)).data,
  disable: async (id: number) => apiClient.delete(`/admin/brands/${id}`),
  domains: async (id: number) => (await apiClient.get<BrandDomain[]>(`/admin/brands/${id}/domains`)).data,
  saveDomain: async (id: number, value: BrandDomain) => (await apiClient[value.id ? 'put' : 'post']<BrandDomain>(`/admin/brands/${id}/domains${value.id ? '/' + value.id : ''}`, value)).data,
  deleteDomain: async (id: number, domainID: number) => apiClient.delete(`/admin/brands/${id}/domains/${domainID}`),
  grants: async (id: number) => (await apiClient.get<{ user_id: number; role: BrandAdminRole }[]>(`/admin/brands/${id}/admins`)).data,
  saveGrant: async (id: number, user_id: number, role: BrandAdminRole) => apiClient.post(`/admin/brands/${id}/admins`, { user_id, role }),
  deleteGrant: async (id: number, userID: number) => apiClient.delete(`/admin/brands/${id}/admins/${userID}`),
  settings: async () => (await apiClient.get<Record<string, unknown>>('/admin/brand-content/settings')).data,
  saveSettings: async (values: Record<string, unknown>) => apiClient.put('/admin/brand-content/settings', values),
  pages: async (published = false) => (await apiClient.get<BrandPage[]>(published ? '/public/docs' : '/admin/brand-content/pages')).data,
  page: async (slug: string, locale: string) => (await apiClient.get<BrandPage>(`/public/docs/${encodeURIComponent(slug)}`, { params: { locale } })).data,
  savePage: async (page: BrandPage) => (await apiClient[page.id ? 'put' : 'post']<BrandPage>(`/admin/brand-content/pages${page.id ? '/' + page.id : ''}`, page)).data,
  revisions: async (pageID: number) => (await apiClient.get<BrandPage[]>(`/admin/brand-content/pages/${pageID}/revisions`)).data,
  uploadMedia: async (file: File) => {
    const data = new FormData()
    data.append('file', file)
		return (await apiClient.post<{ url: string; asset: string }>('/admin/brand-content/media', data, {
			headers: { 'Content-Type': 'multipart/form-data' },
		})).data
  },
  media: async (asset: string) => (await apiClient.get<Blob>('/admin/brand-content/media/' + encodeURIComponent(asset), { responseType: 'blob' })).data,
  summary: async (allBrands = false) => (await apiClient.get<Record<string, unknown>>('/admin/brand-content/summary', { params: allBrands ? { all_brands: true } : {} })).data
}
