import { useDMs } from "../../hooks/useDMs";
import { useSelection } from "../../providers";
import { ListItem } from "../ListItem";

export function DirectChannelsList() {
  const { data: dms = [], directMessagesEnabled } = useDMs();
  const { selected, select } = useSelection();

  if (!directMessagesEnabled) return null;

  return (
    <>
        {dms.length === 0 ? (
          <p className="text-faint text-xs px-2 py-1.5">no channels connected.</p>
        ) : (
          <div className="flex flex-col">
            {dms.map((dm) => {
              const active = selected?.kind === "dm" && selected.id === dm.id;
              return (
                <ListItem
                  key={dm.id}
                  active={active}
                  onClick={() => select({ kind: "dm", id: dm.id })}
                  title={dm.id}
                  trailing={`${dm.message_count ?? 0} msgs`}
                />
              );
            })}
          </div>
        )}
      </>
  );
}
