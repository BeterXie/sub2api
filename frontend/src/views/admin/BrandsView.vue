<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { brandAPI, type Brand, type BrandDomain, type BrandAdminRole } from '@/api/brand'
import { useAppStore } from '@/stores/app'
const { t } = useI18n()
const app = useAppStore()
const brands = ref<Brand[]>([])
const domains = ref<BrandDomain[]>([])
const grants = ref<{ user_id: number; role: BrandAdminRole }[]>([])
const saving = ref(false)
const selected = ref(1)
const edit = reactive<Brand>({ id: 0, code: '', name: '', status: 'disabled', registration_enabled: false, max_concurrent: 0, rpm_limit: 0 })
const domain = reactive<BrandDomain>({ id: 0, brand_id: 0, hostname: '', enabled: false, primary_flag: false, public_enabled: true, gateway_enabled: true, overrides: {}, metadata: {} })
const overrides = ref('{}')
const metadata = ref('{}')
const grantUserID = ref(0)
const grantRole = ref<BrandAdminRole>('operator')
let loadGeneration = 0

function emptyDomain(brandID: number): BrandDomain {
	return { id: 0, brand_id: brandID, hostname: '', enabled: false, primary_flag: false, public_enabled: true, gateway_enabled: true, overrides: {}, metadata: {} }
}

function resetDomainEditor(brandID: number) {
	Object.assign(domain, emptyDomain(brandID))
	overrides.value = '{}'
	metadata.value = '{}'
}

