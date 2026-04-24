import type { TopologyGraphLink } from '../../lib/topology'

export interface PartitionedEdges {
  dagreEdges: TopologyGraphLink[]
  cyclicEdges: TopologyGraphLink[]
}

function edgeSort(left: TopologyGraphLink, right: TopologyGraphLink): number {
  if (left.from_node !== right.from_node) {
    return left.from_node.localeCompare(right.from_node)
  }
  if (left.to_node !== right.to_node) {
    return left.to_node.localeCompare(right.to_node)
  }
  if (left.from_port !== right.from_port) {
    return left.from_port - right.from_port
  }
  return left.display_name.localeCompare(right.display_name)
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

export function partitionCyclicEdges(edges: TopologyGraphLink[]): PartitionedEdges {
  const sortedEdges = [...edges].sort(edgeSort)
  const dagreEdges: TopologyGraphLink[] = []
  const cyclicEdges: TopologyGraphLink[] = []
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
