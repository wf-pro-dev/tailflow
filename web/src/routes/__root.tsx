import { useState } from "react";
import { createRootRouteWithContext, Outlet, Link, useRouterState } from "@tanstack/react-router";
import { QueryClient } from "@tanstack/react-query";
import { useTheme } from "@/lib/theme";
import { useEventStream } from "@/lib/stream/useEventStream";
import { DensityProvider, useDensity } from "@/features/ui/DensityProvider";
import { ToastProvider } from "@/features/ui/ToastProvider";
import { CommandPalette, openCommandPalette } from "@/features/ui/CommandPalette";

interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootComponent,
});

function RootComponent() {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const router = useRouterState();
  const currentPath = router.location.pathname;
  // Base SSE subscription — always active so host/fleet data flows on every
  // route (including Settings). Host pages add their host-specific topics.
  useEventStream(["hosts", "fleet"]);

  return (
    <DensityProvider>
      <ToastProvider>
        <div className="flex min-h-screen bg-surface-base text-zinc-50">
      {/* Sidebar */}
      <aside
        className={`${sidebarCollapsed ? "w-16" : "w-56"} flex flex-col border-r border-line-subtle bg-surface-base transition-all duration-200`}
      >
        {/* Logo */}
        <div className="flex h-14 items-center gap-2 px-4">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-accent text-sm font-bold text-accent-contrast">
            T
          </div>
          {!sidebarCollapsed && (
            <span className="text-sm font-semibold tracking-tight">Tailflow</span>
          )}
        </div>

        {/* Nav items */}
        <nav className="flex-1 space-y-1 px-2 py-4">
          <NavItem
            to="/"
            icon={<OverviewIcon />}
            label="Overview"
            collapsed={sidebarCollapsed}
            active={currentPath === "/"}
          />
          <NavItem
            to="/hosts"
            icon={<HostsIcon />}
            label="Hosts"
            collapsed={sidebarCollapsed}
            active={currentPath.startsWith("/hosts")}
          />
          <NavItem
            to="/settings"
            icon={<SettingsIcon />}
            label="Settings"
            collapsed={sidebarCollapsed}
            active={currentPath === "/settings"}
          />
        </nav>

        {/* Collapse toggle */}
        <button
          onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
          className="flex h-10 items-center justify-center border-t border-line-subtle text-zinc-500 hover:text-zinc-300 transition-colors"
        >
          {sidebarCollapsed ? "→" : "← Collapse"}
        </button>
      </aside>

      {/* Main content area */}
      <div className="flex flex-1 flex-col overflow-hidden">
        {/* Topbar — opaque, sticky */}
        <header className="glass sticky top-0 z-30 flex h-14 items-center justify-between border-b border-line-subtle px-6">
          <div className="flex items-center gap-4">
            <h2 className="text-sm font-medium text-zinc-400">
              {currentPath === "/" ? "Fleet Overview" :
               currentPath.startsWith("/hosts/") ? "Host Detail" :
               currentPath === "/settings" ? "Settings" : "Tailflow"}
            </h2>
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={openCommandPalette}
              className="flex h-8 items-center gap-2 rounded-lg border border-line-subtle px-2.5 text-xs text-ink-muted transition-colors hover:bg-surface-raised hover:text-ink-secondary"
              title="Search hosts (⌘K)"
            >
              <SearchIcon />
              <kbd className="rounded border border-line px-1 text-[10px]">⌘K</kbd>
            </button>
            <DensityToggle />
            <div className="live-indicator">Live</div>
            <ThemeToggle />
          </div>
        </header>

        {/* Page content */}
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
        <CommandPalette />
      </ToastProvider>
    </DensityProvider>
  );
}

