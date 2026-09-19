import { lazy, Suspense } from "react";
import { Navigate, createBrowserRouter } from "react-router";
import { AppShell } from "./AppShell";
import { Landing } from "./Landing";
import { RouteError } from "./RouteError";
import { KeepSearchRedirect } from "./KeepSearchRedirect";
import {
  AuthGuard,
  ConsoleGuard,
  SuperAdminGuard,
  WorkspaceGuard,
} from "@/auth/guards";
import { LoginPage } from "@/auth/LoginPage";
import { NoAccessPage } from "@/auth/NoAccessPage";
import { OIDCCallbackPage } from "@/auth/OIDCCallbackPage";
const ClaimMappingsPage = lazy(() =>
  import("@/features/catalog/CatalogPages").then((module) => ({
    default: module.ClaimMappingsPage,
  })),
);
const CoveragesPage = lazy(() =>
  import("@/features/catalog/CatalogPages").then((module) => ({
    default: module.CoveragesPage,
  })),
);
const LayerGroupsPage = lazy(() =>
  import("@/features/catalog/CatalogPages").then((module) => ({
    default: module.LayerGroupsPage,
  })),
);
const LayersPage = lazy(() =>
  import("@/features/catalog/CatalogPages").then((module) => ({
    default: module.LayersPage,
  })),
);
const StoresPage = lazy(() =>
  import("@/features/catalog/CatalogPages").then((module) => ({
    default: module.StoresPage,
  })),
);
const APIKeysPage = lazy(() =>
  import("@/features/security/APIKeysPage").then((module) => ({
    default: module.APIKeysPage,
  })),
);
const ImportsPage = lazy(() =>
  import("@/features/imports/ImportsPage").then((module) => ({
    default: module.ImportsPage,
  })),
);
const ImportDetailPage = lazy(() =>
  import("@/features/imports/ImportDetailPage").then((module) => ({
    default: module.ImportDetailPage,
  })),
);
const EndpointsPage = lazy(() =>
  import("@/features/endpoints/EndpointsPage").then((module) => ({
    default: module.EndpointsPage,
  })),
);
const AuditPage = lazy(() =>
  import("@/features/operations/AuditPage").then((module) => ({
    default: module.AuditPage,
  })),
);
const OperationsPage = lazy(() =>
  import("@/features/operations/OperationsPages").then((module) => ({
    default: module.OperationsPage,
  })),
);
const DeletionOperationsPage = lazy(() =>
  import("@/features/operations/DeletionOperationsPage").then((module) => ({
    default: module.DeletionOperationsPage,
  })),
);
const IdentityPage = lazy(() =>
  import("@/features/security/SecurityPages").then((module) => ({
    default: module.IdentityPage,
  })),
);
const RolesPage = lazy(() =>
  import("@/features/security/SecurityPages").then((module) => ({
    default: module.RolesPage,
  })),
);
const SettingsPage = lazy(() =>
  import("@/features/settings/SettingsPage").then((module) => ({
    default: module.SettingsPage,
  })),
);
const ServerDeletionsPage = lazy(() =>
  import("@/features/operations/DeletionOperationsPage").then((module) => ({
    default: module.ServerDeletionsPage,
  })),
);
const TileCachePage = lazy(() =>
  import("@/features/tiles/TileCachePage").then((module) => ({
    default: module.TileCachePage,
  })),
);
const TileMatrixSetsPage = lazy(() =>
  import("@/features/tiles/TileCachePage").then((module) => ({
    default: module.TileMatrixSetsPage,
  })),
);
import { WorkspaceOverview } from "@/features/workspaces/WorkspaceOverview";
const WorkspacesPage = lazy(() =>
  import("@/features/workspaces/WorkspacesPage").then((module) => ({
    default: module.WorkspacesPage,
  })),
);
import { runtimeBase } from "@/api/client";

const PreviewPage = lazy(() =>
  import("@/features/viewer/PreviewPage").then((module) => ({
    default: module.PreviewPage,
  })),
);
const StyleEditorPage = lazy(() =>
  import("@/features/styles/StyleEditorPage").then((module) => ({
    default: module.StyleEditorPage,
  })),
);
const fallback = (
  <p className="p-8 font-mono text-sm text-muted-foreground">
    Loading workspace instrument…
  </p>
);

