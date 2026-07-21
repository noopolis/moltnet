package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

var machineRequestGoldens = map[string]struct {
	raw string
	sha string
}{
	"send_nudge_request": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_send_1","operation":"send_nudge","send_nudge":{"delivery_id":"delivery_1","target":{"kind":"room","id":"room_1"},"body":"wake for nudge","origin_message_id":"origin_1","cause_event_ids":["ev_1","ev_2"]}}`,
		sha: "b91e932007623375878d5c229521c9f6d9f069d843ff505280b9f4132efaf6b4",
	},
	"read_request": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_read_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"limit":20,"after":"msg_1"}}`,
		sha: "995ae29cae52959ec3cd9dc23af5f5372fdde5a191007e9e01aee7d3f46a98d0",
	},
	"subscribe_request": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_sub_1","operation":"subscribe","subscribe":{"target":{"kind":"room","id":"room_1"},"resume_cursor":"cursor_1","max_events":25}}`,
		sha: "89f88547d715fbde9f95f6ca5f19dadbf3ba72706aa30983da539f6dcfd15dd2",
	},
	"export_request": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_exp_1","operation":"export","export":{"room_ids":["room_1","room_2"],"dm_peer_ids":["peer_1"],"include_social_speech":true}}`,
		sha: "282459bd4200672eb468c9358059383447a40409c9330994bebe88b984db4df5",
	},
	"cancel_request": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_can_1","operation":"cancel","cancel":{"target_correlation_id":"corr_read_1"}}`,
		sha: "243b527b184b44ee9b13a1391aa03067e8ca0255e0dc8fa03d1fce706e535ae4",
	},
}

var machineResponseGoldens = map[string]struct {
	raw string
	sha string
}{
	"send_nudge_success": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_send_1","operation":"send_nudge","send_nudge":{"message_id":"message_1","event_id":"event_1","accepted":true,"thread_id":"thread_1","thread_created":true,"dm_created":false}}`,
		sha: "061143f0a26c5dc2ec3f3d5a781dfa08e754ec9760ec1c40a3aa11f42fa933ec",
	},
	"read_success_nonempty_with_after": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_read_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"page":{"messages":[{"id":"msg_2","network_id":"net_1","origin":{"network_id":"net_1","message_id":"msg_1"},"target":{"kind":"room","room_id":"room_1"},"from":{"type":"agent","id":"agent_1"},"parts":[{"kind":"text","text":"hello"}],"mentions":["agent_2"],"created_at":"2026-07-21T00:00:00Z"}],"page":{"has_more":true,"next_after":"msg_3"}}}}`,
		sha: "1cf6b11be03b57dcfb921228fdf3f7a6bc9acb0cef427bd45afa227d66d4d9aa",
	},
	"subscribe_event": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_sub_1","operation":"subscribe","event":{"event_id":"e1","type":"message","payload":{"message":"m"}}}`,
		sha: "df2df6cc41e19605eb325f64cda95da1ed5dd2e167bf2ede3db1e522ab1193e9",
	},
	"subscribe_done": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_sub_2","operation":"subscribe","subscribe":{"closed":"closed","reason":"done"}}`,
		sha: "dda3481796a435c1a9577a7c6a49fc20e6647730d861db7010588004a30d34ba",
	},
	"subscribe_limit": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_sub_3","operation":"subscribe","subscribe":{"closed":"closed","reason":"limit"}}`,
		sha: "b69456b12f7db90fccb8b8fc0f5ab28caaac8b6ff7985ae91d370908f7661812",
	},
	"subscribe_eof": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_sub_4","operation":"subscribe","subscribe":{"closed":"closed","reason":"eof"}}`,
		sha: "894e6c7c0de2fffb1829b6640fb9fc8c1f5c8b25347f5fdb9de517625b12f508",
	},
	"export_success": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_exp_1","operation":"export","export":{"version":"moltnet.machine.export.v1","control_marker":"moltnet.control.nudge.v1","transcript":"one\nline","transcript_sha256":"0207e4765bc7aeff18d5235cd7f2cf20d0787148585a8f73061bd06dab269d21"}}`,
		sha: "9cf0d074cd7b328e278f84ba6ac49fda93074dfa103db6c923354a91bd96331d",
	},
	"export_unsupported": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_exp_2","operation":"export","error":{"code":"unsupported"}}`,
		sha: "123d10d7fbb876c97d494920777f894c09bb68cc228bec6734aa087cd55a424b",
	},
	"subscribe_unsupported": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_sub_5","operation":"subscribe","error":{"code":"unsupported"}}`,
		sha: "026eb6460c8c687c9e0dacc7c89836f54ef32c4e6fac1b9f1ec61f6bec72ca7d",
	},
	"error_invalid_request": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_err_1","operation":"send_nudge","error":{"code":"invalid_request"}}`,
		sha: "4e50530254a1c333e68f2f48a47fb49380a69e011c6b20bc5a4dd7542611b15a",
	},
	"error_duplicate_request": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_err_2","operation":"send_nudge","error":{"code":"duplicate_request"}}`,
		sha: "38ae4836cbc0f504894166738af6b82fd59df9764fd76658fc917e2b4b05cbc5",
	},
	"error_not_found": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_err_3","operation":"send_nudge","error":{"code":"not_found"}}`,
		sha: "09e2fa7166c49fdc9194ae2647fc517f801aba346f95b40ba84338b6bdc12f5d",
	},
	"error_conflict": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_err_4","operation":"send_nudge","error":{"code":"conflict"}}`,
		sha: "348e213bfa2b8f16efbeb36be0b39844464db0470a86d53acd4a1e781715e24e",
	},
	"error_capacity": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_err_5","operation":"send_nudge","error":{"code":"capacity"}}`,
		sha: "e1d1a2a982de46da8b1101c1cee0e487008a8ef75c1e8b6a4491511836317042",
	},
	"error_transport": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_err_6","operation":"send_nudge","error":{"code":"transport"}}`,
		sha: "937ad520f1d374230278302ed73a717d22b16b7041af834e61da26155190477a",
	},
	"error_canceled": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_err_7","operation":"send_nudge","error":{"code":"canceled"}}`,
		sha: "62037cefb5dc34f8ec7959339f4fa78d3ac166b7c27f0cbd84b5ec22b2409409",
	},
	"cancel_success": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_can_1","operation":"cancel","cancel":{"target_correlation_id":"corr_read_1","state":"canceled"}}`,
		sha: "0a125231cbe05fc8d7fda1d5fa55cb98665fe5d032e15ece34ecee3d06886c28",
	},
	"cancel_already_final": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_can_2","operation":"cancel","cancel":{"target_correlation_id":"corr_read_1","state":"already_final"}}`,
		sha: "0a204b6226408eb4a5b57df2caeb8b0728b41fbc96e0b6ad1d71bd9ac703f447",
	},
	"cancel_not_found": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_can_3","operation":"cancel","cancel":{"target_correlation_id":"corr_read_1","state":"not_found"}}`,
		sha: "ed7433cd8c925257bd66649c6a649d0e55d5534db10482575a07fd8fddc4efe1",
	},
}

