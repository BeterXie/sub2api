<script setup lang="ts">
import { computed } from 'vue'
import BrandLogo from './BrandLogo.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import { useBrandStore } from '@/stores/brand'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useI18n } from 'vue-i18n'
const brand = useBrandStore()
const auth = useAuthStore()
const app = useAppStore()
const { t } = useI18n()
const links = computed(() => {
  const raw = brand.settings.brand_nav
  try {
    const value = typeof raw === 'string' ? JSON.parse(raw) : raw
    return Array.isArray(value) ? value.filter(v => typeof v?.label === 'string' && /^\/[^/]/.test(v?.url)) : []
  } catch { return [] }
})
</script>
<template>
  <header class="border-b border-gray-200 dark:border-dark-800">
    <nav class="mx-auto flex min-h-16 max-w-6xl flex-wrap items-center justify-between gap-4 px-5 py-3">
      <BrandLogo />
      <div class="flex flex-wrap items-center gap-5 text-sm">
        <LocaleSwitcher />
        <router-link to="/docs">{{ t('brand.docs') }}</router-link>
        <router-link v-if="app.cachedPublicSettings?.model_plaza_enabled" to="/model-plaza">{{ t('nav.modelPlaza') }}</router-link>
        <router-link v-for="link in links" :key="link.url" :to="link.url">{{ link.label }}</router-link>
        <router-link :to="auth.isAuthenticated ? (auth.isAdmin ? '/admin/dashboard' : '/dashboard') : '/login'" class="brand-button">
          {{ auth.isAuthenticated ? t('home.dashboard') : t('home.login') }}
        </router-link>
      </div>
    </nav>
  </header>
</template>
