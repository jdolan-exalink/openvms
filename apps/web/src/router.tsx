import type { QueryClient } from "@tanstack/react-query";
import {
  createRootRouteWithContext, createRoute, createRouter, lazyRouteComponent, Outlet, redirect,
  type RouteComponent,
} from "@tanstack/react-router";
import { meQuery } from "./api/queries";
import { Layout } from "./components/Layout";
import { SettingsLayout } from "./components/SettingsLayout";
import { parseEventsSearch } from "./lib/eventsSearch";

const Account = lazyRouteComponent(() => import("./routes/Account"), "Account");
const Alarms = lazyRouteComponent(() => import("./routes/Alarms"), "Alarms");
const Audit = lazyRouteComponent(() => import("./routes/Audit"), "Audit");
const Branding = lazyRouteComponent(() => import("./routes/Branding"), "Branding");
const CameraGroups = lazyRouteComponent(() => import("./routes/CameraGroups"), "CameraGroups");
const Cameras = lazyRouteComponent(() => import("./routes/Cameras"), "Cameras");
const Dashboard = lazyRouteComponent(() => import("./routes/Dashboard"), "Dashboard");
const Events = lazyRouteComponent(() => import("./routes/Events"), "Events");
const Exports = lazyRouteComponent(() => import("./routes/Exports"), "Exports");
const Groups = lazyRouteComponent(() => import("./routes/Groups"), "Groups");
const Live = lazyRouteComponent(() => import("./routes/Live"), "Live");
const Login = lazyRouteComponent(() => import("./routes/Login"), "Login");
const Notifications = lazyRouteComponent(() => import("./routes/Notifications"), "Notifications");
const Permissions = lazyRouteComponent(() => import("./routes/Permissions"), "Permissions");
const Plates = lazyRouteComponent(() => import("./routes/Plates"), "Plates");
import { Playback } from "./routes/Playback";
const Rules = lazyRouteComponent(() => import("./routes/Rules"), "Rules");
const Channels = lazyRouteComponent(() => import("./routes/Channels"), "Channels");
const Servers = lazyRouteComponent(() => import("./routes/Servers"), "Servers");
const Sites = lazyRouteComponent(() => import("./routes/Sites"), "Sites");
const Users = lazyRouteComponent(() => import("./routes/Users"), "Users");

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
const child = <P extends string>(path: P, component: RouteComponent) => createRoute({ getParentRoute: () => appRoute, path, component });

type PlaybackSearch = { camera?: string; cameras?: string[]; t?: number };

const playbackRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/playback",
  component: Playback,
  validateSearch: (s: Record<string, unknown>): PlaybackSearch => {
    let cameras: string[] | undefined;
    if (Array.isArray(s.cameras)) {
      cameras = s.cameras.filter((c): c is string => typeof c === "string" && !!c);
    } else if (typeof s.cameras === "string" && s.cameras) {
      cameras = s.cameras.split(",").filter(Boolean);
    }
    const singleCamera = typeof s.camera === "string" && s.camera ? s.camera : undefined;
    if (!cameras && singleCamera) {
      cameras = [singleCamera];
    }
    return {
      camera: singleCamera ?? cameras?.[0],
      cameras: cameras && cameras.length > 0 ? cameras : undefined,
      t: typeof s.t === "number" ? s.t : typeof s.t === "string" && /^\d+$/.test(s.t) ? Number(s.t) : undefined,
    };
  },
});

type AlarmsSearch = {
  status?: "open" | "acknowledged" | "resolved";
  camera_id?: string;
  site_id?: string;
};

const alarmsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/alarms",
  component: Alarms,
  validateSearch: (s: Record<string, unknown>): AlarmsSearch => ({
    status: s.status === "open" || s.status === "acknowledged" || s.status === "resolved" ? s.status : undefined,
    camera_id: typeof s.camera_id === "string" && s.camera_id ? s.camera_id : undefined,
    site_id: typeof s.site_id === "string" && s.site_id ? s.site_id : undefined,
  }),
});

// Applied Events filters live in the query string, so a filtered list is shareable and reload-safe.
const eventsRoute = createRoute({ getParentRoute: () => appRoute, path: "/events", component: Events, validateSearch: parseEventsSearch });

// "/" always resolves to Live (En vivo): the operational landing page.
const indexRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/live" });
  },
});

// Settings is a pathless layout (like appRoute above): its children keep their own literal,
// unprefixed paths, so existing links/bookmarks to e.g. "/sites" keep working unchanged. It only
// adds the shared SettingsLayout chrome (sub-nav) around the former Infraestructura/
// Administración pages and the settings landing page (the former Panel/Dashboard content).
const settingsRoute = createRoute({ getParentRoute: () => appRoute, id: "settings", component: SettingsLayout });
const settingsChild = <P extends string>(path: P, component: RouteComponent) =>
  createRoute({ getParentRoute: () => settingsRoute, path, component });

const permissionsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "/permissions",
  component: Permissions,
  validateSearch: (s: Record<string, unknown>): { subject?: string } => ({
    subject: typeof s.subject === "string" && /^(user|group):[0-9a-f-]{36}$/.test(s.subject) ? s.subject : undefined,
  }),
});

type InventorySearch = { site_id?: string; server_id?: string };
const inventorySearch = (s: Record<string, unknown>): InventorySearch => ({
  site_id: typeof s.site_id === "string" && s.site_id ? s.site_id : undefined,
  server_id: typeof s.server_id === "string" && s.server_id ? s.server_id : undefined,
});
const serversRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/servers", component: Servers, validateSearch: inventorySearch });
const camerasRoute = createRoute({ getParentRoute: () => settingsRoute, path: "/cameras", component: Cameras, validateSearch: inventorySearch });

export const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([
    indexRoute,
    child("/live", Live),
    alarmsRoute,
    eventsRoute,
    child("/plates", Plates),
    playbackRoute,
    child("/exports", Exports),
    settingsRoute.addChildren([
      settingsChild("/settings", Dashboard),
      settingsChild("/sites", Sites),
      serversRoute,
      camerasRoute,
      settingsChild("/camera-groups", CameraGroups),
      settingsChild("/rules", Rules),
      settingsChild("/channels", Channels),
      settingsChild("/notifications", Notifications),
      settingsChild("/users", Users),
      settingsChild("/groups", Groups),
      permissionsRoute,
      settingsChild("/audit", Audit),
      settingsChild("/branding", Branding),
      settingsChild("/account", Account),
    ]),
  ]),
]);

export const router = createRouter({ routeTree, context: { queryClient: undefined! } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
