import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import BrandContentView from '../BrandContentView.vue'
import { brandAPI, type BrandPage } from '@/api/brand'

const appStore = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))
const brandStore = vi.hoisted(() => ({
  apiOrigin: 'https://brand.example',
  isPlatformAdmin: false,
  canEditContent: true,
  canEditSettings: false,
}))

vi.mock('@/api/brand', () => ({
  brandAPI: {
    settings: vi.fn(),
    pages: vi.fn(),
    summary: vi.fn(),
    savePage: vi.fn(),
    revisions: vi.fn(),
    media: vi.fn(),
    uploadMedia: vi.fn(),
  },
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))
vi.mock('@/stores/brand', () => ({ useBrandStore: () => brandStore }))
vi.mock('vue-i18n', async importOriginal => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

const page = (id: number, title: string, content = `${title} content`): BrandPage => ({
  id,
  brand_id: 1,
  slug: title.toLowerCase(),
  locale: 'zh',
  status: 'draft',
  title,
  category: '',
  sort_order: id,
  content_md: content,
  revision: 1,
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

function mountView() {
  return mount(BrandContentView, {
    global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } },
  })
}

describe('BrandContentView editing sessions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(brandAPI.settings).mockResolvedValue({ api_base_url: 'https://brand.example' })
    vi.mocked(brandAPI.pages).mockResolvedValue([page(1, 'Alpha'), page(2, 'Beta')])
    vi.mocked(brandAPI.summary).mockResolvedValue({})
    vi.mocked(brandAPI.revisions).mockResolvedValue([])
    vi.mocked(brandAPI.media).mockResolvedValue(new Blob(['image']))
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:https://brand.example/preview') })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
  })

  it('does not overwrite later edits with an older save response', async () => {
    const save = deferred<BrandPage>()
    vi.mocked(brandAPI.savePage).mockReturnValue(save.promise)
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="edit-page-1"]').trigger('click')
    await wrapper.get('[data-testid="page-content"]').setValue('submitted content')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('[data-testid="page-content"]').setValue('newer unsaved content')
    save.resolve({ ...page(1, 'Alpha', 'submitted content'), revision: 2 })
    await flushPromises()
    expect((wrapper.get('[data-testid="page-content"]').element as HTMLTextAreaElement).value).toBe('newer unsaved content')
  })

  it('does not apply a save response after switching pages', async () => {
    const save = deferred<BrandPage>()
    vi.mocked(brandAPI.savePage).mockReturnValue(save.promise)
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="edit-page-1"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('[data-testid="edit-page-2"]').trigger('click')
    save.resolve({ ...page(1, 'Saved Alpha', 'saved alpha content'), revision: 2 })
    await flushPromises()
    expect((wrapper.get('[data-testid="page-title"]').element as HTMLInputElement).value).toBe('Beta')
    expect((wrapper.get('[data-testid="page-content"]').element as HTMLTextAreaElement).value).toBe('Beta content')
  })

  it('ignores revision history returned for a previous editing session', async () => {
    const revisions = deferred<BrandPage[]>()
    vi.mocked(brandAPI.revisions).mockReturnValue(revisions.promise)
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="edit-page-1"]').trigger('click')
    await wrapper.get('[data-testid="page-revisions"]').trigger('click')
    await wrapper.get('[data-testid="edit-page-2"]').trigger('click')
    revisions.resolve([{ ...page(1, 'Old Alpha'), revision: 9 }])
    await flushPromises()
    expect(wrapper.text()).not.toContain('Old Alpha')
  })

  it('renders fetched brand media through a post-sanitization blob URL', async () => {
    const asset = '0123456789abcdef0123456789abcdef.png'
    vi.mocked(brandAPI.pages).mockResolvedValue([page(1, 'Alpha', `![image](/api/v1/public/brand-media/${asset})`)])
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="edit-page-1"]').trigger('click')
    await flushPromises()
    expect(brandAPI.media).toHaveBeenCalledWith(asset)
    expect(wrapper.get('.brand-markdown img').attributes('src')).toBe('blob:https://brand.example/preview')
  })
})
