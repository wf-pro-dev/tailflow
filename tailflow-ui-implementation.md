# Tailflow Web UI — Implementation Guide

This document covers everything needed to implement the Tailflow frontend from scratch. It is written for a developer who has not been part of the prior design discussions.

---

## What Tailflow Is

Tailflow is a single-page network topology tool. It connects to a Go backend over SSE and REST, shows which TCP ports are bound on each Tailscale node, which containers are publishing those ports, and how proxy configs (nginx, Caddy) route traffic between them. The primary view is a directed graph. Everything else — node lists, proxy config forms, run history — is secondary to that canvas.

The UI is desktop-only for v1. No mobile support. No authentication layer — the tool is single-user, exposed only on the Tailscale network.

---

## Stack Decisions

| Concern | Choice | Reason |
|---|---|---|
| Framework | React 19 + Vite | Graph library ecosystem is React-first |
| Component library | shadcn/ui + Tailwind CSS | Unstyled primitives you own, no design system conflict |
| Graph canvas | ReactFlow + Dagre | Production-grade canvas, predictable hierarchical layout |
| Server state | TanStack Query v5 | Fetch, cache, and invalidate REST data |
| Real-time state | Zustand | Flat store, zero boilerplate, fast streaming delta application |
| Layout engine | `@dagrejs/dagre` via ReactFlow | Stable, deterministic, sufficient for v1 topologies |
| HTTP client | Native `fetch` | No extra dependency needed |
| SSE client | Native `EventSource` | Browser-native, handles reconnect |
| Styling | Tailwind CSS v4 | Utility-first, co-located with shadcn |

---

## Design Language

The graph canvas is the product. Every other visual decision supports it.

**Color system**

- Background: near-white `#fafafa` for the canvas, `#ffffff` for panels
- Sidebar: `#0f0f0f` dark, white text
- Nodes: white cards with `1px` border `#e4e4e7`
- Edges: `#a1a1aa` hairline, `1.5px`
- Online indicator: `#22c55e` (green-500)
- Offline / degraded indicator: `#f59e0b` (amber-500) for degraded, `#71717a` (zinc-500) for offline
- Error: `#ef4444` (red-500)
- Accent (unresolved edges, warnings): `#f59e0b`
- No gradients. No box shadows on interactive elements. Borders only.

**Typography**

- Font: `Inter` (loaded from Google Fonts or bundled)
- Node title: `13px` medium
- Port labels: `11px` regular, `#71717a`
- Sidebar labels: `12px` medium, uppercase, `#a1a1aa` tracking-wide

**Interactions**

- Node hover: border changes to `#a1a1aa`, slight elevation via `box-shadow: 0 1px 4px rgba(0,0,0,0.08)`
- Selected node: border `#0f0f0f`, detail panel slides in from right
- Zoom: scroll wheel. Pan: click-drag on canvas background
- Cyclic edges rendered as curved arcs above the node layer, not as straight lines through the hierarchy

**Design references to study before building**

- Vercel dashboard — status communication without noise
- Linear — real-time updates that do not cause layout shifts
- Planetscale branch topology view — directed graph in an infrastructure UI
- Datadog service map — progressive disclosure at different zoom levels

---

## File Hierarchy

