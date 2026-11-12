import { act, render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LexicalComposer } from "@lexical/react/LexicalComposer";
import { useLexicalComposerContext } from "@lexical/react/LexicalComposerContext";
import { ClearEditorPlugin } from "@lexical/react/LexicalClearEditorPlugin";
import { $getRoot, $createParagraphNode, $createTextNode, $createLineBreakNode, KEY_ENTER_COMMAND, type LexicalEditor } from "lexical";
import { beforeEach, expect, it, vi } from "vitest";
import { SubmitPlugin } from "../components/Composer/SubmitPlugin";
import { api } from "../lib/api";

vi.mock("../providers",()=>({useSelection:()=>({selected:{kind:"room",id:"general"}})}));
vi.mock("../hooks/useDMs",()=>({useDMs:()=>({data:[]})}));
vi.mock("../hooks/useNetwork",()=>({useNetwork:()=>({data:{capabilities:{direct_messages:true}}})}));
vi.mock("../lib/api",async(importOriginal)=>({...await importOriginal<typeof import("../lib/api")>(),api:{sendMessage:vi.fn()}}));

let editor: LexicalEditor;
function Capture(){[editor]=useLexicalComposerContext();return null;}
function mount(recipient="grok-bot"){
 const client=new QueryClient({defaultOptions:{mutations:{retry:false}}});
 render(<QueryClientProvider client={client}><LexicalComposer initialConfig={{namespace:"test-owner-send",onError:err=>{throw err;}}}>
   <Capture/><ClearEditorPlugin/><SubmitPlugin recipient={recipient}/>
 </LexicalComposer></QueryClientProvider>);
}
async function type(text:string){await act(async()=>{editor.update(()=>{$getRoot().clear().append($createParagraphNode().append($createTextNode(text)));},{discrete:true});});}
beforeEach(()=>{vi.mocked(api.sendMessage).mockReset();});

it("sends owner identity and selected AI, suppresses double send, allows same text after success",async()=>{
 let resolve!: (value:never)=>void;
 vi.mocked(api.sendMessage).mockImplementationOnce(()=>new Promise(done=>{resolve=done;}));
 mount();await type("你好");
 act(()=>{editor.dispatchCommand(KEY_ENTER_COMMAND,new KeyboardEvent("keydown",{key:"Enter"}));editor.dispatchCommand(KEY_ENTER_COMMAND,new KeyboardEvent("keydown",{key:"Enter"}));});
 await waitFor(()=>expect(api.sendMessage).toHaveBeenCalledTimes(1));
 const first=vi.mocked(api.sendMessage).mock.calls[0][0];
 expect(first.from).toEqual({type:"human",id:"operator",name:"我"});
 expect(first.mentions).toEqual(["grok-bot"]);
 expect(first.target).toEqual({kind:"room",room_id:"general"});
 await act(async()=>{resolve({} as never);});
 await waitFor(()=>expect((screen.getByRole("button",{name:"发送消息"}) as HTMLButtonElement).disabled).toBe(false));
 vi.mocked(api.sendMessage).mockResolvedValue({} as never);
 await type("你好");fireEvent.click(screen.getByRole("button",{name:"发送消息"}));
 await waitFor(()=>expect(api.sendMessage).toHaveBeenCalledTimes(2));
 expect(vi.mocked(api.sendMessage).mock.calls[1][0].id).not.toBe(first.id);
});

it("retries with the same ID and leaves broadcast mentions empty",async()=>{
 vi.mocked(api.sendMessage).mockRejectedValueOnce(new Error("网络暂时中断"));
 mount("");await type("给全部 AI 的问题");fireEvent.click(screen.getByRole("button",{name:"发送消息"}));
 await screen.findByText("网络暂时中断");
 const first=vi.mocked(api.sendMessage).mock.calls[0][0];expect(first.mentions).toBeUndefined();
 vi.mocked(api.sendMessage).mockResolvedValue({} as never);
 fireEvent.click(screen.getByRole("button",{name:"发送消息"}));
 await waitFor(()=>expect(api.sendMessage).toHaveBeenCalledTimes(2));
 expect(vi.mocked(api.sendMessage).mock.calls[1][0].id).toBe(first.id);
});

it("preserves a line break inserted inside a paragraph",async()=>{
 vi.mocked(api.sendMessage).mockResolvedValue({} as never);mount();
 await act(async()=>{editor.update(()=>{$getRoot().clear().append($createParagraphNode().append($createTextNode("第一行"),$createLineBreakNode(),$createTextNode("第二行")));},{discrete:true});});
 fireEvent.click(screen.getByRole("button",{name:"发送消息"}));
 await waitFor(()=>expect(api.sendMessage).toHaveBeenCalledTimes(1));
 expect(vi.mocked(api.sendMessage).mock.calls[0][0].parts).toEqual([{kind:"text",text:"第一行\n第二行"}]);
});
