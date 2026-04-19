import { create } from 'zustand'

interface UiState {
  selectedNodeName: string | null
  isDetailPanelOpen: boolean
  canvasZoom: number
  layoutMode: 'dagre-lr'
  setSelectedNodeName: (nodeName: string) => void
  clearSelectedNodeName: () => void
  setCanvasZoom: (zoomLevel: number) => void
  openDetailPanel: () => void
  closeDetailPanel: () => void
}

export const useUiStore = create<UiState>((set) => ({
  selectedNodeName: null,
  isDetailPanelOpen: false,
  canvasZoom: 1,
  layoutMode: 'dagre-lr',
  setSelectedNodeName: (nodeName) =>
    set({
      selectedNodeName: nodeName,
      isDetailPanelOpen: true,
    }),
  clearSelectedNodeName: () =>
    set({
      selectedNodeName: null,
      isDetailPanelOpen: false,
    }),
  setCanvasZoom: (zoomLevel) => set({ canvasZoom: zoomLevel }),
  openDetailPanel: () => set({ isDetailPanelOpen: true }),
  closeDetailPanel: () => set({ isDetailPanelOpen: false }),
}))
