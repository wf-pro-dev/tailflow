import { MiniMap } from '@xyflow/react'
import type { NodeResponse } from '../../api/types'
import { getNodeStatusView } from '../../lib/node-status'

interface NodeMinimapProps {
  inventoryNodesByName: Record<string, NodeResponse>
}

export function NodeMinimap(props: NodeMinimapProps) {
  return (
    <MiniMap
      pannable={false}
      zoomable={false}
      position="bottom-right"
      nodeStrokeWidth={2}
      maskColor="rgba(250, 250, 250, 0.75)"
      nodeColor={(node) => {
        const inventoryNode = props.inventoryNodesByName[node.id]
        const status = inventoryNode
          ? getNodeStatusView(inventoryNode)
          : { tone: 'offline' as const }

        if (status.tone === 'online') {
          return '#22c55e'
        }
        if (status.tone === 'warning') {
          return '#f59e0b'
        }
        return '#71717a'
      }}
    />
  )
}
