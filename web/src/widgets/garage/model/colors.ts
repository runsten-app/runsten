import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { useTheme } from 'vuetify'

// The series' colors of the theme, in the order the vehicles take them: the accent, the
// blue, the teal, then the grey; the fifth, a shade of the accent, only beyond. Each reads
// at 3:1 on the tiles (test/app/theme.test.ts).
const palette = ['chart-1', 'chart-2', 'chart-5', 'chart-3', 'chart-4']

// useVehicleColors gives each vehicle of the account its color, by its place in the list:
// a vehicle keeps it when another is hidden from the comparison.
export function useVehicleColors(ids: MaybeRefOrGetter<string[]>) {
  const theme = useTheme()
  return computed(() => {
    const colors = theme.current.value.colors
    return new Map(
      toValue(ids).map((id, i) => {
        const c = colors[palette[i % palette.length] ?? 'chart-1']
        return [id, typeof c === 'string' ? c : '#000']
      }),
    )
  })
}
