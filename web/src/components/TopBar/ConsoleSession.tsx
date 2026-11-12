import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNetwork } from "../../hooks/useNetwork";

export function ConsoleSession() {
  const { data: network } = useNetwork();
  const { data: session } = useQuery({
    queryKey: ["console-session"],
    queryFn: async () => {
      const response = await fetch("/v1/console/session");
      if (!response.ok) throw new Error("无法读取登录状态");
      return response.json() as Promise<{ local_owner_available?: boolean }>;
    },
    retry: false,
  });
  const [open, setOpen] = useState(false);
  const [token, setToken] = useState("");
  const [remember, setRemember] = useState(true);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    if (open) dialog.current?.showModal();
    else dialog.current?.close();
  }, [open]);

  async function login(local: boolean) {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      const response = await fetch("/v1/console/session", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ local, token: local ? undefined : token, remember }),
      });
      if (!response.ok) throw new Error(response.status === 401 ? "访问令牌无效，请检查后重试。" : "登录失败，请检查是否从本机地址访问。");
      setToken("");
      window.location.reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
    } finally {
      setPending(false);
    }
  }

  async function logout() {
    const response = await fetch("/v1/console/session", {
      method: "DELETE", headers: { "Content-Type": "application/json" },
    });
    if (response.ok) window.location.reload();
    else { setError("退出失败，请重试。"); setOpen(true); }
  }

  const signedIn = network?.console?.viewer?.kind === "token";
  const local = session?.local_owner_available === true;
  return <>
    <button className="shrink-0 rounded-lg border border-line px-3 py-1.5 text-[12px] hover:border-green-dim"
      onClick={() => signedIn ? void logout() : setOpen(true)}>
      {signedIn ? "退出登录" : "登录发消息"}
    </button>
    <dialog ref={dialog} onCancel={() => { setOpen(false); setToken(""); }}
      className="fixed inset-0 m-auto w-[min(420px,calc(100%-32px))] rounded-xl border border-line bg-panel p-6 text-ink backdrop:bg-black/70">
      <form onSubmit={(event) => { event.preventDefault(); void login(local); }} className="flex flex-col gap-4">
        <div className="flex items-center justify-between gap-3">
          <h2 className="font-semibold text-lg">登录 Moltnet</h2>
          <button type="button" aria-label="关闭登录" onClick={() => { setOpen(false); setToken(""); }}>×</button>
        </div>
        {local ? <p className="text-sub text-sm">这是你本机的 AI 群聊。以本机主人身份进入后，可以给全部 AI 或指定 AI 发消息。</p>
          : <label className="flex flex-col gap-2 text-sm">访问令牌
              <input type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} required
                className="rounded-lg border border-line bg-bg p-2.5 outline-none focus:border-green-dim" />
            </label>}
        <label className="flex items-center gap-2 text-sm text-sub">
          <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
          在此浏览器保持登录 30 天
        </label>
        {error && <p role="alert" className="text-coral text-sm">{error}</p>}
        <button type="submit" disabled={pending} className="rounded-lg bg-green px-4 py-2 font-semibold text-bg disabled:opacity-50">
          {pending ? "正在登录…" : local ? "以本机主人身份进入" : "登录"}
        </button>
      </form>
    </dialog>
  </>;
}
