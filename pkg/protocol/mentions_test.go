package protocol

import "testing"

func TestNormalizeMentions(t *testing.T) {
	t.Parallel()

	t.Run("extracts from text parts", func(t *testing.T) {
		t.Parallel()

		mentions := NormalizeMentions(
			[]Part{
				{Kind: "text", Text: "@orchestrator please involve @researcher and @remote:reviewer"},
				{Kind: "url", URL: "https://example.com"},
				{Kind: "text", Text: "loop in <@molt://local_lab/agents/writer> too"},
			},
			nil,
		)

		if len(mentions) != 4 ||
			mentions[0] != "orchestrator" ||
			mentions[1] != "researcher" ||
			mentions[2] != "remote:reviewer" ||
			mentions[3] != "molt://local_lab/agents/writer" {
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

	mentions = ParseMentions("@local:writer please ask <@molt://remote/agents/reviewer>")
	if len(mentions) != 2 || mentions[0] != "local:writer" || mentions[1] != "molt://remote/agents/reviewer" {
		t.Fatalf("unexpected scoped/canonical mentions %#v", mentions)
	}

	mentions = ParseMentions("@molt://remote/agents/reviewer ask @writer")
	if len(mentions) != 1 || mentions[0] != "writer" {
		t.Fatalf("unexpected raw URI mention handling %#v", mentions)
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
		{"<@molt://local/agents/editor>", "molt://local/agents/editor", ""},
		{"@remote:planner", "", "remote:planner"},
	})

	if len(mentions) != 4 ||
		mentions[0] != "writer" ||
		mentions[1] != "reviewer" ||
		mentions[2] != "molt://local/agents/editor" ||
		mentions[3] != "remote:planner" {
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