function NavItem({
  to,
  icon,
  label,
  collapsed,
  active,
}: {
  to: string;
  icon: React.ReactNode;
  label: string;
  collapsed: boolean;
  active: boolean;
}) {
  return (
    <Link
      to={to}
      className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors duration-150 ${
        active
          ? "bg-accent/10 text-accent"
          : "text-zinc-400 hover:bg-surface-raised hover:text-zinc-200"
      }`}
    >
      <span className="flex h-5 w-5 items-center justify-center">{icon}</span>
      {!collapsed && <span>{label}</span>}
    </Link>
  );
}

// --- Icons (inline SVG to avoid extra dependencies) ---

function OverviewIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="1" y="1" width="6" height="6" rx="1" stroke="currentColor" strokeWidth="1.5" />
      <rect x="9" y="1" width="6" height="6" rx="1" stroke="currentColor" strokeWidth="1.5" />
      <rect x="1" y="9" width="6" height="6" rx="1" stroke="currentColor" strokeWidth="1.5" />
      <rect x="9" y="9" width="6" height="6" rx="1" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  );
}

function HostsIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="1" y="2" width="14" height="4" rx="1" stroke="currentColor" strokeWidth="1.5" />
      <rect x="1" y="10" width="14" height="4" rx="1" stroke="currentColor" strokeWidth="1.5" />
      <circle cx="4" cy="4" r="0.5" fill="currentColor" />
      <circle cx="4" cy="12" r="0.5" fill="currentColor" />
    </svg>
  );
}

function SettingsIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="8" cy="8" r="2" stroke="currentColor" strokeWidth="1.5" />
      <path
        d="M8 1V3M8 13V15M15 8H13M3 8H1M12.95 3.05L11.5 4.5M4.5 11.5L3.05 12.95M12.95 12.95L11.5 11.5M4.5 4.5L3.05 3.05"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  );
}

/*
 * ThemeToggle — quick dark/light switch in the topbar. Shows the icon for the
 * theme you'd switch TO (moon when light, sun when dark). System mode is the
 * default and is configurable on the Settings page; this button commits an
 * explicit choice.
 */
function ThemeToggle() {
  const { resolved, setMode } = useTheme();
  const isDark = resolved === "dark";
  return (
    <button
      onClick={() => setMode(isDark ? "light" : "dark")}
      className="flex h-8 w-8 items-center justify-center rounded-lg text-zinc-400 transition-colors hover:bg-surface-raised hover:text-zinc-200"
      aria-label={isDark ? "Switch to light theme" : "Switch to dark theme"}
      title={isDark ? "Light theme" : "Dark theme"}
    >
      {isDark ? <SunIcon /> : <MoonIcon />}
    </button>
  );
}

function SunIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="8" cy="8" r="3" stroke="currentColor" strokeWidth="1.5" />
      <path
        d="M8 1V2.5M8 13.5V15M15 8H13.5M2.5 8H1M12.95 3.05L11.9 4.1M4.1 11.9L3.05 12.95M12.95 12.95L11.9 11.9M4.1 4.1L3.05 3.05"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path
        d="M13.5 9.2A5.5 5.5 0 0 1 6.8 2.5a5.5 5.5 0 1 0 6.7 6.7Z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
    </svg>
  );
}

/* DensityToggle — compact vs comfortable row density. */
function DensityToggle() {
  const { density, toggle } = useDensity();
  return (
    <button
      onClick={toggle}
      className="flex h-8 w-8 items-center justify-center rounded-lg text-zinc-400 transition-colors hover:bg-surface-raised hover:text-zinc-200"
      aria-label={`Density: ${density}. Click to toggle.`}
      title={`Density: ${density} (click to toggle)`}
    >
      {density === "compact" ? <CompactIcon /> : <ComfortableIcon />}
    </button>
  );
}

function SearchIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="7" cy="7" r="5" stroke="currentColor" strokeWidth="1.5" />
      <path d="M11 11L14 14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function CompactIcon() {
  // Three tight rows
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="2" y="3" width="12" height="2" rx="1" fill="currentColor" />
      <rect x="2" y="7" width="12" height="2" rx="1" fill="currentColor" />
      <rect x="2" y="11" width="12" height="2" rx="1" fill="currentColor" />
    </svg>
  );
}

function ComfortableIcon() {
  // Three spaced rows
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="2" y="2" width="12" height="2.5" rx="1" fill="currentColor" />
      <rect x="2" y="6.75" width="12" height="2.5" rx="1" fill="currentColor" />
      <rect x="2" y="11.5" width="12" height="2.5" rx="1" fill="currentColor" />
    </svg>
  );
}
