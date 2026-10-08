import { describe, expect, it, vi } from 'vitest'
import apiClient from './client'
import { brandAPI } from './brand'

vi.mock('./client', () => ({
	default: { post: vi.fn() },
}))

describe('brandAPI', () => {
	it('uploads media as multipart form data', async () => {
		vi.mocked(apiClient.post).mockResolvedValue({ data: { url: '/media/test', asset: 'test' } })
		const file = new File(['image'], 'test.png', { type: 'image/png' })

		await brandAPI.uploadMedia(file)

		expect(apiClient.post).toHaveBeenCalledOnce()
		const [url, body, config] = vi.mocked(apiClient.post).mock.calls[0]
		expect(url).toBe('/admin/brand-content/media')
		expect(body).toBeInstanceOf(FormData)
		expect((body as FormData).get('file')).toBe(file)
		expect(config).toMatchObject({ headers: { 'Content-Type': 'multipart/form-data' } })
	})
})
