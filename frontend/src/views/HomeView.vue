<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useBrandStore } from '@/stores/brand'
import LLMPHome from '@/brands/llmp/Home.vue'
const brand = useBrandStore()
const homes = {
	llmp: LLMPHome,
  mues: defineAsyncComponent(() => import('@/brands/mues/Home.vue')),
  aisi: defineAsyncComponent(() => import('@/brands/aisi/Home.vue')),
  opensi_codes: defineAsyncComponent(() => import('@/brands/opensi-codes/Home.vue')),
  opensi_in: defineAsyncComponent(() => import('@/brands/opensi-in/Home.vue'))
}
const home = computed(() => brand.enabled ? homes[String(brand.settings.home_template || brand.code) as keyof typeof homes] || homes[brand.code as keyof typeof homes] || LLMPHome : LLMPHome)
</script>
<template><component :is="home" /></template>
