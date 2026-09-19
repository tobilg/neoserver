import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  getRoleDeletionPlan,
  deleteRole,
  getListRolesQueryKey,
  getListRolePoliciesQueryKey,
} from "@/api/generated/roles/roles";
import { Button } from "@/components/ui/button";
import { QueryError } from "@/components/QueryError";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/AppDialog";

export function RoleDeleteButton({
  id,
  system,
}: {
  id: string;
  system: boolean;
}) {
  const [open, setOpen] = useState(false);
  const client = useQueryClient();
  const plan = useQuery({
    queryKey: ["role-deletion-plan", id],
    queryFn: () => getRoleDeletionPlan(id),
    enabled: open,
    staleTime: 0,
  });
  const remove = useMutation({
    mutationFn: () => deleteRole(id),
    onSuccess: async () => {
      setOpen(false);
      await Promise.all([
        client.invalidateQueries({ queryKey: getListRolesQueryKey() }),
        client.invalidateQueries({ queryKey: getListRolePoliciesQueryKey(id) }),
      ]);
      toast.success(`Deleted role ${id}`);
    },
    onError: () => {
      void plan.refetch();
    },
    meta: { suppressErrorToast: true },
  });
  const dependencies = Object.entries(plan.data?.dependencies ?? {}).filter(
    ([, count]) => count > 0,
  );
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!remove.isPending) {
          setOpen(value);
          remove.reset();
        }
      }}
    >
      <DialogTrigger asChild>
        <Button
          size="sm"
          variant="ghost"
          disabled={system}
          title={
            system ? "Built-in roles cannot be deleted" : `Delete role ${id}`
          }
        >
          <Trash2 />
          Delete
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete role {id}?</DialogTitle>
          <DialogDescription>
            Deleting an unassigned role removes all its policies and permanently
            retires its ID. This cannot be undone.
          </DialogDescription>
        </DialogHeader>
        {plan.isFetching && <p>Checking assignments…</p>}
        <QueryError error={plan.error} retry={() => plan.refetch()} />
        {dependencies.length > 0 ? (
          <div>
            <p>Remove these assignments before deleting:</p>
            <ul className="list-inside list-disc">
              {dependencies.map(([kind, count]) => (
                <li key={kind}>
                  {kind.replaceAll("_", " ")}: {count}
                </li>
              ))}
            </ul>
          </div>
        ) : (
          plan.data && <p>No active credentials or publication assignments.</p>
        )}
        <QueryError error={remove.error} context="Role could not be deleted" />
        <DialogFooter>
          <Button
            variant="outline"
            disabled={remove.isPending}
            onClick={() => setOpen(false)}
          >
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={
              !plan.data ||
              plan.isFetching ||
              Boolean(plan.error) ||
              plan.data.is_system ||
              dependencies.length > 0 ||
              remove.isPending
            }
            onClick={() => remove.mutate()}
          >
            {remove.isPending ? "Deleting…" : "Delete role"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
