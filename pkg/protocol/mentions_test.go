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

func TestParseMentions(t *testing.T) {
	t.Parallel()

	mentions := ParseMentions("@writer please ask @reviewer and @writer again")
	if len(mentions) != 2 || mentions[0] != "writer" || mentions[1] != "reviewer" {
		t.Fatalf("unexpected mentions %#v", mentions)
	}

	if mentions := ParseMentions("no mentions here"); mentions != nil {
		t.Fatalf("expected nil mentions, got %#v", mentions)
	}
}

func TestParseMentionMatchesSkipsMalformedEntries(t *testing.T) {
	t.Parallel()

	mentions := parseMentionMatches([][]string{
		nil,
		{""},
		{"@writer", ""},
		{"@writer", "writer"},
		{"@writer", "writer"},
		{"@reviewer", "reviewer"},
	})

	if len(mentions) != 2 || mentions[0] != "writer" || mentions[1] != "reviewer" {
		t.Fatalf("unexpected mentions %#v", mentions)
	}
}

func TestArtifactFilterScoped(t *testing.T) {
	t.Parallel()

	if (ArtifactFilter{}).Scoped() {
		t.Fatal("expected empty filter to be unscoped")
	}
	if !(ArtifactFilter{ThreadID: "thread_1"}).Scoped() {
		t.Fatal("expected thread filter to be scoped")
	}
}