func TestMachineRequestGoldensRoundTripAndShapeHashes(t *testing.T) {
	t.Parallel()

	for name, item := range machineRequestGoldens {
		decoded, err := DecodeMachineRequestLine(item.raw)
		if err != nil {
			t.Fatalf("%s decode: %v", name, err)
		}
		encoded, err := EncodeMachineRequestLine(decoded)
		if err != nil {
			t.Fatalf("%s encode: %v", name, err)
		}
		if encoded != item.raw {
			t.Fatalf("%s wire mismatch\n got  %s\nwant %s", name, encoded, item.raw)
		}
		h := sha256.Sum256([]byte(item.raw))
		if got := hex.EncodeToString(h[:]); got != item.sha {
			t.Fatalf("%s hash mismatch\n got  %s\nwant %s", name, got, item.sha)
		}
	}
}

func TestMachineResponseGoldensRoundTripAndShapeHashes(t *testing.T) {
	t.Parallel()

	for name, item := range machineResponseGoldens {
		decoded, err := DecodeMachineResponseLine(item.raw)
		if err != nil {
			t.Fatalf("%s decode: %v", name, err)
		}
		encoded, err := EncodeMachineResponseLine(decoded)
		if err != nil {
			t.Fatalf("%s encode: %v", name, err)
		}
		if encoded != item.raw {
			t.Fatalf("%s wire mismatch\n got  %s\nwant %s", name, encoded, item.raw)
		}
		h := sha256.Sum256([]byte(item.raw))
		if got := hex.EncodeToString(h[:]); got != item.sha {
			t.Fatalf("%s hash mismatch\n got  %s\nwant %s", name, got, item.sha)
		}
	}
}
