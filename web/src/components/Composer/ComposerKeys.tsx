import { isMessageTargetSelection } from "../../lib/types";
import { useSelection } from "../../providers";

export function ComposerKeys() {
  const { selected } = useSelection();
  const targetText = isMessageTargetSelection(selected)
    ? `target: ${selected.kind} ${selected.id}`
    : "no target selected.";

  return (
    <div className="flex justify-between items-center gap-4 text-[11px] text-faint">
      <span>{targetText}</span>
      <div className="flex gap-3.5">
        <span>
          <kbd className="text-green">[enter]</kbd> send
        </span>
        <span>
          <kbd className="text-green">[ctrl+enter]</kbd> newline
        </span>
      </div>
    </div>
  );
}
