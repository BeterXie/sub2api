<script setup lang="ts">
import { computed } from 'vue'
import { useBrandStore } from '@/stores/brand'
import { useAppStore } from '@/stores/app'
const brand = useBrandStore()
const app = useAppStore()
const links = computed(() => {
  try {
    const raw = brand.settings.brand_footer
    const list = typeof raw === 'string' ? JSON.parse(raw) : raw
    return Array.isArray(list) ? list.filter(v => typeof v?.label === 'string' && /^\/[^/]/.test(v?.url)) : []
  } catch { return [] }
})
</script>
<template>
  <footer class="mt-auto border-t border-gray-200 px-5 py-7 text-sm text-gray-600 dark:border-dark-800 dark:text-gray-400">
    <div class="mx-auto flex max-w-6xl flex-wrap justify-between gap-4">
      <p>{{ brand.settings.footer_text || brand.name }}</p>
      <div class="flex flex-wrap gap-5">
        <router-link v-for="link in links" :key="link.url" :to="link.url">{{ link.label }}</router-link>
        <span v-if="app.contactInfo">{{ app.contactInfo }}</span>
      </div>
    </div>
  </footer>
</template>
