import { ref } from 'vue'

// Module-level singleton: every component that calls useTheme() shares the
// same state, so the sidebar toggle and any page-level toggle can't desync.
// main.ts#initThemeClass applies the initial class before app mount; here we
// only mirror and flip it.
const isDark = ref(document.documentElement.classList.contains('dark'))

function setTheme(dark: boolean) {
  isDark.value = dark
  document.documentElement.classList.toggle('dark', dark)
  localStorage.setItem('theme', dark ? 'dark' : 'light')
}

export function useTheme() {
  return {
    isDark,
    toggleTheme: () => setTheme(!isDark.value),
    setTheme
  }
}
