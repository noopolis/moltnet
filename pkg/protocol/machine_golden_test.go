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
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_send_1","operation":"send_nudge","send_nudge":{"message_id":"message_1","event_id":"event_1","accepted":true,"thread_id":"thread_1","thread_created":false,"dm_id":"dm_1","dm_created":false}}`,
		sha: "08deded491d2baf558e3c29b0baf461bc0dc7ac3cac717bf81f26a87aecbd064",
	},
	"read_success": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_read_1","operation":"read","read":{"target":{"kind":"room","id":"room_1"},"page":{"messages":[],"page":{"has_more":false}}}}`,
		sha: "7b5a9fadfe3e473b75e9a7e42383a213d1975adc08aace8bbea35a28bc8370a5",
	},
	"subscribe_event": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_sub_1","operation":"subscribe","event":{"event_id":"e1","type":"message","payload":{"message":"m"}}}`,
		sha: "df2df6cc41e19605eb325f64cda95da1ed5dd2e167bf2ede3db1e522ab1193e9",
	},
	"subscribe_done": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_sub_2","operation":"subscribe","subscribe":{"closed":"closed","reason":"done"}}`,
		sha: "dda3481796a435c1a9577a7c6a49fc20e6647730d861db7010588004a30d34ba",
	},
	"export_success": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_exp_1","operation":"export","export":{"version":"moltnet.machine.export.v1","control_marker":"moltnet.control.nudge.v1","transcript":"one\nline","transcript_sha256":"0207e4765bc7aeff18d5235cd7f2cf20d0787148585a8f73061bd06dab269d21"}}`,
		sha: "9cf0d074cd7b328e278f84ba6ac49fda93074dfa103db6c923354a91bd96331d",
	},
	"export_unsupported": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_exp_1","operation":"export","error":{"code":"unsupported","message":"export is not yet supported"}}`,
		sha: "1b26b5ec60b38200a30114fc03fa21dd88a0bce3dcfbca0f8f4fb28916edcd96",
	},
	"subscribe_unsupported": {
		raw: `{"version":"moltnet.machine.v1","correlation_id":"corr_sub_3","operation":"subscribe","error":{"code":"unsupported","message":"subscribe is not yet supported"}}`,
		sha: "ce4aa8b8cb094bbb9ee2463fdbd0d0478606c4729eb27d7ae2681d86a1d639b4",
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
