import { ensureStudioThumbnail, type StudioImage } from '@/api/imageStudio'
import { isImageUrlFresh } from './imageUrlCache'

const urls = new Map<string, string>()
const pending = new Map<string, Promise<string>>()

export async function studioThumbnailUrl(image: StudioImage & { id?: string }, force = false): Promise<string> {
  if (!image.id) return image.thumbnail_url || image.url
  const cached = urls.get(image.id)
  if (!force) {
    if (cached && isImageUrlFresh(cached)) return cached
    if (image.thumbnail_url && isImageUrlFresh(image.thumbnail_url)) return image.thumbnail_url
  }
  const id = image.id
  let request = pending.get(id)
  if (!request) {
    request = ensureStudioThumbnail(id).then(asset => {
      const url = asset.thumbnail_url!
      urls.set(id, url)
      if (urls.size > 256) urls.delete(urls.keys().next().value!)
      return url
    }).finally(() => pending.delete(id))
    pending.set(id, request)
  }
  return request
}
