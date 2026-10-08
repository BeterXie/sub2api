<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { brandAPI, type BrandPage } from '@/api/brand'
import { useBrandStore } from '@/stores/brand'
import { useAppStore } from '@/stores/app'
import { renderBrandMarkdownPreview } from '@/utils/brandMarkdown'
const { t } = useI18n()
const brand = useBrandStore()
const app = useAppStore()
const settings = reactive<Record<string, string>>({})
const contentOrigin = ref(brand.apiOrigin)
const advanced = ref('{}')
const pages = ref<BrandPage[]>([])
const history = ref<BrandPage[]>([])
const summary = ref<Record<string, unknown>>({})
const allBrands = ref(false)
const saving = ref(false)
const selectedPage = ref(false)
const pageSession = ref(0)
const pageEditVersion = ref(0)
const mediaURLs = reactive<Record<string, string>>({})
const page = reactive<BrandPage>({ id: 0, brand_id: 0, slug: '', locale: 'zh', status: 'draft', title: '', category: '', sort_order: 0, content_md: '', revision: 0 })
const preview = computed(() => renderBrandMarkdownPreview(page.content_md, contentOrigin.value, mediaURLs))
const fields: Record<string, string> = { site_name: 'siteName', site_logo: 'logo', home_template: 'homeTemplate', home_headline: 'headline', home_description: 'description', seo_title: 'seoTitle', seo_description: 'seoDescription', theme_color: 'themeColor', footer_text: 'footer', brand_nav: 'nav', brand_footer: 'footerLinks', home_sections: 'sections' }
const smtpFields: Record<string, string> = { smtp_host: 'smtpHost', smtp_port: 'smtpPort', smtp_username: 'smtpUsername', smtp_password: 'smtpPassword', smtp_from: 'smtpFrom', smtp_from_name: 'smtpFromName', smtp_use_tls: 'smtpTLS' }
async function load() {
  const [values, documents, stats] = await Promise.all([brandAPI.settings(), brandAPI.pages(), brandAPI.summary(allBrands.value && brand.isPlatformAdmin)])
  contentOrigin.value = String(values.api_base_url || brand.apiOrigin)
  for (const key of [...Object.keys(fields), ...Object.keys(smtpFields)]) settings[key] = typeof values[key] === 'string' ? values[key] as string : values[key] === undefined ? '' : JSON.stringify(values[key])
  pages.value = documents; summary.value = stats
  advanced.value = JSON.stringify(Object.fromEntries(Object.entries(values).filter(([key]) => !(key in fields) && !(key in smtpFields) && !key.endsWith('_configured') && !['registration_enabled', 'api_base_url', 'frontend_url'].includes(key))), null, 2)
}
async function perform(action: () => Promise<unknown>) {
  saving.value = true
  try { await action(); await load(); app.showSuccess(t('brand.saved')) }
  catch (error) { app.showError((error as { message?: string }).message || t('brand.failed')) }
  finally { saving.value = false }
}
function editPage(value?: BrandPage) {
  pageSession.value += 1
  Object.assign(page, value || { id: 0, brand_id: 0, slug: '', locale: 'zh', status: 'draft', title: '', category: '', sort_order: 0, content_md: '', revision: 0 })
  selectedPage.value = true; history.value = []
}
async function savePage(status: 'draft' | 'published') {
  const session = pageSession.value
  const editVersion = pageEditVersion.value
  const request = { ...page, status }
  await perform(async () => {
    const saved = await brandAPI.savePage(request)
    if (pageSession.value !== session || page.id !== request.id) return
    if (pageEditVersion.value === editVersion) {
      Object.assign(page, saved)
      return
    }
    page.id = saved.id
    page.brand_id = saved.brand_id
    page.status = saved.status
    page.revision = saved.revision
  })
}
async function saveSettings() {
  await perform(async () => {
    const values = { ...JSON.parse(advanced.value) }
    for (const key of [...Object.keys(fields), ...Object.keys(smtpFields)]) if (settings[key] !== undefined) values[key] = settings[key]
    await brandAPI.saveSettings(values)
  })
}
async function revisions() {
  const pageID = page.id
  const session = pageSession.value
  try {
    const revisions = await brandAPI.revisions(pageID)
    if (pageSession.value === session && page.id === pageID) history.value = revisions
  } catch { app.showError(t('brand.failed')) }
}
function restore(value: BrandPage) { page.title = value.title; page.content_md = value.content_md; page.category = value.category; page.sort_order = value.sort_order; page.status = 'draft' }
async function upload(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  await perform(async () => {
    const media = await brandAPI.uploadMedia(file)
    page.content_md += '\n\n![](' + media.url + ')\n'
  })
  input.value = ''
}
watch(() => page.content_md, async (value, _previous, onCleanup) => {
  let active = true
  onCleanup(() => { active = false })
  const assets = [...new Set(value.match(/[a-f0-9]{32}\.(?:png|jpg|gif|webp)/g) || [])]
  for (const asset of assets) if (!mediaURLs[asset]) {
    try { const blob = await brandAPI.media(asset); if (active) mediaURLs[asset] = URL.createObjectURL(blob) } catch { /* Unavailable media stays visible as a broken image. */ }
  }
})
watch(
  () => [page.title, page.slug, page.locale, page.category, page.sort_order, page.content_md],
  () => { pageEditVersion.value += 1 },
  { flush: 'sync' },
)
onUnmounted(() => { for (const url of Object.values(mediaURLs)) URL.revokeObjectURL(url) })
onMounted(() => load().catch(() => app.showError(t('brand.failed'))))
watch(allBrands, async value => {
  try { summary.value = await brandAPI.summary(value && brand.isPlatformAdmin) } catch { app.showError(t('brand.failed')) }
})
</script>
<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-8">
      <h1 class="text-2xl font-semibold">{{ t('brand.content') }}</h1>
      <p v-if="!brand.canEditContent" class="text-sm text-gray-500">{{ t('brand.readonly') }}</p>
      <section class="card space-y-3 p-6"><div class="flex flex-wrap justify-between gap-3"><h2 class="text-lg font-semibold">{{ t('brand.summary') }}</h2><label v-if="brand.isPlatformAdmin" class="flex items-center gap-2"><input v-model="allBrands" type="checkbox" />{{ t('brand.allBrands') }}</label></div><div class="flex flex-wrap gap-8"><p>{{ t('brand.walletBalance') }}: {{ summary.balance_usd }}</p><p>{{ t('brand.usageCharges') }}: {{ summary.usage_charges_usd }}</p><p>{{ t('brand.accountCost') }}: {{ summary.account_cost_usd }}</p><p>{{ t('brand.usageMargin') }}: {{ summary.usage_margin_usd }}</p></div><p>{{ t('brand.revenue') }}</p><pre class="overflow-auto text-sm">{{ JSON.stringify(summary.payments_by_currency || [], null, 2) }}</pre></section>
      <form v-if="brand.canEditSettings" class="card grid gap-5 p-6 md:grid-cols-2" @submit.prevent="saveSettings">
        <h2 class="text-lg font-semibold md:col-span-2">{{ t('brand.siteSettings') }}</h2>
        <label v-for="(label, key) in fields" :key="key">{{ t('brand.' + label) }}<textarea v-if="['brand_nav', 'brand_footer', 'home_sections'].includes(String(key))" v-model="settings[key]" class="input mt-2 font-mono text-sm" rows="3"></textarea><input v-else v-model="settings[key]" class="input mt-2" /></label>
        <h3 class="font-semibold md:col-span-2">{{ t('brand.smtpSettings') }}</h3>
        <p class="text-sm text-gray-500 md:col-span-2">{{ t('brand.smtpHint') }}</p>
        <label v-for="(label, key) in smtpFields" :key="key">{{ t('brand.' + label) }}<select v-if="key === 'smtp_use_tls'" v-model="settings[key]" class="input mt-2"><option value="false">STARTTLS</option><option value="true">TLS</option></select><input v-else v-model="settings[key]" class="input mt-2" :type="key === 'smtp_password' ? 'password' : 'text'" :autocomplete="key === 'smtp_password' ? 'new-password' : 'off'" /></label>
        <label class="md:col-span-2">{{ t('brand.advanced') }}<textarea v-model="advanced" class="input mt-2 font-mono text-sm" rows="10"></textarea></label>
        <p class="text-sm text-gray-500 md:col-span-2">{{ t('brand.secretHint') }}</p>
        <button class="btn btn-primary w-fit" :disabled="saving">{{ t('brand.save') }}</button>
      </form>
      <section class="card space-y-5 p-6">
        <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="text-lg font-semibold">{{ t('brand.pages') }}</h2><button v-if="brand.canEditContent" class="btn btn-secondary" @click="editPage()">{{ t('brand.newPage') }}</button></div>
        <p class="text-sm text-gray-500">{{ t('brand.homeHint') }}</p>
        <p v-if="!pages.length">{{ t('brand.noPages') }}</p>
        <button v-for="item in pages" :key="item.id" :data-testid="'edit-page-' + item.id" class="flex w-full flex-wrap justify-between gap-4 border-b border-gray-200 py-3 text-left dark:border-dark-700" @click="editPage(item)"><span>{{ item.title }} · {{ item.locale }}</span><span class="text-gray-500">/{{ item.slug }} · {{ t('brand.' + item.status) }} · {{ item.revision }}</span></button>
      </section>
      <form v-if="selectedPage" class="card space-y-5 p-6" @submit.prevent="savePage('draft')">
        <div class="grid gap-5 sm:grid-cols-3">
          <label>{{ t('brand.title') }}<input v-model="page.title" data-testid="page-title" class="input mt-2" required :disabled="!brand.canEditContent" /></label>
          <label>{{ t('brand.slug') }}<input v-model="page.slug" class="input mt-2" pattern="[a-z0-9][a-z0-9_-]*" required :disabled="!brand.canEditContent" /></label>
          <label>{{ t('brand.language') }}<select v-model="page.locale" class="input mt-2" :disabled="!brand.canEditContent"><option value="zh">中文</option><option value="en">English</option></select></label>
          <label>{{ t('brand.category') }}<input v-model="page.category" class="input mt-2" :disabled="!brand.canEditContent" :placeholder="t('brand.categoryHint')" /></label>
          <label>{{ t('brand.sortOrder') }}<input v-model.number="page.sort_order" type="number" class="input mt-2" :disabled="!brand.canEditContent" /></label>
        </div>
        <p class="text-sm text-gray-500">{{ t('brand.apiPlaceholderHint') }}</p>
        <label v-if="brand.canEditContent" class="block text-sm">{{ t('brand.uploadMedia') }}<input class="mt-2 block" type="file" accept="image/png,image/jpeg,image/gif,image/webp" :disabled="saving" @change="upload" /></label>
        <div class="grid gap-6 lg:grid-cols-2">
          <label>{{ t('brand.markdown') }}<textarea v-model="page.content_md" data-testid="page-content" class="input mt-2 min-h-96 font-mono text-sm" :disabled="!brand.canEditContent"></textarea></label>
          <div><h3 class="mb-3 font-medium">{{ t('brand.preview') }}</h3><div class="brand-markdown max-h-[40rem] overflow-auto" v-html="preview"></div></div>
        </div>
        <div class="flex flex-wrap gap-3">
          <template v-if="brand.canEditContent"><button class="btn btn-secondary" :disabled="saving">{{ t('brand.save') }}</button><button type="button" class="btn btn-primary" :disabled="saving || !page.title || !page.slug" @click="savePage('published')">{{ t('brand.publish') }}</button><button v-if="page.status === 'published'" type="button" class="btn btn-secondary" :disabled="saving" @click="savePage('draft')">{{ t('brand.withdraw') }}</button></template>
          <button v-if="page.id" type="button" data-testid="page-revisions" class="btn btn-secondary" @click="revisions">{{ t('brand.revisions') }}</button>
        </div>
        <div v-for="version in history" :key="version.revision" class="flex justify-between border-t border-gray-200 py-3 dark:border-dark-700"><span>{{ version.revision }} · {{ version.title }} · {{ t('brand.' + version.status) }}</span><button v-if="brand.canEditContent" type="button" class="btn btn-secondary" @click="restore(version)">{{ t('brand.restore') }}</button></div>
      </form>
    </div>
  </AppLayout>
</template>
