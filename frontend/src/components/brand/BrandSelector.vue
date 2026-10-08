<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { brandAPI, type Brand } from '@/api/brand'
import { useBrandStore } from '@/stores/brand'
import { useI18n } from 'vue-i18n'
const brand = useBrandStore()
const { t } = useI18n()
const brands = ref<Brand[]>([])
onMounted(async () => { if (brand.isPlatformAdmin) brands.value = await brandAPI.list() })
function select(event: Event) {
  const id = Number((event.target as HTMLSelectElement).value)
  brand.selectBrand(id || null)
  // Remount existing data views so their filters, selections and requests cannot
  // retain records from the previously selected tenant.
  window.location.reload()
}
</script>
<template>
  <label v-if="brand.enabled && brand.isPlatformAdmin" class="flex items-center gap-2 text-sm">
    <span>{{ t('brand.scope') }}</span>
    <select class="input w-auto" :value="brand.selectedBrandID || 1" @change="select">
      <option v-for="item in brands" :key="item.id" :value="item.id">{{ item.name }}</option>
    </select>
  </label>
</template>