async function load() {
	const generation = ++loadGeneration
	let selectedID = Number(selected.value)
	if (!Number.isFinite(selectedID) || selectedID <= 0) selectedID = 1
	const cached = brands.value.find(value => value.id === selectedID)
	if (cached) Object.assign(edit, cached)
	domains.value = []
	grants.value = []
	resetDomainEditor(selectedID)

	const nextBrands = await brandAPI.list()
	if (!selected.value && generation === loadGeneration) selected.value = selectedID
	let value = nextBrands.find(item => item.id === selectedID)
	if (!value && nextBrands.length > 0 && generation === loadGeneration && Number(selected.value || selectedID) === selectedID) {
		selectedID = nextBrands[0].id
		selected.value = selectedID
		value = nextBrands[0]
		resetDomainEditor(selectedID)
	}
	if (!value) {
		if (generation === loadGeneration && Number(selected.value) === selectedID) brands.value = nextBrands
		return
	}
	const [nextDomains, nextGrants] = await Promise.all([brandAPI.domains(selectedID), brandAPI.grants(selectedID)])
	if (generation !== loadGeneration || Number(selected.value || selectedID) !== selectedID) return
	brands.value = nextBrands
	Object.assign(edit, value)
	domains.value = nextDomains
	grants.value = nextGrants
}
async function perform(action: () => Promise<unknown>) {
  saving.value = true
  try { await action(); await load(); app.showSuccess(t('brand.saved')) }
  catch (error) { app.showError((error as { message?: string }).message || t('brand.failed')) }
  finally { saving.value = false }
}
function addBrand() {
	loadGeneration++
	Object.assign(edit, { id: 0, code: '', name: '', status: 'disabled', registration_enabled: false, max_concurrent: 0, rpm_limit: 0 })
	domains.value = []
	grants.value = []
	resetDomainEditor(0)
}
function editDomain(value?: BrandDomain) {
	Object.assign(domain, emptyDomain(edit.id), value || {})
	domain.overrides = value?.overrides || {}
	domain.metadata = value?.metadata || {}
  overrides.value = JSON.stringify(domain.overrides || {}, null, 2)
  metadata.value = JSON.stringify(domain.metadata || {}, null, 2)
}
async function saveBrand() {
  await perform(async () => { const saved = await brandAPI.save({ ...edit }); selected.value = saved.id })
}
async function saveDomain() {
  await perform(async () => { await brandAPI.saveDomain(edit.id, { ...domain, overrides: JSON.parse(overrides.value), metadata: JSON.parse(metadata.value) }); editDomain() })
}
onMounted(() => load().catch(() => app.showError(t('brand.failed'))))
</script>
<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-8">
      <div class="flex flex-wrap items-center justify-between gap-4"><h1 class="text-2xl font-semibold">{{ t('brand.management') }}</h1><button class="btn btn-secondary" @click="addBrand">{{ t('brand.addBrand') }}</button></div>
			<label class="block max-w-sm">{{ t('brand.scope') }}<select v-model.number="selected" data-testid="brand-scope" class="input mt-2" @change="load"><option v-for="item in brands" :key="item.id" :value="item.id">{{ item.name }} · {{ item.code }}</option></select></label>
      <form class="card grid gap-5 p-6 md:grid-cols-2" @submit.prevent="saveBrand">
        <label>{{ t('brand.name') }}<input v-model="edit.name" class="input mt-2" required maxlength="100" /></label>
        <label>{{ t('brand.code') }}<input v-model="edit.code" class="input mt-2" required pattern="[a-z0-9][a-z0-9_-]*" :disabled="edit.id > 0" maxlength="40" /></label>
        <label>{{ t('brand.concurrent') }}<input v-model.number="edit.max_concurrent" class="input mt-2" type="number" min="0" /></label>
        <label>{{ t('brand.rpm') }}<input v-model.number="edit.rpm_limit" class="input mt-2" type="number" min="0" /></label>
        <p class="text-sm text-gray-500 md:col-span-2">{{ t('brand.unlimitedHint') }}</p>
        <label>{{ t('brand.enabled') }}<select v-model="edit.status" class="input mt-2"><option value="disabled">{{ t('brand.disabled') }}</option><option value="active">{{ t('brand.active') }}</option></select></label>
        <label class="flex items-center gap-2"><input v-model="edit.registration_enabled" type="checkbox" />{{ t('brand.registration') }}</label>
        <div><button class="btn btn-primary" :disabled="saving">{{ t('brand.save') }}</button></div>
      </form>
      <section v-if="edit.id" class="card space-y-5 p-6">
        <div class="flex justify-between"><h2 class="text-lg font-semibold">{{ t('brand.domains') }}</h2><button class="btn btn-secondary" @click="editDomain()">{{ t('brand.addDomain') }}</button></div>
        <div v-for="item in domains" :key="item.id" class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-200 py-3 dark:border-dark-700">
          <span>{{ item.hostname }} <span class="text-sm text-gray-500">{{ item.primary_flag ? t('brand.primary') : '' }} · {{ item.enabled ? t('brand.active') : t('brand.disabled') }}</span></span>
          <div class="flex gap-3"><button class="btn btn-secondary" @click="editDomain(item)">{{ t('brand.save') }}</button><button v-if="!item.primary_flag" class="btn btn-secondary" :disabled="saving" @click="perform(() => brandAPI.deleteDomain(edit.id, item.id))">{{ t('brand.delete') }}</button></div>
        </div>
        <form class="grid gap-4 md:grid-cols-2" @submit.prevent="saveDomain">
					<label class="md:col-span-2">{{ t('brand.hostname') }}<input v-model="domain.hostname" data-testid="domain-hostname" class="input mt-2" required placeholder="example.com" /></label>
          <label v-for="field in (['enabled', 'primary_flag', 'public_enabled', 'gateway_enabled'] as const)" :key="field" class="flex items-center gap-2"><input v-model="domain[field]" type="checkbox" />{{ t('brand.' + ({ enabled: 'enabled', primary_flag: 'primary', public_enabled: 'publicSite', gateway_enabled: 'gateway' }[field])) }}</label>
          <label class="md:col-span-2">{{ t('brand.overrides') }}<textarea v-model="overrides" class="input mt-2 font-mono text-sm" rows="5"></textarea></label>
          <label class="md:col-span-2">{{ t('brand.metadata') }}<textarea v-model="metadata" class="input mt-2 font-mono text-sm" rows="3" placeholder='{"dns_status":"pending","certificate_status":"pending","notes":""}'></textarea></label>
          <button class="btn btn-primary w-fit" :disabled="saving">{{ t('brand.save') }}</button>
        </form>
      </section>
      <section v-if="edit.id" class="card space-y-5 p-6">
        <h2 class="text-lg font-semibold">{{ t('brand.admins') }}</h2>
        <div v-for="item in grants" :key="item.user_id" class="flex items-center justify-between"><span>{{ item.user_id }} · {{ t('brand.' + item.role) }}</span><button class="btn btn-secondary" :disabled="saving" @click="perform(() => brandAPI.deleteGrant(edit.id, item.user_id))">{{ t('brand.delete') }}</button></div>
        <form class="flex flex-wrap items-end gap-3" @submit.prevent="perform(() => brandAPI.saveGrant(edit.id, grantUserID, grantRole))">
          <label>{{ t('brand.userID') }}<input v-model.number="grantUserID" class="input mt-2" type="number" min="1" required /></label>
          <label>{{ t('brand.role') }}<select v-model="grantRole" class="input mt-2"><option v-for="value in ['owner', 'operator', 'support']" :key="value" :value="value">{{ t('brand.' + value) }}</option></select></label>
          <button class="btn btn-primary" :disabled="saving">{{ t('brand.grant') }}</button>
        </form>
      </section>
    </div>
  </AppLayout>
</template>
