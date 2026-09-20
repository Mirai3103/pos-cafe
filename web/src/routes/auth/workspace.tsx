import { createFileRoute } from "@tanstack/react-router";
import { WorkspacePicker } from "@/features/auth/components/workspace-picker";

export const Route = createFileRoute("/auth/workspace")({
  component: WorkspacePicker,
});
