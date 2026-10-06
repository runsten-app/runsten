import { defineComponent, h } from 'vue'

// jsdom has no canvas: the charts of vue-chartjs become a canvas with their props, as
// vue-chartjs renders it (an image with its label), and the tests read their data.
const stub = (name: string) =>
  defineComponent({
    name,
    props: {
      data: { type: Object, required: true },
      options: { type: Object, default: undefined },
      plugins: { type: Array, default: () => [] },
      ariaLabel: { type: String, default: undefined },
    },
    setup: (props) => () => h('canvas', { role: 'img', 'aria-label': props.ariaLabel }),
  })

export const Bar = stub('Bar')
export const Line = stub('Line')
