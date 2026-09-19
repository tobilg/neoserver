import { useNavigate } from "react-router";
import { useAuth } from "@/auth/auth-context";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { useCatalogChoices } from "@/features/catalog/use-catalog-choices";
import type { CommandDestination } from "./CommandMenu";

/** Palette body, loaded on first use to keep cmdk out of the initial bundle. */
export default function CommandPalette({
  open,
  onOpenChange,
  workspace,
  pages,
  query,
  onQueryChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  workspace?: string;
  pages: CommandDestination[];
  query: string;
  onQueryChange: (query: string) => void;
}) {
  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Go to"
      description="Search pages, workspaces and publications"
    >
      {open && (
        // CommandDialog renders no cmdk root of its own.
        <Command>
          <CommandContent
            workspace={workspace}
            pages={pages}
            query={query}
            onQueryChange={onQueryChange}
            close={() => onOpenChange(false)}
          />
        </Command>
      )}
    </CommandDialog>
  );
}

function CommandContent({
  workspace,
  pages,
  query,
  onQueryChange,
  close,
}: {
  workspace?: string;
  pages: CommandDestination[];
  query: string;
  onQueryChange: (query: string) => void;
  close: () => void;
}) {
  const navigate = useNavigate();
  const { me } = useAuth();
  const go = (to: string) => {
    close();
    navigate(to);
  };
  return (
    <>
      <CommandInput
        autoFocus
        value={query}
        onValueChange={onQueryChange}
        placeholder="Search pages, workspaces, layers…"
      />
      <CommandList>
        <CommandEmpty>No matching page or publication.</CommandEmpty>
        <CommandGroup heading="Pages">
          {pages.map((page) => (
            <CommandItem
              key={page.to}
              value={`page ${page.label}`}
              onSelect={() => go(page.to)}
            >
              {page.label}
            </CommandItem>
          ))}
        </CommandGroup>
        {workspace && <PublicationItems workspace={workspace} onSelect={go} />}
        {(me?.workspaces.length ?? 0) > 1 && (
          <CommandGroup heading="Workspaces">
            {me?.workspaces
              .filter((item) => item.console_access)
              .map((item) => (
                <CommandItem
                  key={item.id}
                  value={`workspace ${item.name}`}
                  onSelect={() =>
                    go(`/workspaces/${encodeURIComponent(item.name)}`)
                  }
                >
                  {item.name}
                  <span className="ml-auto text-xs text-muted-foreground">
                    {item.role}
                  </span>
                </CommandItem>
              ))}
          </CommandGroup>
        )}
      </CommandList>
    </>
  );
}

function PublicationItems({
  workspace,
  onSelect,
}: {
  workspace: string;
  onSelect: (to: string) => void;
}) {
  const catalog = useCatalogChoices(workspace);
  const root = `/workspaces/${encodeURIComponent(workspace)}`;
  if (!catalog.allResources.length) return null;
  return (
    <CommandGroup heading="Publications">
      {catalog.allResources.map((resource) => (
        <CommandItem
          key={`${resource.kind}-${resource.public_id}`}
          value={`${resource.kind} ${resource.public_id} ${resource.title ?? ""}`}
          onSelect={() =>
            onSelect(
              `${root}/preview?${new URLSearchParams({
                layers: resource.public_id,
                ...(resource.kind === "feature" ? {} : { sources: "wms" }),
              })}`,
            )
          }
        >
          {resource.title || resource.public_id}
          <span className="ml-auto font-mono text-xs text-muted-foreground">
            {resource.kind} · preview
          </span>
        </CommandItem>
      ))}
    </CommandGroup>
  );
}
