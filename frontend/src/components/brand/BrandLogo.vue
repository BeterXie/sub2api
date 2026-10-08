<script setup lang="ts">
import { useBrandStore } from '@/stores/brand'
import { useAppStore } from '@/stores/app'
const brand = useBrandStore()
const app = useAppStore()
</script>
<template>
  <router-link to="/home" class="inline-flex items-center gap-3 font-semibold tracking-tight" :aria-label="brand.name">
    <img v-if="app.siteLogo" :src="app.siteLogo" :alt="brand.name" class="h-8 w-8 object-contain" />
    <span>{{ brand.name }}</span>
  </router-link>
</template>
