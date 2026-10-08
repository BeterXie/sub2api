<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { englishThemes } from '@/brands/themes'
import { renderBrandMarkdown } from '@/utils/brandMarkdown'
import { useBrandStore } from '@/stores/brand'
import { useAppStore } from '@/stores/app'
import { useI18n } from 'vue-i18n'
import { brandAPI } from '@/api/brand'
import BrandNavbar from './BrandNavbar.vue'
import BrandFooter from './BrandFooter.vue'
defineProps<{ variant: 'studio' | 'application' | 'code' | 'workspace' }>()
const brand = useBrandStore()
const app = useAppStore()
const { t, locale } = useI18n()
const defaultCopy = computed(() => locale.value.startsWith('zh') ? brand.theme : (englishThemes[brand.code] || englishThemes.llmp))
const headline = computed(() => brand.settings.home_headline || defaultCopy.value.headline)
const description = computed(() => brand.settings.home_description || defaultCopy.value.description)
const publishedHome = ref('')
const sections = computed(() => {
  try {
    const raw = brand.settings.home_sections
    const list = typeof raw === 'string' ? JSON.parse(raw) : raw
    return Array.isArray(list) ? list.filter(v => typeof v?.title === 'string' && typeof v?.body === 'string') : []
  } catch { return [] }
})
const example = computed(() => `curl ${brand.apiOrigin}/v1/models \\\n  -H "Authorization: Bearer $API_KEY"`)
const homeHTML = computed(() => renderBrandMarkdown(publishedHome.value, brand.apiOrigin))
watch(locale, async () => {
  publishedHome.value = ''
  const requested = locale.value
  try {
    const content = (await brandAPI.page('home', requested.startsWith('zh') ? 'zh' : 'en')).content_md
    if (requested === locale.value) publishedHome.value = content
  } catch { /* Optional CMS home. */ }
}, { immediate: true })
</script>
<template>
  <div class="brand-site flex min-h-[100dvh] flex-col bg-white text-gray-900 dark:bg-dark-950 dark:text-gray-100" :class="'brand-home-' + variant">
    <BrandNavbar />
    <main class="mx-auto w-full max-w-6xl px-5">
      <section class="brand-hero">
        <div class="brand-hero-copy">
          <h1 class="text-4xl font-semibold leading-tight tracking-tight sm:text-6xl">{{ headline }}</h1>
          <p class="mt-6 max-w-lg text-lg leading-relaxed text-gray-600 dark:text-gray-400">{{ description }}</p>
          <div class="mt-8 flex flex-wrap gap-3">
            <router-link :to="brand.scope?.registration_enabled ? '/register' : '/login'" class="brand-button">{{ t('brand.getStarted') }}</router-link>
            <router-link to="/docs" class="btn btn-secondary">{{ t('brand.integrationGuide') }}</router-link>
          </div>
        </div>
        <div class="brand-example overflow-hidden rounded-xl border border-gray-200 bg-gray-50 p-6 dark:border-dark-700 dark:bg-dark-900">
          <p class="mb-4 text-sm font-medium">{{ t('brand.apiEndpoint') }}</p>
          <p class="break-all font-mono text-sm" style="color: var(--brand-accent)">{{ brand.apiOrigin }}</p>
          <pre class="mt-6 overflow-x-auto text-sm leading-7"><code>{{ example }}</code></pre>
          <p class="mt-4 text-xs leading-6 text-gray-500">{{ t('brand.keyHint') }}</p>
        </div>
      </section>
      <article v-if="homeHTML" class="brand-markdown mb-16" v-html="homeHTML"></article>
      <section v-if="sections.length" class="mb-16 space-y-10">
        <div v-for="(section, index) in sections" :key="index" class="grid gap-5 border-t border-gray-200 pt-8 md:grid-cols-[1fr_2fr] dark:border-dark-800">
          <h2 class="text-2xl font-semibold">{{ section.title }}</h2>
          <p class="max-w-xl leading-8 text-gray-600 dark:text-gray-400">{{ section.body }}</p>
        </div>
      </section>
      <section v-if="app.cachedPublicSettings?.model_plaza_enabled" class="mb-16 flex flex-wrap items-center justify-between gap-5 border-t border-gray-200 pt-8 dark:border-dark-800">
        <h2 class="text-2xl font-semibold">{{ t('brand.exploreModels') }}</h2>
        <router-link to="/model-plaza" class="btn btn-secondary">{{ t('nav.modelPlaza') }}</router-link>
      </section>
    </main>
    <BrandFooter />
  </div>
</template>
<style scoped>
.brand-hero { display: grid; gap: 3rem; align-items: center; padding: 5rem 0; grid-template-columns: 1.1fr 1fr; min-height: 65dvh; }
.brand-home-studio .brand-hero { grid-template-columns: 1.35fr .9fr; }
.brand-home-studio h1 { font-family: Georgia, 'Songti SC', serif; font-weight: 500; }
.brand-home-code .brand-hero { grid-template-columns: .95fr 1.2fr; }
.brand-home-code h1 { font-family: ui-monospace, Consolas, monospace; }
.brand-home-workspace .brand-hero { grid-template-columns: 1fr; max-width: 52rem; margin: auto; text-align: center; padding-top: 3rem; }
.brand-home-workspace .brand-hero-copy p { margin-left: auto; margin-right: auto; }
.brand-home-workspace .brand-hero-copy > div { justify-content: center; }
.brand-home-workspace .brand-example { text-align: left; }
@media (max-width: 768px) { .brand-hero, .brand-home-studio .brand-hero, .brand-home-code .brand-hero { grid-template-columns: 1fr; padding: 3rem 0; } }
</style>
