const imageObjectURLCache = new Map<string, string>()
const imageObjectURLPending = new Map<string, Promise<string>>()

export function peekCachedImageObjectURL(url: string) {
  return imageObjectURLCache.get(url) || ''
}

export async function getCachedImageObjectURL(url: string) {
  const cached = imageObjectURLCache.get(url)
  if (cached) return cached

  const pending = imageObjectURLPending.get(url)
  if (pending) return pending

  const request = fetch(url)
    .then((res) => {
      if (!res.ok) throw new Error(`image fetch failed: ${res.status}`)
      return res.blob()
    })
    .then((blob) => {
      const objectURL = URL.createObjectURL(blob)
      imageObjectURLCache.set(url, objectURL)
      return objectURL
    })
    .finally(() => {
      imageObjectURLPending.delete(url)
    })

  imageObjectURLPending.set(url, request)
  return request
}
