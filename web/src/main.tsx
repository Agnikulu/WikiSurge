import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'

// Apply persisted dark mode immediately to avoid flash
try {
  const stored = JSON.parse(localStorage.getItem('wikisurge-settings') || '{}');
  if (stored?.state?.darkMode) {
    document.documentElement.classList.add('dark');
  }
} catch {
  // ignore
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
