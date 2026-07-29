import { writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

const here = dirname(fileURLToPath(import.meta.url))

/**
 * Puts dist/.gitkeep back after every build.
 *
 * That file is tracked on purpose: main.go does //go:embed all:frontend/dist,
 * and Wails loads the package to generate its bindings *before* it compiles
 * the frontend, so the directory has to exist on a fresh clone or the build
 * fails before it starts. Vite empties outDir on every run and would take the
 * file with it, leaving a spurious deletion in git status.
 */
function keepDistDirectory() {
  return {
    name: 'keep-dist-directory',
    closeBundle() {
      writeFileSync(resolve(here, 'dist/.gitkeep'), '')
    },
  }
}

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte(), keepDistDirectory()],
})
