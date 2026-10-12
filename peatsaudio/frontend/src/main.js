import './style.css'
import { mount } from 'svelte'
import App from './App.svelte'

// index.html always carries #app; the cast tells the type checker so.
const target = /** @type {HTMLElement} */ (document.getElementById('app'))

const app = mount(App, { target })

export default app