```
tailflow-ui/
  index.html
  vite.config.ts
  tailwind.config.ts
  tsconfig.json
  package.json
  .env.example
  .env.local                        # gitignored
  src/
    main.tsx                        # React root, QueryClientProvider, ErrorBoundary
    App.tsx                         # Route shell, sidebar, canvas area
    env.ts                          # Typed env variable accessors
    api/
      client.ts                     # Base fetch wrapper, base URL from env
      nodes.ts                      # GET /api/v1/nodes, GET /api/v1/nodes/{name}
      topology.ts                   # GET /api/v1/topology, edges, unresolved
      runs.ts                       # GET /api/v1/runs, POST /api/v1/runs
      proxy-configs.ts              # GET/PUT/DELETE /api/v1/configs
      health.ts                     # GET /api/v1/health
    sse/
      client.ts                     # EventSource wrapper, reconnect logic, Last-Event-ID
      useNodeStream.ts              # Hook: subscribes to /api/v1/nodes/stream
      useTopologyStream.ts          # Hook: subscribes to /api/v1/topology/stream
    store/
      topology.ts                   # Zustand store: applyPortBound, applyPortReleased,
                                    #   applySnapshot, applyEdgeDiff, setNodeStatus
      ui.ts                         # Zustand store: selected node, panel open state,
                                    #   zoom level, layout mode
    hooks/
      useNodes.ts                   # TanStack Query: fetches and caches node list
      useTopology.ts                # TanStack Query: fetches and caches topology
      useRuns.ts                    # TanStack Query: fetches collection run history
      useProxyConfigs.ts            # TanStack Query: fetches proxy configs for a node
      useHealth.ts                  # TanStack Query: fetches health, polling interval 30s
      useTriggerRun.ts              # TanStack Query mutation: POST /api/v1/runs
      useSetProxyConfig.ts          # TanStack Query mutation: PUT /api/v1/configs/{node}
    components/
      canvas/
        TopologyCanvas.tsx          # ReactFlow root, layout engine wiring
        TopologyNode.tsx            # Custom ReactFlow node: name, IP, port list, status dot
        TopologyEdge.tsx            # Custom ReactFlow edge: label (proxy_pass / container),
                                    #   curved arc for cyclic edges
        NodeMinimap.tsx             # ReactFlow MiniMap, positioned bottom-right
        CanvasControls.tsx          # Zoom in / zoom out / fit view buttons
        layout.ts                  # Dagre layout function: takes nodes+edges, returns
                                    #   positioned nodes+edges for ReactFlow
        cycleDetection.ts          # Detects cyclic edges before layout, flags them
                                    #   for curved rendering
        EmptyCanvas.tsx             # Shown when no collection run exists yet
        LoadingCanvas.tsx           # Skeleton state while first fetch is in flight
      sidebar/
        Sidebar.tsx                 # Fixed left panel: logo, nav, health indicator
        NodeList.tsx                # Scrollable list of nodes with online/offline/degraded
        RunHistory.tsx              # Last N collection runs with timestamps and error counts
        TriggerButton.tsx           # "Collect now" button, calls useTriggerRun
      detail/
        DetailPanel.tsx             # Slide-in right panel when a node is selected
        PortTable.tsx               # Table of ListenPort entries for selected node
        ContainerTable.tsx          # Table of ContainerPort entries
        EdgeList.tsx                # Outbound and inbound edges for selected node
        ProxyConfigForm.tsx         # Kind selector + config path input + preview
        StaleIndicator.tsx          # "Last seen X ago" with amber color if stale
      shared/
        StatusDot.tsx               # Green / amber / grey dot, used on nodes and sidebar
        ErrorBoundary.tsx           # React error boundary wrapping canvas and SSE hooks
        ErrorState.tsx              # Shown inside ErrorBoundary on uncaught render error
        Tooltip.tsx                 # Radix UI tooltip wrapper
        Badge.tsx                   # shadcn badge for edge kind labels
        Spinner.tsx                 # Minimal loading indicator
    lib/
      utils.ts                      # cn() classname helper (shadcn convention)
      time.ts                       # formatRelative(), formatTimestamp() helpers
      stale.ts                      # isStale(collectedAt, thresholdSeconds): boolean
  public/
    favicon.svg
```

---

## Environment Variables

All variables are prefixed `VITE_` so Vite exposes them to the browser bundle. Never put secrets here.

```bash
# .env.example

# Base URL of the tailflow Go backend.
# In development, Vite proxies /api to this address (see vite.config.ts).
# In production (Docker), this is the same origin as the UI, so leave empty.
VITE_API_BASE_URL=

# How long (in seconds) before a node snapshot is considered stale.
# Nodes not updated within this window get the amber stale indicator.
# Should be set to roughly 2x the backend collect interval.
VITE_STALE_THRESHOLD_SECONDS=90

# Optional: override the SSE reconnect delay in milliseconds.
# Defaults to 3000 if not set.
VITE_SSE_RECONNECT_DELAY_MS=3000
```

Access these in code only through `src/env.ts`, never via `import.meta.env` directly in components:

```ts
// src/env.ts
export const env = {
  apiBaseUrl: import.meta.env.VITE_API_BASE_URL ?? '',
  staleThresholdSeconds: Number(import.meta.env.VITE_STALE_THRESHOLD_SECONDS ?? 90),
  sseReconnectDelayMs: Number(import.meta.env.VITE_SSE_RECONNECT_DELAY_MS ?? 3000),
} as const
```

---

## Docker Integration

The UI is served as static files built by Vite and served by a lightweight HTTP server inside Docker. The Go backend and the UI run as separate services in the same Docker Compose file.

```dockerfile
# Dockerfile.ui
FROM node:22-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
```

```nginx
# nginx.conf — serves the SPA and proxies /api to the backend
server {
  listen 80;

  root /usr/share/nginx/html;
  index index.html;

  # SPA fallback: all non-asset routes serve index.html
  location / {
    try_files $uri $uri/ /index.html;
  }

  # Proxy API and SSE to the Go backend
  location /api/ {
    proxy_pass http://tailflow-backend:8080;
    proxy_http_version 1.1;

    # Critical for SSE: disable nginx buffering
    proxy_set_header Connection '';
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 3600s;
  }
}
```

