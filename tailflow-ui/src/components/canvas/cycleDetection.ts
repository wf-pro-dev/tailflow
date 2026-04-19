import type { TopologyEdge } from '../../api/types'

export interface PartitionedEdges {
  dagreEdges: TopologyEdge[]
  cyclicEdges: TopologyEdge[]
}

function edgeSort(left: TopologyEdge, right: TopologyEdge): number {
  if (left.from_node !== right.from_node) {
    return left.from_node.localeCompare(right.from_node)
  }
  if (left.from_port !== right.from_port) {
    return left.from_port - right.from_port
  }
  if (left.kind !== right.kind) {
    return left.kind.localeCompare(right.kind)
  }
  return left.raw_upstream.localeCompare(right.raw_upstream)
}

function hasPath(
  adjacency: Map<string, Set<string>>,
  start: string,
  goal: string,
  visited: Set<string> = new Set(),
): boolean {
  if (start === goal) {
    return true
  }
  if (visited.has(start)) {
    return false
  }

  visited.add(start)
  const nextNodes = adjacency.get(start)
  if (!nextNodes) {
    return false
  }

  for (const nextNode of nextNodes) {
    if (hasPath(adjacency, nextNode, goal, visited)) {
      return true
    }
  }

  return false
}

export function partitionCyclicEdges(edges: TopologyEdge[]): PartitionedEdges {
  const sortedEdges = [...edges].sort(edgeSort)
  const dagreEdges: TopologyEdge[] = []
  const cyclicEdges: TopologyEdge[] = []
  const adjacency = new Map<string, Set<string>>()

  for (const edge of sortedEdges) {
    if (edge.from_node === edge.to_node) {
      cyclicEdges.push(edge)
      continue
    }

    if (hasPath(adjacency, edge.to_node, edge.from_node)) {
      cyclicEdges.push(edge)
      continue
    }

    dagreEdges.push(edge)
    if (!adjacency.has(edge.from_node)) {
      adjacency.set(edge.from_node, new Set())
    }
    adjacency.get(edge.from_node)?.add(edge.to_node)
  }

  return {
    dagreEdges,
    cyclicEdges,
  }
}
