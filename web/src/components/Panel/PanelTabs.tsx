export interface PanelTab<K extends string = string> {
  key: K;
  label: string;
}

interface PanelTabsProps<K extends string> {
  tabs: PanelTab<K>[];
  active: K;
  onSelect: (key: K) => void;
}

/**
 * Tabs rendered as panel legends riding the top border, matching the
 * `[ ROOMS ]` treatment PanelTitle uses. The parent PanelHeader is
 * pointer-events-none so the border stays inert; each tab opts back in.
 */
export function PanelTabs<K extends string>({
  tabs,
  active,
  onSelect,
}: PanelTabsProps<K>) {
  return (
    <div className="flex items-center gap-1 min-w-0" role="tablist">
      {tabs.map((tab) => {
        const isActive = tab.key === active;
        return (
          <button
            key={tab.key}
            type="button"
            role="tab"
            aria-selected={isActive}
            onClick={() => onSelect(tab.key)}
            className={`pointer-events-auto shrink-0 cursor-pointer whitespace-nowrap bg-bg py-1.5 -my-1.5 text-[11px] tracking-[0.12em] transition-colors ${
              isActive
                ? "px-2 text-green"
                : "px-1.5 text-faint hover:text-mute"
            }`}
          >
            {isActive ? `[ ${tab.label} ]` : tab.label}
          </button>
        );
      })}
    </div>
  );
}