```yaml
# Addition to docker-compose.yaml
services:
  tailflow-ui:
    build:
      context: ../ui
      dockerfile: Dockerfile.ui
    networks:
      - tailflow-net
    container_name: tailflow-ui
    restart: unless-stopped
    ports:
      - "3000:80"
    depends_on:
      - tailflow-backend
```

In development, Vite's dev server proxies `/api` to the backend directly:

```ts
// vite.config.ts
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
```

---

## State Architecture

The two-layer state model is the most important architectural decision in the frontend.

**Layer 1 — TanStack Query (server state)**

Responsible for: initial data fetch on page load, caching REST responses, background refetch, optimistic updates on mutations.

Every REST endpoint has a corresponding query key and hook in `src/hooks/`. The query keys are stable strings that TanStack Query uses for cache invalidation:

```
['nodes']                       → GET /api/v1/nodes
['topology']                    → GET /api/v1/topology
['runs']                        → GET /api/v1/runs
['proxy-configs', nodeName]     → GET /api/v1/configs?node={name}
['health']                      → GET /api/v1/health
```

When an SSE event signals that data has changed — a `snapshot.updated` event, a `topology.run_completed` event — the SSE hook calls `queryClient.invalidateQueries(['topology'])` to trigger a background refetch. This keeps the REST cache and the stream in sync without the UI needing to manually merge them.

**Layer 2 — Zustand (real-time state)**

Responsible for: applying streaming deltas to in-memory state between REST fetches. This state is ephemeral — it is not persisted, it is rebuilt from the SSE stream on every page load.

```ts
// src/store/topology.ts — shape
interface TopologyStore {
  // Port state per node, patched by port.bound / port.released events
  portsByNode: Record<string, ListenPort[]>

  // Node liveness, patched by node.connected / node.disconnected / node.degraded
  nodeStatus: Record<string, NodeStatus>

  // Latest edge diff received from topology.edge_added / removed / changed
  lastEdgeDiff: EdgeDiff | null

  // Actions
  applyPortBound: (event: PortBoundEvent) => void
  applyPortReleased: (event: PortReleasedEvent) => void
  applyNodeStatus: (event: NodeStatusEvent) => void
  applyEdgeDiff: (event: EdgeEvent) => void
  applySnapshot: (snapshot: TopologyResponse) => void
}
```

**How they connect**

On initial page load:

1. TanStack Query fetches `GET /api/v1/topology` → renders the canvas
2. `useTopologyStream` opens `EventSource` on `/api/v1/topology/stream`
3. The SSE connection receives `topology.snapshot` as its first event
4. If the SSE snapshot differs from the REST response, `applySnapshot` updates Zustand and `queryClient.setQueryData` updates TanStack Query's cache — the SSE version wins
5. Subsequent events (`port.bound`, `topology.edge_added`, etc.) call Zustand actions directly
6. `topology.run_completed` calls `queryClient.invalidateQueries(['topology', 'runs'])` to sync REST cache

---

## SSE Client

The native `EventSource` API does not support custom headers and reconnects automatically, but it does not pass `Last-Event-ID` on the initial connection — only on reconnects. The implementation in `src/sse/client.ts` wraps `EventSource` to:

- Store the last received event ID in memory
- On reconnect, pass `Last-Event-ID` as a query parameter (`?lastEventId=N`) since headers are not available, and the Go backend should accept it from either location
- Apply the configurable reconnect delay from `env.sseReconnectDelayMs`
- Expose an `onError` callback for the UI to show a connection status indicator

---

## Graph Layout

The layout function in `src/components/canvas/layout.ts` is called every time the topology data changes. It takes the raw node and edge lists from the API and returns positioned nodes and edges for ReactFlow.

**Dagre configuration**

```ts
const dagreGraph = new dagre.graphlib.Graph()
dagreGraph.setDefaultEdgeLabel(() => ({}))
dagreGraph.setGraph({
  rankdir: 'LR',        // left-to-right: traffic flows left to right
  nodesep: 60,          // vertical space between nodes in the same rank
  ranksep: 120,         // horizontal space between ranks
  marginx: 40,
  marginy: 40,
})
```

**Cycle handling**

Before passing edges to Dagre, `cycleDetection.ts` runs a DFS to identify back-edges. Cyclic edges are removed from the Dagre input (so they do not break the layout) and stored separately. After layout, they are added back to the ReactFlow edge list with `type: 'floating'` and a high `curvature` value so they render as arcs above the node layer. This prevents Dagre from seeing cycles while still showing them on the canvas.

**Layout stability on live updates**

When a port event patches a node — adding or removing a port — the node's dimensions may change, which would normally cause Dagre to recompute positions for all nodes. To prevent this: node dimensions are fixed at a maximum size (port list scrolls internally rather than expanding the card), so Dagre always receives the same node sizes and produces the same layout regardless of port count changes.

---

