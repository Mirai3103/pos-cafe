import type { ReactElement } from "react";
import { RotateCcw } from "lucide-react";
import type { PreparationQueueCorrectionResponse } from "@/api/generated/models";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";

export interface CorrectionsLogProps {
  corrections: PreparationQueueCorrectionResponse[];
  busy: boolean;
  onRemake: (entry: PreparationQueueCorrectionResponse) => void;
}

export function CorrectionsLog({ corrections, busy, onRemake }: CorrectionsLogProps): ReactElement | null {
  if (corrections.length === 0) return null;

  const remadeWasteIds = new Set(
    corrections
      .filter((entry) => entry.entry_kind === "REMAKE" && entry.waste_id)
      .map((entry) => entry.waste_id),
  );

  return (
    <Accordion className="text-xs">
      <AccordionItem value="corrections">
        <AccordionTrigger>Hoạt động gần đây ({corrections.length})</AccordionTrigger>
        <AccordionContent keepMounted>
          <div className="flex flex-col gap-2">
            {corrections.map((entry) => (
              <div key={entry.id} className="flex items-center gap-2 py-1">
                <span className="w-14 shrink-0 font-mono text-2xs text-muted-foreground">
                  {entry.entry_kind === "WASTE" ? "Huỷ" : entry.entry_kind === "REMAKE" ? "Pha lại" : "Điều chỉnh"}
                </span>
                <span className="min-w-0 flex-1 truncate">
                  {`${entry.item_name} #${entry.unit_number} (Đơn #${entry.service_number})`}
                </span>
                {entry.entry_kind === "WASTE" && entry.id && !remadeWasteIds.has(entry.id) && (
                  <Button
                    type="button"
                    size="xs"
                    variant="outline"
                    aria-label="Pha lại món"
                    disabled={busy}
                    onClick={() => onRemake(entry)}
                  >
                    <RotateCcw className="mr-1 h-3 w-3" />
                    Pha lại
                  </Button>
                )}
              </div>
            ))}
          </div>
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}
