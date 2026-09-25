import type { ReactElement } from "react";
import { AvailabilityToggle } from "./availability-toggle";
import type { AvailabilityGroupView, AvailabilityKind } from "../lib/availability";

export interface AvailabilityToppingsProps {
  groups: AvailabilityGroupView[];
  onToggle: (kind: AvailabilityKind, id: string, next: boolean) => void;
}

export function AvailabilityToppings({ groups, onToggle }: AvailabilityToppingsProps): ReactElement | null {
  if (groups.length === 0) return null;
  return (
    <section className="flex flex-col gap-4">
      <h3 className="text-sm font-bold text-foreground">Topping</h3>
      {groups.map((group) => (
        <div key={group.id} className="rounded-2xl border border-border bg-card p-4">
          <h4 className="mb-3 text-xs font-bold uppercase tracking-wider text-muted-foreground">{group.name}</h4>
          <div className="flex flex-wrap gap-2">
            {group.options.map((option) => (
              <AvailabilityToggle
                key={option.id}
                size="chip"
                checked={option.available}
                label={option.name}
                onChange={(next) => onToggle("modifier_option", option.id, next)}
              />
            ))}
          </div>
        </div>
      ))}
    </section>
  );
}
