import {
  Children,
  cloneElement,
  isValidElement,
  type ComponentProps,
  type ReactNode,
} from "react";
import * as Primitive from "@/components/ui/dialog";
import { cn } from "@/lib/utils";

export {
  Dialog,
  DialogTrigger,
  DialogClose,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";

export function DialogHeader(
  props: ComponentProps<typeof Primitive.DialogHeader>,
) {
  return (
    <Primitive.DialogHeader
      {...props}
      className={cn("shrink-0 pr-7", props.className)}
    />
  );
}

export function DialogFooter(
  props: ComponentProps<typeof Primitive.DialogFooter>,
) {
  return (
    <Primitive.DialogFooter
      {...props}
      className={cn("m-0 shrink-0 flex-wrap rounded-lg", props.className)}
    />
  );
}

/** Keep actions in view; only the fields scroll, including in short landscape windows. */
function layout(children: ReactNode): ReactNode {
  const nodes = Children.toArray(children);
  const form =
    nodes.length === 1 &&
    isValidElement<{ children?: ReactNode; className?: string }>(nodes[0]) &&
    nodes[0].type === "form"
      ? nodes[0]
      : null;
  if (form)
    return cloneElement(form, {
      className: cn("flex min-h-0 flex-col gap-4", form.props.className),
      children: layout(form.props.children),
    });
  const headers = nodes.filter(
    (node) => isValidElement(node) && node.type === DialogHeader,
  );
  const footers = nodes.filter(
    (node) => isValidElement(node) && node.type === DialogFooter,
  );
  const body = nodes.filter(
    (node) => !headers.includes(node) && !footers.includes(node),
  );
  return (
    <>
      {headers}
      <div
        data-dialog-body
        className="min-h-0 space-y-4 overflow-y-auto overscroll-contain px-1 py-1"
      >
        {body}
      </div>
      {footers}
    </>
  );
}

export function DialogContent({
  children,
  className,
  ...props
}: ComponentProps<typeof Primitive.DialogContent>) {
  return (
    <Primitive.DialogContent
      {...props}
      className={cn(
        "sm:max-w-lg",
        className,
        "flex max-h-[calc(100dvh-2rem)] min-w-0 flex-col overflow-hidden",
      )}
    >
      {layout(children)}
    </Primitive.DialogContent>
  );
}
