import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { allowedBuckets, autoBucket, buckets, type Bucket, type Days } from '@/shared/lib'

// useBucket is the split of the statistics: the URL's bucket when the period allows it,
// else one chosen by the period's length. A split finer than the API's 400 intervals is
// never asked for.
export function useBucket(days: MaybeRefOrGetter<Days>) {
  const route = useRoute()
  const router = useRouter()
  const today = new Date()
  const allowed = computed(() => allowedBuckets(toValue(days), today))
  const bucket = computed<Bucket>(() => {
    const asked = route.query.bucket
    const ok = allowed.value
    if (typeof asked === 'string' && ok.some((b) => b === asked)) return asked as Bucket
    const auto = autoBucket(toValue(days), today)
    return ok.includes(auto) ? auto : (ok.at(-1) ?? 'month')
  })
  function setBucket(b: Bucket) {
    if (!buckets.includes(b)) return
    return router.replace({ query: { ...route.query, bucket: b } })
  }
  return { bucket, allowed, setBucket }
}
