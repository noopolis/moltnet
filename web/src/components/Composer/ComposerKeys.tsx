import { isMessageTargetSelection } from "../../lib/types";
import { useSelection } from "../../providers";

export function ComposerKeys() {
  const { selected } = useSelection();
  const targetText = isMessageTargetSelection(selected)
    ? `我 → ${selected.id}`
    : "请选择对话";

  return (
    <div className="flex justify-between items-center gap-4 px-1 pt-1.5 font-mono text-[11px] text-faint">
      <span className="truncate">{targetText}</span>
      <div className="flex gap-3.5 shrink-0">
        <span>
          <kbd className="text-mute">Enter</kbd> 发送
        </span>
        <span>
          <kbd className="text-mute">Shift+Enter</kbd> 换行
        </span>
        <span>
          <kbd className="text-mute">@</kbd> 指定 AI
        </span>
      </div>
    </div>
  );
}
