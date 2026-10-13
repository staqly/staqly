"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { LayoutGrid, List, Table2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { Controller, useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { api, ApiError, type Collection, type CollectionView } from "@/lib/api";
import { toastApiError } from "@/lib/notify";
import { qk } from "@/lib/queries";

export const VIEW_OPTIONS: { value: CollectionView; label: string; Icon: typeof LayoutGrid }[] = [
  { value: "gallery", label: "Gallery", Icon: LayoutGrid },
  { value: "table", label: "Table", Icon: Table2 },
  { value: "list", label: "List", Icon: List },
];

const schema = z.object({
  name: z.string().trim().min(1, "Name is required").max(100, "At most 100 characters"),
  description: z.string().max(2000, "At most 2000 characters"),
  default_view: z.enum(["gallery", "table", "list"]),
});
type Values = z.infer<typeof schema>;

export function CreateCollectionDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (c: Collection) => void;
}) {
  const qc = useQueryClient();
  const router = useRouter();
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: "", description: "", default_view: "gallery" },
  });
  const { register, handleSubmit, formState, control, reset, setError } = form;

  const create = useMutation({
    mutationFn: (v: Values) =>
      api.createCollection({ name: v.name.trim(), description: v.description.trim(), default_view: v.default_view }),
    onSuccess: (c) => {
      void qc.invalidateQueries({ queryKey: qk.collectionsAll });
      toast.success(`Collection “${c.name}” created`);
      reset();
      onOpenChange(false);
      if (onCreated) onCreated(c);
      else router.push(`/collections/${c.id}`);
    },
    onError: (err) => {
      if (err instanceof ApiError && err.fields) {
        for (const [k, msg] of Object.entries(err.fields)) {
          if (k === "name" || k === "description" || k === "default_view") setError(k, { message: msg });
        }
      }
      toastApiError(err);
    },
  });

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) reset();
        onOpenChange(o);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleSubmit((v) => create.mutate(v))} className="grid gap-4" noValidate>
          <DialogHeader>
            <DialogTitle>New collection</DialogTitle>
            <DialogDescription>
              Agents add items to collections by ID or by exact name — pick something they can reference.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="gap-4">
            <Field data-invalid={!!formState.errors.name}>
              <FieldLabel htmlFor="collection-name">Name</FieldLabel>
              <Input
                id="collection-name"
                // oxlint-disable-next-line jsx-a11y/no-autofocus -- first field of a modal dialog (WAI-ARIA dialog pattern)
                autoFocus
                placeholder="Instagram Avatar"
                aria-invalid={!!formState.errors.name}
                {...register("name")}
              />
              <FieldError errors={[formState.errors.name]} />
            </Field>
            <Field data-invalid={!!formState.errors.description}>
              <FieldLabel htmlFor="collection-description">Description</FieldLabel>
              <Textarea
                id="collection-description"
                rows={3}
                placeholder="What lands here and who reviews it?"
                {...register("description")}
              />
              <FieldError errors={[formState.errors.description]} />
            </Field>
            <Field>
              <FieldLabel>Default view</FieldLabel>
              <Controller
                control={control}
                name="default_view"
                render={({ field }) => (
                  <ToggleGroup
                    type="single"
                    variant="outline"
                    value={field.value}
                    onValueChange={(v) => v && field.onChange(v)}
                    className="w-full"
                    aria-label="Default view"
                  >
                    {VIEW_OPTIONS.map(({ value, label, Icon }) => (
                      <ToggleGroupItem key={value} value={value} className="flex-1" aria-label={label}>
                        <Icon /> {label}
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                )}
              />
              <FieldDescription>
                New collections get Regenerate, Publish, Retry and Archive actions — edit them in settings.
              </FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? "Creating…" : "Create collection"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
