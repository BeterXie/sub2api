<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { brandAPI, type BrandPage } from '@/api/brand'
import { useBrandStore } from '@/stores/brand'
import { renderBrandMarkdown } from '@/utils/brandMarkdown'
import BrandNavbar from '@/components/brand/BrandNavbar.vue'
import BrandFooter from '@/components/brand/BrandFooter.vue'
const route = useRoute()
const brand = useBrandStore()
const { t, locale } = useI18n()
const pages = ref<BrandPage[]>([])
const page = ref<BrandPage | null>(null)
const loading = ref(false)
const error = ref('')
const language = computed(() => String(route.query.locale || (locale.value.startsWith('zh') ? 'zh' : 'en')))
const visiblePages = computed(() => pages.value.filter(v => v.locale === language.value && v.slug !== 'home'))
const categories = computed(() => [...new Set(visiblePages.value.map(value => value.category || ''))])
const html = computed(() => renderBrandMarkdown(page.value?.content_md || '', brand.apiOrigin))
let generation = 0
async function load() {
  const version = ++generation
  loading.value = true; error.value = ''; page.value = null
  try {
    const list = await brandAPI.pages(true)
    const slug = String(route.params.slug || '')
    const document = slug ? await brandAPI.page(slug, language.value) : null
    if (version !== generation) return
    pages.value = list; page.value = document
    if (document) window.document.title = document.title + ' · ' + brand.name
  } catch { if (version === generation) error.value = t('brand.docUnavailable') }
  finally { if (version === generation) loading.value = false }
}
watch(() => [route.params.slug, language.value], load, { immediate: true })
</script>
<template>
  <div class="brand-site flex min-h-[100dvh] flex-col bg-white text-gray-900 dark:bg-dark-950 dark:text-gray-100">
    <BrandNavbar />
    <div class="mx-auto grid w-full max-w-6xl flex-1 gap-10 px-5 py-10 md:grid-cols-[14rem_1fr]">
      <aside>
        <h2 class="mb-5 font-semibold">{{ t('brand.docs') }}</h2>
        <nav class="space-y-2">
          <section v-for="category in categories" :key="category" class="space-y-1">
            <h3 v-if="category" class="pb-1 pt-4 text-xs font-medium text-gray-500">{{ category.split('/').join(' / ') }}</h3>
            <router-link v-for="item in visiblePages.filter(value => (value.category || '') === category)" :key="item.id" :to="{ path: '/docs/' + item.slug, query: { locale: item.locale } }" class="block rounded-md px-3 py-2 text-sm hover:bg-gray-100 dark:hover:bg-dark-800" :class="{ 'bg-gray-100 dark:bg-dark-800': item.slug === page?.slug }">{{ item.title }}</router-link>
          </section>
        </nav>
      </aside>
      <main class="min-w-0">
        <p v-if="loading" role="status">{{ t('brand.loading') }}</p>
        <div v-else-if="error" role="alert"><p>{{ error }}</p><button class="btn btn-secondary mt-4" @click="load">{{ t('brand.retry') }}</button></div>
        <article v-else-if="page">
          <h1 class="mb-8 text-3xl font-semibold">{{ page.title }}</h1>
          <div class="brand-markdown" v-html="html"></div>
        </article>
        <div v-else>
          <h1 class="mb-6 text-3xl font-semibold">{{ t('brand.integrationGuide') }}</h1>
          <p v-if="!visiblePages.length" class="text-gray-500">{{ t('brand.noPublishedDocs') }}</p>
          <router-link v-for="item in visiblePages" :key="item.id" :to="{ path: '/docs/' + item.slug, query: { locale: item.locale } }" class="mb-3 block text-lg underline underline-offset-4">{{ item.title }}</router-link>
        </div>
      </main>
    </div>
    <BrandFooter />
  </div>
</template>
