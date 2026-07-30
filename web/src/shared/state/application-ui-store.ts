import { create } from 'zustand'
import { useShallow } from 'zustand/react/shallow'

export type Density = 'comfortable' | 'compact'

type ApplicationUiState = {
  readonly activeCommandMenu: boolean
  readonly density: Density
  readonly setActiveCommandMenu: (value: boolean) => void
  readonly setDensity: (value: Density) => void
}

export const useApplicationUiStore = create<ApplicationUiState>((set) => ({
  activeCommandMenu: false,
  density: 'comfortable',
  setActiveCommandMenu: (activeCommandMenu) => set({ activeCommandMenu }),
  setDensity: (density) => set({ density }),
}))

export function useApplicationUiPreferences() {
  return useApplicationUiStore(
    useShallow((state) => ({
      density: state.density,
      setDensity: state.setDensity,
    })),
  )
}
