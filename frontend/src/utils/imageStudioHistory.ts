import type { ImageRatio, ImageResolution, StudioImage } from '@/api/imageStudio'

export interface LegacyStudioCreation {
  id: string; prompt: string; model: string; ratio: ImageRatio; resolution: ImageResolution
  count: number; keyName: string; createdAt: number; status: string
  references: File[]; images: StudioImage[]; error?: string; taskId?: string
}

// Legacy records only: read old reference Files/URLs and remove each record after
// successful database import. New creations are never written to IndexedDB.
async function database(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open('sub2api-image-studio', 1)
    request.onupgradeneeded = () => request.result.createObjectStore('history')
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
    request.onblocked = () => reject(new Error('History database is blocked'))
  })
}

export async function readImageHistory(userId: number): Promise<LegacyStudioCreation[]> {
  const db = await database()
  try {
    return await new Promise((resolve, reject) => {
      const request = db.transaction('history').objectStore('history').get(userId)
      request.onsuccess = () => resolve(Array.isArray(request.result) ? request.result : [])
      request.onerror = () => reject(request.error)
    })
  } finally { db.close() }
}

export async function writeImageHistory(userId: number, records: LegacyStudioCreation[]): Promise<void> {
  const db = await database()
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction('history', 'readwrite')
      tx.objectStore('history').put(records, userId)
      tx.oncomplete = () => resolve()
      tx.onerror = () => reject(tx.error)
      tx.onabort = () => reject(tx.error)
    })
  } finally { db.close() }
}
