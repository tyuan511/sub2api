import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { useImageStudioStore } from '../imageStudio'
import type { StudioCreationResponse } from '@/api/imageStudio'
const mocks = vi.hoisted(() => ({ generate: vi.fn(), poll: vi.fn(), history: vi.fn(), remove: vi.fn(), import: vi.fn(), read: vi.fn(), write: vi.fn(), auth: { user: { id: 1 } } }))
vi.mock('@/api/imageStudio', async original => ({ ...await original<object>(), generateImages: mocks.generate, pollImageTask: mocks.poll, getStudioHistory: mocks.history, deleteStudioCreation: mocks.remove, importStudioHistory: mocks.import }))
vi.mock('@/utils/imageStudioHistory', () => ({ readImageHistory: mocks.read, writeImageHistory: mocks.write }))
vi.mock('../auth', () => ({ useAuthStore: () => reactive(mocks.auth) }))
const taskId = 'imgtask_' + 'a'.repeat(32)
const record = (overrides: Partial<StudioCreationResponse> = {}): StudioCreationResponse => ({ id: taskId, prompt: 'a cat', model: 'gpt-image-2', ratio: '1:1', resolution: '1K', count: 1, key_id: 7, key_name: 'Art', created_at: 123456, status: 'completed', references: [], images: [{ id: 'asset-id', filename: 'image.png', content_type: 'image/png', size: 10, url: 'https://storage.example/image.png' }], ...overrides })
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
beforeEach(() => { vi.clearAllMocks(); mocks.auth.user = { id: 1 }; mocks.history.mockResolvedValue({ items: [], has_more: false }); mocks.read.mockResolvedValue([]); mocks.write.mockResolvedValue(undefined); setActivePinia(createPinia()) })
afterEach(() => vi.restoreAllMocks())
const submit = (store: ReturnType<typeof useImageStudioStore>) => store.generate('secret-key', 'a cat', 'gpt-image-2', '1:1', 1, '1K', [], 7, 'Art')
describe('database image history', () => {
  it('loads history in a fresh browser without IndexedDB and retains stable asset IDs', async () => {
    mocks.read.mockRejectedValue(new Error('IndexedDB unavailable'))
    mocks.history.mockResolvedValue({ items: [record()], has_more: false })
    const store = useImageStudioStore(); await flushPromises()
    expect(store.creations[0]).toMatchObject({ id: taskId, images: [{ id: 'asset-id' }] })
    expect(store.historyUnavailable).toBe(false)
    expect(mocks.generate).not.toHaveBeenCalled(); expect(mocks.write).not.toHaveBeenCalled(); store.$dispose()
  })
  it('isolates concurrent submissions, out-of-order results and failures without browser persistence', async () => {
    const ids = [taskId, 'imgtask_' + 'b'.repeat(32), 'imgtask_' + 'c'.repeat(32)]
    const tasks = ids.map(() => deferred<StudioCreationResponse>())
    tasks.forEach((task, index) => mocks.generate.mockImplementationOnce((_key, _payload, _signal, _refs, submitted) => {
      submitted({ taskId: ids[index] }); return task.promise
    }))
    const store = useImageStudioStore(); await flushPromises()
    const firstFile = new File(['cat'], 'cat.png', { type: 'image/png' })
    const secondFile = new File(['sky'], 'sky.png', { type: 'image/png' })
    const references = [firstFile]
    const first = store.generate('first-key', 'a cat', 'gpt-image-2', '1:1', 1, '1K', references, 7, 'Art')
    references.splice(0, 1, secondFile)
    const second = store.generate('second-key', 'a sky', 'gpt-image-2', '16:9', 2, '2K', references, 8, 'Landscape')
    const third = store.generate('first-key', 'a tree', 'gpt-image-2', '3:4', 1, '2K', [], 7, 'Art')
    expect(mocks.generate).toHaveBeenCalledTimes(3)
    expect(store.creations.map(item => item.id)).toEqual([...ids].reverse())
    expect(store.creations.every(item => item.status === 'generating')).toBe(true)
    expect(mocks.generate.mock.calls[0][3]).toEqual([firstFile])
    expect(mocks.generate.mock.calls[1][3]).toEqual([secondFile])
    expect(mocks.generate.mock.calls[1][1]).toMatchObject({ prompt: 'a sky', n: 2, size: '2048x1152' })
    expect(new Set(mocks.generate.mock.calls.map(call => call[2])).size).toBe(3)
    tasks[1].resolve(record({ id: ids[1], prompt: 'a sky', ratio: '16:9', resolution: '2K', key_id: 8, key_name: 'Landscape', images: [] }))
    await second
    expect(store.creations.find(item => item.id === ids[1])).toMatchObject({ status: 'completed', prompt: 'a sky', keyId: 8 })
    expect(store.creations.find(item => item.id === ids[0])).toMatchObject({ status: 'generating', references: [firstFile] })
    tasks[0].reject(new Error('Provider rejected this task')); await first
    expect(store.creations.find(item => item.id === ids[0])).toMatchObject({ status: 'failed', error: 'Provider rejected this task' })
    expect(store.creations.find(item => item.id === ids[2])?.status).toBe('generating')
    expect(store.generating).toBe(true)
    tasks[2].resolve(record({ id: ids[2], prompt: 'a tree' })); await third
    expect(store.creations.find(item => item.id === ids[1])?.status).toBe('completed')
    expect(store.creations.find(item => item.id === ids[2])).toMatchObject({ status: 'completed', prompt: 'a tree' })
    expect(store.generating).toBe(false)
    expect(mocks.write).not.toHaveBeenCalled(); store.$dispose()
  })
  it('resumes a database task without an API Key or a new generation POST', async () => {
    mocks.history.mockResolvedValue({ items: [record({ status: 'processing', images: [] })], has_more: false })
    mocks.poll.mockResolvedValue(record())
    const store = useImageStudioStore(); await flushPromises()
    expect(mocks.poll).toHaveBeenCalledWith(taskId, expect.any(AbortSignal))
    expect(mocks.generate).not.toHaveBeenCalled(); expect(store.creations[0].status).toBe('completed'); store.$dispose()
  })
  it('resumes multiple database tasks independently and permits another submission', async () => {
    const secondId = 'imgtask_' + 'b'.repeat(32)
    const ids = [taskId, secondId]
    const pending = ids.map(() => deferred<StudioCreationResponse>())
    mocks.history.mockResolvedValue({ items: ids.map(id => record({ id, status: 'processing', images: [] })), has_more: false })
    mocks.poll.mockImplementation(id => pending[ids.indexOf(id)].promise)
    mocks.generate.mockResolvedValue(record({ id: 'imgtask_' + 'c'.repeat(32), prompt: 'new task' }))
    const store = useImageStudioStore(); await flushPromises()
    expect(mocks.poll).toHaveBeenCalledTimes(2)
    await submit(store)
    expect(mocks.generate).toHaveBeenCalledTimes(1)
    expect(store.creations).toHaveLength(3)
    pending[1].resolve(record({ id: secondId })); await flushPromises()
    expect(store.creations.find(item => item.id === taskId)?.status).toBe('generating')
    pending[0].resolve(record()); await flushPromises()
    expect(store.creations.every(item => item.status === 'completed')).toBe(true); store.$dispose()
  })
  it('isolates accounts from late results', async () => {
    let finish!: (item: StudioCreationResponse) => void
    mocks.generate.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const store = useImageStudioStore(); await flushPromises(); const pending = submit(store)
    reactive(mocks.auth).user = { id: 2 }; await flushPromises(); finish(record()); await pending
    expect(store.creations).toEqual([]); store.$dispose()
  })
  it('deletes history on the server and preserves it if deletion fails', async () => {
    mocks.history.mockResolvedValue({ items: [record()], has_more: false })
    const store = useImageStudioStore(); await flushPromises()
    mocks.remove.mockRejectedValueOnce(new Error('network'))
    await expect(store.remove(taskId)).rejects.toThrow('network'); expect(store.creations).toHaveLength(1)
    mocks.remove.mockResolvedValueOnce(undefined); await store.remove(taskId)
    expect(mocks.remove).toHaveBeenCalledWith(taskId); expect(store.creations).toHaveLength(0); store.$dispose()
  })
  it('loads additional pages without dropping earlier history', async () => {
    mocks.history.mockResolvedValueOnce({ items: [record()], has_more: true }).mockResolvedValueOnce({ items: [record({ id: 'older' })], has_more: false })
    const store = useImageStudioStore(); await flushPromises(); await store.loadMore()
    expect(store.creations.map(item => item.id)).toEqual([taskId, 'older']); expect(store.hasMore).toBe(false); store.$dispose()
  })
})
