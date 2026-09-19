import {
  Fragment,
  useEffect,
  useEffectEvent,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import {
  Link,
  NavLink,
  Outlet,
  useLocation,
  useNavigate,
  useParams,
} from "react-router";
import {
  Activity,
  BookOpen,
  Database,
  Fingerprint,
  FolderTree,
  Grid3X3,
  Group,
  KeyRound,
  Layers,
  LayoutGrid,
  LinkIcon,
  LogOut,
  Map,
  Monitor,
  Moon,
  Palette,
  Ruler,
  ScrollText,
  ShieldCheck,
  SlidersHorizontal,
  Sun,
  Trash2,
  Upload,
  UserCheck,
  Waypoints,
} from "lucide-react";
import { basePath } from "@/api/client";
import { useAuth } from "@/auth/auth-context";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarSeparator,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { CommandMenu, type CommandDestination } from "./CommandMenu";
import { readinessQuery } from "@/lib/health";
import { useGetImport } from "@/api/generated/imports/imports";
import { cn } from "@/lib/utils";
import { readPreference, writePreference } from "@/lib/preferences";

/** Viewport width below which the sidebar starts collapsed. */
const SIDEBAR_RAIL_BELOW = 1100;

const workspaceGroups = [
  {
    label: "Data",
    items: [
      ["Overview", "", Waypoints],
      ["Stores", "stores", Database],
      ["Imports", "imports", Upload],
      ["Layers", "layers", Layers],
      ["Layer groups", "layer-groups", Group],
      ["Coverages", "coverages", Grid3X3],
      ["Styles", "styles", Palette],
    ],
  },
  {
    label: "Publish",
    items: [
      ["Service settings", "settings", SlidersHorizontal],
      ["Preview", "preview", Map],
      ["Endpoints", "endpoints", LinkIcon],
      ["API keys", "api-keys", KeyRound],
    ],
  },
  {
    label: "Operate",
    collapsible: true,
    items: [
      ["Caching", "caching", LayoutGrid],
      ["Deletions", "deletions", Trash2],
      ["Claim mappings", "claim-mappings", UserCheck],
    ],
  },
] as const;

/**
 * Names the workspace section for a path. Detail routes such as
 * `imports/<id>` belong to their list section, and a trailing slash is ignored.
 */
function workspaceSection(pathname: string, workspace: string) {
  const prefix = `/workspaces/${encodeURIComponent(workspace)}`;
  const rest = pathname.startsWith(prefix)
    ? pathname.slice(prefix.length).replace(/^\/+/, "")
    : "";
  const section = rest.split("/")[0] ?? "";
  for (const group of workspaceGroups)
    for (const [label, suffix] of group.items)
      if (suffix === section) return label;
  return "Workspace";
}

const serverItems = [
  ["Workspaces", "/workspaces", FolderTree],
  ["Roles & policies", "/roles", ShieldCheck],
  ["Identity", "/identity", Fingerprint],
  ["Tile matrix sets", "/tile-matrix-sets", Ruler],
  ["Deletions", "/deletions", Trash2],
  ["Audit", "/audit", ScrollText],
  ["Operations", "/operations", Activity],
] as const;

function ThemeButton() {
  type Theme = "system" | "light" | "dark";
  const [theme, setTheme] = useState<Theme>(() => {
    const saved = readPreference("theme", "system");
    return saved === "light" || saved === "dark" ? saved : "system";
  });
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () =>
      document.documentElement.classList.toggle(
        "dark",
        theme === "dark" || (theme === "system" && media.matches),
      );
    apply();
    writePreference("theme", theme);
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [theme]);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={`Color theme: ${theme}. Change theme`}
        >
          {theme === "system" ? (
            <Monitor />
          ) : theme === "dark" ? (
            <Moon />
          ) : (
            <Sun />
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>Color theme</DropdownMenuLabel>
        <DropdownMenuRadioGroup
          value={theme}
          onValueChange={(value) => setTheme(value as Theme)}
        >
          {(["light", "dark", "system"] as const).map((value) => (
            <DropdownMenuRadioItem key={value} value={value}>
              {value[0].toUpperCase() + value.slice(1)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * A collapsible sidebar section. The icon rail hides the summary, so there the
 * section is always open and its icons stay reachable; the user's choice
 * returns when the sidebar expands again.
 */
function NavDisclosure({
  label,
  defaultOpen,
  summaryClassName,
  children,
}: {
  label: string;
  defaultOpen: boolean;
  summaryClassName?: string;
  children: ReactNode;
}) {
  const { state, isMobile } = useSidebar();
  const rail = state === "collapsed" && !isMobile;
  const [open, setOpen] = useState(defaultOpen);
  return (
    <details
      open={rail || open}
      onToggle={(event) => {
        if (!rail) setOpen(event.currentTarget.open);
      }}
    >
      <summary
        className={cn(
          "cursor-pointer text-xs font-medium group-data-[collapsible=icon]:hidden",
          summaryClassName,
        )}
      >
        {label}
      </summary>
      {children}
    </details>
  );
}

/** A blocked transition never changes location, so it keeps the sheet open. */
function MobileNavigationSync() {
  const location = useLocation();
  const previous = useRef(location.key);
  const { isMobile, openMobile, setOpenMobile } = useSidebar();
  const completeNavigation = useEffectEvent(() => {
    if (!isMobile || !openMobile) return;
    setOpenMobile(false);
    // Wait for the sheet's exit and focus restoration before focusing the new
    // heading. Observe the DOM instead of guessing its animation duration.
    const focusDestination = () => {
      if (
        document.querySelector('[data-sidebar="sidebar"][data-mobile="true"]')
      )
        return;
      const heading = document.querySelector<HTMLElement>("main h1");
      if (!heading) return;
      heading.focus({ preventScroll: true });
      observer.disconnect();
    };
    const observer = new MutationObserver(focusDestination);
    observer.observe(document.body, { childList: true, subtree: true });
    focusDestination();
    return () => observer.disconnect();
  });
  useEffect(() => {
    if (previous.current === location.key) return;
    previous.current = location.key;
    return completeNavigation();
  }, [location.key]);
  return null;
}

export function AppShell() {
  const { me, config, logout } = useAuth();
  const { ws } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  // Without a saved choice, start with the icon rail on narrow desktops so
  // tables keep enough width for their columns and actions.
  const [sidebarOpen, setSidebarOpen] = useState(() => {
    const saved = readPreference("sidebar", "");
    return saved
      ? saved !== "closed"
      : !window.matchMedia(`(max-width: ${SIDEBAR_RAIL_BELOW - 1}px)`).matches;
  });
  const readiness = useQuery(readinessQuery);
  const current = me?.workspaces.find((workspace) => workspace.name === ws);
  const ready = readiness.error
    ? "unavailable"
    : (readiness.data?.status ?? "checking");
  return (
    <TooltipProvider>
      <SidebarProvider
        open={sidebarOpen}
        onOpenChange={(value) => {
          setSidebarOpen(value);
          writePreference("sidebar", value ? "open" : "closed");
        }}
      >
        <a
          href="#main-content"
          className="sr-only z-50 rounded-md bg-background px-3 py-2 text-sm shadow focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
          onClick={(event) => {
            event.preventDefault();
            const target =
              document.querySelector<HTMLElement>("main h1") ??
              document.getElementById("main-content");
            target?.focus();
          }}
        >
          Skip to content
        </a>
        <MobileNavigationSync />
        <Sidebar collapsible="icon" role="navigation" aria-label="Console">
          {/* Same height as the page header, so both bottom borders line up. */}
          <SidebarHeader className="h-13 shrink-0 justify-center border-b py-0">
            <Link
              to="/"
              aria-label="neoserver home"
              className="flex items-center gap-2 px-2 py-1"
            >
              <Waypoints className="size-5 text-primary" />
              <span className="font-semibold group-data-[collapsible=icon]:hidden">
                neoserver
              </span>
            </Link>
          </SidebarHeader>
          {me && me.workspaces.length > 0 && (
            <div className="shrink-0 border-b p-2 group-data-[collapsible=icon]:hidden">
              <Select
                value={current?.name}
                onValueChange={(value) =>
                  navigate(`/workspaces/${encodeURIComponent(value)}`)
                }
              >
                <SelectTrigger aria-label="Workspace" className="w-full">
                  <SelectValue placeholder="Select workspace" />
                </SelectTrigger>
                <SelectContent>
                  {me.workspaces
                    .filter((item) => item.console_access)
                    .map((workspace) => (
                      <SelectItem key={workspace.id} value={workspace.name}>
                        {workspace.name} · {workspace.role}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
          )}
          <SidebarContent>
            {ws &&
              workspaceGroups.map((group) => {
                const items = group.items
                  .filter(
                    ([label]) => label !== "Coverages" || config?.services.wcs,
                  )
                  .map(([label, suffix, Icon]) => {
                    const target = `/workspaces/${encodeURIComponent(ws)}${suffix ? `/${suffix}` : ""}`;
                    return (
                      <SidebarMenuItem key={label}>
                        <SidebarMenuButton
                          asChild
                          tooltip={label}
                          isActive={
                            workspaceSection(location.pathname, ws) === label
                          }
                        >
                          <NavLink to={target} end>
                            <Icon />
                            <span>{label}</span>
                          </NavLink>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    );
                  });
                const holdsCurrent = group.items.some(
                  ([label]) =>
                    workspaceSection(location.pathname, ws) === label,
                );
                return (
                  <SidebarGroup key={group.label}>
                    {"collapsible" in group ? (
                      <NavDisclosure
                        key={`${group.label}-${holdsCurrent}`}
                        label={group.label}
                        defaultOpen={holdsCurrent}
                        summaryClassName="list-inside rounded-md px-2 py-1.5 text-sidebar-foreground/70"
                      >
                        <SidebarGroupContent>
                          <SidebarMenu>{items}</SidebarMenu>
                        </SidebarGroupContent>
                      </NavDisclosure>
                    ) : (
                      <>
                        <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
                        <SidebarGroupContent>
                          <SidebarMenu>{items}</SidebarMenu>
                        </SidebarGroupContent>
                      </>
                    )}
                  </SidebarGroup>
                );
              })}
            {me?.super_admin && (
              <Fragment>
                <SidebarSeparator />
                <SidebarGroup>
                  <NavDisclosure
                    key={ws ? "workspace" : "server"}
                    label="Server administration"
                    defaultOpen={!ws}
                    summaryClassName="px-2 py-2 text-muted-foreground"
                  >
                    <SidebarGroupContent>
                      <SidebarMenu>
                        {serverItems.map(([label, target, Icon]) => (
                          <SidebarMenuItem key={label}>
                            <SidebarMenuButton
                              asChild
                              tooltip={label}
                              isActive={location.pathname === target}
                            >
                              <NavLink to={target} end>
                                <Icon />
                                <span>{label}</span>
                              </NavLink>
                            </SidebarMenuButton>
                          </SidebarMenuItem>
                        ))}
                        <SidebarMenuItem>
                          <SidebarMenuButton asChild tooltip="API reference">
                            <a
                              href={`${basePath}/api/v1/api.html`}
                              target="_blank"
                              rel="noreferrer"
                            >
                              <BookOpen />
                              <span>API reference</span>
                            </a>
                          </SidebarMenuButton>
                        </SidebarMenuItem>
                      </SidebarMenu>
                    </SidebarGroupContent>
                  </NavDisclosure>
                </SidebarGroup>
              </Fragment>
            )}
          </SidebarContent>
          <SidebarFooter className="border-t">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  aria-label="Account menu"
                  className="justify-start overflow-hidden px-2"
                >
                  <span className="size-2 shrink-0 rounded-full bg-success" />
                  <span className="truncate group-data-[collapsible=icon]:hidden">
                    {me?.display_name || me?.subject}
                  </span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent side="right" align="end">
                <DropdownMenuLabel>
                  {me?.email || me?.auth_method}
                </DropdownMenuLabel>
                {config && (
                  <SignInMethods
                    config={config}
                    linkToIdentity={me?.super_admin === true}
                  />
                )}
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={() =>
                    void logout()
                      .then(() => navigate("/login"))
                      .catch(() =>
                        toast.error("Sign-out failed", {
                          description:
                            "Your session could not be revoked. Please retry End session.",
                        }),
                      )
                  }
                >
                  <LogOut /> End session
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarFooter>
          <SidebarRail />
        </Sidebar>
        <SidebarInset id="main-content" tabIndex={-1} className="min-w-0">
          <header className="sticky top-0 z-10 flex h-13 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur">
            <SidebarTrigger />
            <div className="h-4 w-px bg-border" />
            <Breadcrumb
              workspace={current ? ws : undefined}
              workspaceLabel={current?.name}
              pathname={location.pathname}
            />
            <CommandMenu
              workspace={current?.name}
              pages={commandPages(current?.name, me?.super_admin === true)}
            />
            {/* Readiness only takes space when something needs attention. */}
            <span
              role="status"
              aria-label={
                ready === "ready"
                  ? "Server ready"
                  : ready === "checking"
                    ? "Checking server readiness"
                    : "Server not ready"
              }
              title={ready === "ready" ? "Server ready" : undefined}
              className={cn(
                "flex items-center gap-2 text-xs",
                ready === "ready"
                  ? "text-success"
                  : ready === "checking"
                    ? "text-muted-foreground"
                    : "font-medium text-destructive",
              )}
            >
              <span className="size-2 rounded-full bg-current" />
              {ready !== "ready" && (
                <span className="hidden sm:inline">
                  {ready === "checking" ? "Checking…" : "Server not ready"}
                </span>
              )}
            </span>
            <ThemeButton />
          </header>
          <Outlet />
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  );
}

function commandPages(workspace: string | undefined, superAdmin: boolean) {
  const pages: CommandDestination[] = [];
  if (workspace)
    for (const group of workspaceGroups)
      for (const [label, suffix] of group.items)
        pages.push({
          label,
          to: `/workspaces/${encodeURIComponent(workspace)}${suffix ? `/${suffix}` : ""}`,
        });
  if (superAdmin)
    for (const [label, target] of serverItems)
      pages.push({ label: `Server · ${label}`, to: target });
  return pages;
}

/** Linked trail: workspace › section › detail. */
function Breadcrumb({
  workspace,
  workspaceLabel,
  pathname,
}: {
  workspace?: string;
  workspaceLabel?: string;
  pathname: string;
}) {
  const crumbs: { label: string; to?: string }[] = [];
  const root = `/workspaces/${encodeURIComponent(workspace ?? "")}`;
  const rest = workspace
    ? pathname.slice(root.length).replace(/^\/+|\/+$/g, "")
    : "";
  const [suffix, ...detail] = rest.split("/");
  const importID =
    suffix === "imports" && detail.length === 1
      ? decodeURIComponent(detail[0])
      : "";
  // Shares the detail page's cache entry, so the crumb shows the import's
  // name instead of its ID without an extra request.
  const importJob = useGetImport(workspace ?? "", importID, {
    query: { enabled: Boolean(importID) },
  });
  const detailLabel = importID ? importJob.data?.name : undefined;
  if (workspace) {
    const section = workspaceSection(pathname, workspace);
    crumbs.push({
      label: workspaceLabel ?? workspace,
      to: rest ? root : undefined,
    });
    if (rest)
      crumbs.push({
        label: section,
        to: detail.length ? `${root}/${suffix}` : undefined,
      });
    if (detail.length)
      crumbs.push({
        label: detailLabel ?? decodeURIComponent(detail.join("/")),
      });
  } else {
    crumbs.push({ label: "Server", to: "/workspaces" });
    crumbs.push({
      label:
        serverItems.find(([, target]) => target === pathname)?.[0] ??
        "Overview",
    });
  }
  return (
    <nav aria-label="Breadcrumb" className="min-w-0 flex-1">
      <ol className="flex min-w-0 items-center gap-1 font-mono text-xs text-muted-foreground">
        {crumbs.map((crumb, index) => (
          <li key={index} className="flex min-w-0 items-center gap-1">
            {index > 0 && <span aria-hidden="true">/</span>}
            {crumb.to ? (
              <Link
                to={crumb.to}
                className="truncate underline-offset-4 hover:text-foreground hover:underline"
              >
                {crumb.label}
              </Link>
            ) : (
              <span aria-current="page" className="truncate text-foreground">
                {crumb.label}
              </span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}

/** Replaces the old warning banner: states which sign-in methods exist. */
function SignInMethods({
  config,
  linkToIdentity,
}: {
  config: NonNullable<ReturnType<typeof useAuth>["config"]>;
  linkToIdentity: boolean;
}) {
  const methods = [
    config.auth.password_login && "password",
    config.auth.token_login && "token",
    config.auth.oidc.enabled && "OIDC",
  ].filter(Boolean);
  const label = `Sign-in methods: ${methods.join(", ") || "none"}`;
  return linkToIdentity ? (
    <DropdownMenuItem asChild>
      <Link to="/identity" className="text-xs text-muted-foreground">
        {label}
      </Link>
    </DropdownMenuItem>
  ) : (
    <p className="px-2 pb-1.5 text-xs text-muted-foreground">{label}</p>
  );
}