## Loading and Empty States

**First load — data is fetching**

`LoadingCanvas.tsx` renders three placeholder node cards in roughly the positions a small topology would occupy, with a pulsing skeleton animation. No spinner in the centre of the canvas — that reads as an error state.

**No data — first collection run has not completed**

`EmptyCanvas.tsx` renders a centred message: "No topology data yet." Below it, a "Collect now" button that calls `useTriggerRun`. A subtle animated dashed border on the canvas area communicates that it is waiting for data, not broken.

**Node is stale**

`StaleIndicator.tsx` inside the detail panel shows "Last seen 4 minutes ago" in `#f59e0b`. The node card on the canvas gets an amber border instead of the default zinc border. The threshold is controlled by `VITE_STALE_THRESHOLD_SECONDS`. This is purely visual — stale nodes remain in the topology.

**SSE disconnected**

A thin banner at the top of the canvas (not a modal, not a toast) shows "Live updates paused — reconnecting..." in amber. It disappears automatically when the `EventSource` reconnects. The REST data remains visible and usable while disconnected.

---

## Error Boundaries

Two error boundaries wrap the two highest-risk surfaces.

`ErrorBoundary.tsx` wraps `TopologyCanvas.tsx`. If ReactFlow or the layout engine throws — a malformed edge, a null node ID — the canvas is replaced with a non-disruptive error state: "Graph rendering failed. Try refreshing." The sidebar and detail panel remain functional.

A second instance of `ErrorBoundary.tsx` wraps the SSE hook tree. If the SSE client throws an uncaught error, it is caught here and the disconnected banner is shown rather than crashing the page.

---

## Proxy Config Form

The proxy config form in `DetailPanel.tsx` is the only write surface in the UI.

**Fields**

- Kind: dropdown, options `nginx` and `caddy`
- Config path: text input, placeholder `/etc/nginx/nginx.conf`
- Submit: "Save and preview"

**Behaviour**

On submit, `useSetProxyConfig` calls `PUT /api/v1/configs/{node}` with `{ kind, config_path }`. The backend reads the file from the remote node and returns a `preview` field containing the parsed `ForwardAction[]`. The form shows this preview inline — a small table of `listener → target` rows — before the user sees the topology update. If the parse fails, the backend returns a 400 with an error message and the form shows it inline without dismissing.

After a successful save, `useTriggerRun` is called automatically to trigger a collection cycle so the topology updates immediately.

---

## Development Setup

```bash
# Prerequisites: Node 22+, npm 10+

cd tailflow-ui
npm install

# Copy env template
cp .env.example .env.local
# Edit .env.local: set VITE_API_BASE_URL= (empty uses Vite proxy to localhost:8080)

# Start the Go backend first (see main repo README)
# Then:
npm run dev
# UI available at http://localhost:5173
```

**Recommended VS Code extensions**

- Tailwind CSS IntelliSense
- ESLint
- Prettier
- Pretty TypeScript Errors

---

## Implementation Order

The following order minimises blocked work and delivers a usable UI at each step.

**Step 1 — Shell and API client**
Set up Vite, Tailwind, shadcn, TanStack Query, Zustand. Implement `src/api/` and `src/env.ts`. Write a simple node list page that fetches and renders node names. No canvas yet. Verify the backend connection works end-to-end in Docker.

**Step 2 — SSE client**
Implement `src/sse/client.ts` and `useTopologyStream.ts`. Log events to the console. Verify reconnect behaviour by stopping and starting the backend.

**Step 3 — Zustand store**
Implement `src/store/topology.ts`. Wire SSE events to store actions. Verify that port events patch the store correctly by logging store state.

**Step 4 — Canvas and layout**
Implement `TopologyCanvas.tsx`, `TopologyNode.tsx`, `TopologyEdge.tsx`, and `layout.ts`. Use static mock data first. Verify Dagre produces correct layout. Then wire to real API data. Implement cycle detection and curved arc rendering.

**Step 5 — Empty and loading states**
Implement `EmptyCanvas.tsx`, `LoadingCanvas.tsx`, `StaleIndicator.tsx`, and the SSE disconnected banner.

**Step 6 — Detail panel**
Implement `DetailPanel.tsx` with port table, container table, edge list, and stale indicator. Wire node selection from ReactFlow click events to `ui.ts` Zustand store.

**Step 7 — Proxy config form**
Implement `ProxyConfigForm.tsx` and `useSetProxyConfig.ts`. Test the full flow: set a config, see the preview, trigger a run, see the topology update.

**Step 8 — Error boundaries**
Wrap canvas and SSE hooks in `ErrorBoundary.tsx`. Test by deliberately throwing in `TopologyNode.tsx`.

**Step 9 — Polish**
Sidebar run history, trigger button, health indicator, status dots, CSS consistency pass.

