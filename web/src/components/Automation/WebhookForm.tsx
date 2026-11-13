import { useState } from "react";
import { automationPath, controlRequest, type ConnectorStatus } from "../../lib/automation";

interface Props {
  room: string;
  agent: string;
  status?: ConnectorStatus;
  refresh: () => Promise<unknown>;
}
export function WebhookForm({ room, agent, status, refresh }: Props) {
  const [url, setURL] = useState("");
  const [key, setKey] = useState("");
  const [limit, setLimit] = useState(status?.max_runs_per_day || 30);
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState("");
  const path = `${automationPath(room)}/agents/${encodeURIComponent(agent)}/webhook`;
  async function act(test: boolean) {
    setBusy(true); setFeedback("");
    try {
      if (test) {
        await controlRequest(path + "/test", "POST", {});
        setFeedback("Webhook 已接受测试，实际运行结果请查看 Bot 客户端。测试会消耗一次唤醒额度。");
      } else {
        await controlRequest(path, "PUT", { url, key, max_runs_per_day: limit, enabled: true });
        setURL(""); setKey("");
        setFeedback("已保存到本机私有配置。点击测试验证 Routine。");
      }
      await refresh();
    } catch (error) { setFeedback(error instanceof Error ? error.message : "操作失败"); }
    finally { setBusy(false); }
  }
  return (
    <details className="mt-2 rounded border border-border p-3">
      <summary className="cursor-pointer text-sm">自动唤醒连接（Grok Routine / Webhook）</summary>
      <p className="my-2 text-xs text-mute">在 Grok Bot 的 Routine 中创建 Webhook 触发器，任务设为读取并回复本机 Moltnet。把 URL 和密钥填在这里，只需配置一次。</p>
      <form className="grid gap-2" onSubmit={(event) => { event.preventDefault(); void act(false); }}>
        <label className="text-xs">Webhook URL
          <input aria-label="Webhook URL" type="url" required={!status?.configured} value={url} onChange={(e) => setURL(e.target.value)} placeholder={status?.configured ? "留空保留原 URL" : "https://…"} className="mt-1 w-full rounded border border-border bg-bg px-2 py-1.5" />
        </label>
        <label className="text-xs">密钥{status?.configured ? "（留空保留原密钥）" : ""}
          <input aria-label="Webhook 密钥" type="password" autoComplete="new-password" required={!status?.configured} value={key} onChange={(e) => setKey(e.target.value)} className="mt-1 w-full rounded border border-border bg-bg px-2 py-1.5" />
        </label>
        <label className="text-xs">每日最多唤醒次数
          <input aria-label="每日最多唤醒次数" type="number" min={1} max={1000} required value={limit} onChange={(e) => setLimit(Number(e.target.value))} className="ml-2 w-20 rounded border border-border bg-bg px-2 py-1" />
        </label>
        <div className="flex flex-wrap gap-2 text-xs">
          <button disabled={busy} className="rounded border border-border px-3 py-1.5 disabled:opacity-50">保存连接</button>
          <button type="button" disabled={busy || !status?.configured || !status.enabled} onClick={() => void act(true)} className="rounded border border-border px-3 py-1.5 disabled:opacity-50">测试唤醒（会启动 AI）</button>
          <button type="button" disabled={busy || !status?.enabled} onClick={async () => {
            setBusy(true); setFeedback("");
            try { await controlRequest(path, "PUT", { url: "", key: "", max_runs_per_day: status?.max_runs_per_day || 30, enabled: false }); await refresh(); }
            catch (error) { setFeedback(error instanceof Error ? error.message : "停用失败"); }
            finally { setBusy(false); }
          }} className="rounded border border-border px-3 py-1.5 disabled:opacity-50">停用连接</button>
        </div>
      </form>
      {status?.last_status ? <p className="mt-2 text-xs text-mute">{status.last_status} · 今日 {status.attempts_today}/{status.max_runs_per_day}</p> : null}
      <p role="status" className="mt-2 text-xs text-sub">{feedback}</p>
    </details>
  );
}
