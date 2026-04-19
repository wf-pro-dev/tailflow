# Tailflow UI

This is the React frontend for Tailflow. It now includes the application shell, typed API client, live node inventory, health and run status, SSE-driven updates, topology canvas rendering, and node detail views backed by the Go API.

## Conventions

### Spacing system

Tailflow uses a 4px base scale exposed as CSS variables and mirrored through Tailwind spacing utilities:

- `space-1` = 4px
- `space-2` = 8px
- `space-3` = 12px
- `space-4` = 16px
- `space-5` = 20px
- `space-6` = 24px
- `space-8` = 32px
- `space-10` = 40px
- `space-12` = 48px

Use the named token first, not ad hoc pixel values.

### Naming

- Components: `PascalCase`
- Hooks: `useCamelCase`
- Stores: `useXStore` with action names as `verbNoun`
- API DTOs: `SomethingResponse` / `SomethingRequest`
- UI-only derived data: `SomethingState` / `SomethingItem`
- Boolean helpers: `isX`, `hasX`, `canX`
- Shared constants: `SCREAMING_SNAKE_CASE`

## Current Scope

- Vite + React + Tailwind app scaffold
- Typed REST client for `nodes`, `runs`, `health`, `topology`, and `proxy configs`
- Sidebar with health, trigger-run action, node list, and run summary
- SSE subscriptions for node and topology updates
- Topology canvas with layout, edges, controls, and empty/loading states
- Detail panel for ports, containers, services, edges, and proxy config data