const base = runtimeBase();
export const router = createBrowserRouter(
  [
    {
      path: "/login",
      element: <LoginPage />,
      errorElement: <RouteError scope="app" />,
    },
    { path: "/auth/callback", element: <OIDCCallbackPage /> },
    {
      element: <AuthGuard />,
      errorElement: <RouteError scope="app" />,
      children: [
        { path: "/no-access", element: <NoAccessPage /> },
        {
          element: <ConsoleGuard />,
          children: [
            {
              path: "/",
              element: <AppShell />,
              // Scoped to the layout so a broken screen leaves the shell and
              // its navigation usable.
              errorElement: <RouteError />,
              children: [
                { index: true, element: <Landing /> },
                {
                  element: <SuperAdminGuard />,
                  children: [
                    {
                      path: "workspaces",
                      element: (
                        <Suspense fallback={fallback}>
                          <WorkspacesPage />
                        </Suspense>
                      ),
                    },
                    {
                      path: "roles",
                      element: (
                        <Suspense fallback={fallback}>
                          <RolesPage />
                        </Suspense>
                      ),
                    },
                    {
                      path: "identity",
                      element: (
                        <Suspense fallback={fallback}>
                          <IdentityPage />
                        </Suspense>
                      ),
                    },
                    {
                      path: "tile-matrix-sets",
                      element: (
                        <Suspense fallback={fallback}>
                          <TileMatrixSetsPage />
                        </Suspense>
                      ),
                    },
                    {
                      path: "audit",
                      element: (
                        <Suspense fallback={fallback}>
                          <AuditPage />
                        </Suspense>
                      ),
                    },
                    {
                      path: "operations",
                      element: (
                        <Suspense fallback={fallback}>
                          <OperationsPage />
                        </Suspense>
                      ),
                    },
                    {
                      path: "deletions",
                      element: (
                        <Suspense fallback={fallback}>
                          <ServerDeletionsPage />
                        </Suspense>
                      ),
                    },
                  ],
                },
              ],
            },
            {
              // Sibling of the super-admin `/workspaces` list rather than a
              // child of it: entering a workspace requires console access, not
              // super admin, so the two cannot share a guard.
              path: "/workspaces/:ws",
              element: (
                <WorkspaceGuard>
                  <AppShell />
                </WorkspaceGuard>
              ),
              errorElement: <RouteError />,
              children: [
                { index: true, element: <WorkspaceOverview /> },
                {
                  path: "deletions",
                  element: (
                    <Suspense fallback={fallback}>
                      <DeletionOperationsPage />
                    </Suspense>
                  ),
                },
                {
                  path: "stores",
                  element: (
                    <Suspense fallback={fallback}>
                      <StoresPage />
                    </Suspense>
                  ),
                },
                {
                  path: "layers",
                  element: (
                    <Suspense fallback={fallback}>
                      <LayersPage />
                    </Suspense>
                  ),
                },
                {
                  path: "layer-groups",
                  element: (
                    <Suspense fallback={fallback}>
                      <LayerGroupsPage />
                    </Suspense>
                  ),
                },
                {
                  path: "coverages",
                  element: (
                    <Suspense fallback={fallback}>
                      <CoveragesPage />
                    </Suspense>
                  ),
                },
                {
                  path: "styles",
                  element: (
                    <Suspense fallback={fallback}>
                      <StyleEditorPage />
                    </Suspense>
                  ),
                },
                {
                  path: "imports",
                  element: (
                    <Suspense fallback={fallback}>
                      <ImportsPage />
                    </Suspense>
                  ),
                },
                {
                  path: "imports/:importId",
                  element: (
                    <Suspense fallback={fallback}>
                      <ImportDetailPage />
                    </Suspense>
                  ),
                },
                {
                  path: "settings",
                  element: (
                    <Suspense fallback={fallback}>
                      <SettingsPage />
                    </Suspense>
                  ),
                },
                {
                  path: "endpoints",
                  element: (
                    <Suspense fallback={fallback}>
                      <EndpointsPage />
                    </Suspense>
                  ),
                },
                {
                  path: "preview",
                  element: (
                    <Suspense fallback={fallback}>
                      <PreviewPage />
                    </Suspense>
                  ),
                },
                {
                  path: "caching",
                  element: (
                    <Suspense fallback={fallback}>
                      <TileCachePage />
                    </Suspense>
                  ),
                },
                // Earlier URLs of the two pages merged into Caching.
                {
                  path: "tile-cache",
                  element: <KeepSearchRedirect to="../caching" />,
                },
                {
                  path: "cache",
                  element: <KeepSearchRedirect to="../caching" />,
                },
                {
                  path: "api-keys",
                  element: (
                    <Suspense fallback={fallback}>
                      <APIKeysPage />
                    </Suspense>
                  ),
                },
                {
                  path: "claim-mappings",
                  element: (
                    <Suspense fallback={fallback}>
                      <ClaimMappingsPage />
                    </Suspense>
                  ),
                },
              ],
            },
          ],
        },
      ],
    },
    { path: "*", element: <Navigate to="/" replace /> },
  ],
  { basename: `${base.replace(/\/$/, "")}/admin` },
);
