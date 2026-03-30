package protocol

import "testing"

func TestNormalizeMentions(t *testing.T) {
	t.Parallel()

	t.Run("extracts from text parts", func(t *testing.T) {
		t.Parallel()

		mentions := NormalizeMentions(
			[]Part{
				{Kind: "text", Text: "@orchestrator please involve @researcher"},
				{Kind: "url", URL: "https://example.com"},
				{Kind: "text", Text: "loop in @writer too"},
			},
			nil,
		)

		if len(mentions) != 3 || mentions[0] != "orchestrator" || mentions[1] != "researcher" || mentions[2] != "writer" {
			t.Fatalf("unexpected mentions %#v", mentions)
		}
	})

	t.Run("merges explicit mentions without duplicates", func(t *testing.T) {
		t.Parallel()

		mentions := NormalizeMentions(
			[]Part{{Kind: "text", Text: "@researcher ask @writer"}},
			[]string{"researcher", "writer", "researcher"},
		)

		if len(mentions) != 2 || mentions[0] != "researcher" || mentions[1] != "writer" {
			t.Fatalf("unexpected mentions %#v", mentions)
		}
	})

	t.Run("returns nil when no mentions exist", func(t *testing.T) {
		t.Parallel()

		if mentions := NormalizeMentions([]Part{{Kind: "text", Text: "hello world"}}, nil); mentions != nil {
			t.Fatalf("expected nil mentions, got %#v", mentions)
		}
	})
}
