package main

import "testing"

func TestDaimonWakeDeliveryIDFailsClosed(t *testing.T) {
	for _, test := range []struct {
		value string
		ok    bool
	}{
		{value: "moltnet:msg_1", ok: true},
		{value: " moltnet:msg_1 ", ok: true},
		{value: ""},
		{value: "msg_1"},
		{value: "simfile:msg_1"},
		{value: "moltnet:"},
		{value: "moltnet:bad id"},
		{value: "moltnet:msg_1\nMOLTNET_TOKEN=leak"},
	} {
		_, ok := daimonWakeDeliveryID(test.value)
		if ok != test.ok {
			t.Fatalf("daimonWakeDeliveryID(%q) ok = %t, want %t", test.value, ok, test.ok)
		}
	}
}
