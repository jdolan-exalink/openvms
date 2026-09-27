import type { QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { createRootRouteWithContext, createRoute, createRouter, Outlet, redirect } from "@tanstack/react-router";
import { meQuery } from "./api/queries";
import { Layout } from "./components/Layout";
import { Account } from "./routes/Account";
import { Audit } from "./routes/Audit";
import { Cameras } from "./routes/Cameras";
import { Dashboard } from "./routes/Dashboard";
import { Events } from "./routes/Events";
import { Exports } from "./routes/Exports";
import { Groups } from "./routes/Groups";
import { Live } from "./routes/Live";
import { Login } from "./routes/Login";
import { Permissions } from "./routes/Permissions";
import { Plates } from "./routes/Plates";
import { Playback } from "./routes/Playback";
import { Servers } from "./routes/Servers";
import { Sites } from "./routes/Sites";
import { Users } from "./routes/Users";

export type RouterContext = { queryClient: QueryClient };

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: Outlet });

const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: Login });

// Everything else needs a session (or an API token); the API still authorizes each request.
const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  component: Layout,
  beforeLoad: async ({ context }) => {
    try {
      await context.queryClient.ensureQueryData(meQuery);
    } catch {
      throw redirect({ to: "/login" });
    }
  },
});

// Each route is declared on its own so paths stay literal types for typed <Link to>.
const child = <P extends string>(path: P, component: () => ReactNode) => createRoute({ getParentRoute: () => appRoute, path, component });

type PlaybackSearch = { camera?: string; t?: number };

const playbackRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/playback",
  component: Playback,
  validateSearch: (s: Record<string, unknown>): PlaybackSearch => ({
    camera: typeof s.camera === "string" ? s.camera : undefined,
    t: typeof s.t === "number" ? s.t : typeof s.t === "string" && /^\d+$/.test(s.t) ? Number(s.t) : undefined,
  }),
});

const permissionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/permissions",
  component: Permissions,
  validateSearch: (s: Record<string, unknown>): { subject?: string } => ({
    subject: typeof s.subject === "string" && /^(user|group):[0-9a-f-]{36}$/.test(s.subject) ? s.subject : undefined,
  }),
});

export const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([
    child("/", Dashboard),
    child("/live", Live),
    child("/events", Events),
    child("/plates", Plates),
    playbackRoute,
    child("/exports", Exports),
    child("/sites", Sites),
    child("/servers", Servers),
    child("/cameras", Cameras),
    child("/users", Users),
    child("/groups", Groups),
    permissionsRoute,
    child("/audit", Audit),
    child("/account", Account),
  ]),
]);

export const router = createRouter({ routeTree, context: { queryClient: undefined! } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
