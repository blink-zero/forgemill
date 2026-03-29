import { LayoutGrid, List } from "lucide-react";
import { cn } from "@/lib/utils";
import { usePreferences } from "@/context/PreferencesContext";

export function ViewToggle() {
  const { getPreference, setPreference } = usePreferences();
  const viewMode = getPreference("view_mode", "cards");

  return (
    <div className="flex items-center border rounded-md overflow-hidden">
      <button
        onClick={() => setPreference("view_mode", "cards")}
        className={cn(
          "p-1.5 transition-colors",
          viewMode === "cards"
            ? "bg-primary text-primary-foreground"
            : "text-muted-foreground hover:text-foreground hover:bg-accent"
        )}
        title="Card view"
      >
        <LayoutGrid className="h-4 w-4" />
      </button>
      <button
        onClick={() => setPreference("view_mode", "table")}
        className={cn(
          "p-1.5 transition-colors",
          viewMode === "table"
            ? "bg-primary text-primary-foreground"
            : "text-muted-foreground hover:text-foreground hover:bg-accent"
        )}
        title="Table view"
      >
        <List className="h-4 w-4" />
      </button>
    </div>
  );
}
