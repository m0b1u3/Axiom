import { ref } from "vue";
const isDark = ref(localStorage.getItem("axiom_theme") === "dark");
export function applyTheme(): void {
  document.documentElement.classList.toggle("dark", isDark.value);
  document.documentElement.style.colorScheme = isDark.value ? "dark" : "light";
}
export function useTheme() {
  function setTheme(dark: boolean): void {
    isDark.value = dark;
    localStorage.setItem("axiom_theme", dark ? "dark" : "light");
    applyTheme();
  }
  return { isDark, setTheme, toggleTheme: () => setTheme(!isDark.value) };
}
