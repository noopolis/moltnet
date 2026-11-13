import { expect, it } from "vitest";
import { roomRecipients } from "../lib/recipients";

it("includes a disconnected registered Grok in an open writing room",()=>{
 expect(roomRecipients([{id:"grok-bot",connected:false}],{id:"general",name:"General",visibility:"public",write_policy:"registered_agents"})).toHaveLength(1);
});
it("limits member rooms to members and attached agents",()=>{
 expect(roomRecipients([{id:"member"},{id:"attached",rooms:["private"]},{id:"stranger"}],{id:"private",name:"Private",write_policy:"members",members:["member"]}).map(a=>a.id)).toEqual(["member","attached"]);
});
