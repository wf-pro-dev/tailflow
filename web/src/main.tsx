import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { routeTree } from "./routeTree.gen";
import "./styles.css";

// SSE-first query config: when the SSE stream is connected, queries are
// updated in real time via queryClient.setQueryData (see useEventStream).
// staleTime is set high (5 min) so TanStack Query doesn't refetch in the
// background — the SSE events are the source of truth. When SSE is
// disconnected (the useEventStream hook detects 3 failures), it calls
// invalidateQueries which triggers a refetch + starts polling.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 5 * 60 * 1000, // 5 min — SSE keeps data fresh, don't poll
    },
  },
});

const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  context: { queryClient },
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </React.StrictMode>,
);
