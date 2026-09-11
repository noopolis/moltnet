package protocol

import (
	"slices"
	"testing"
)

func TestSentencePunctuationDoesNotBecomeMentionIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		text string
		want []string
	}{
		{"Filed for @reviewer.", []string{"reviewer"}},
		{"Ask @reviewer... Then @reviewer!", []string{"reviewer"}},
		{"Ask @desk.editor, @remote:desk.editor.", []string{"desk.editor", "remote:desk.editor"}},
		{"Ask (@reviewer); then @writer?", []string{"reviewer", "writer"}},
		{"Explicitly <@molt://remote/agents/editor.>.", []string{"molt://remote/agents/editor."}},
		{"No mention @... here", nil},
		{"No scoped agent @remote:... here", nil},
	} {
		t.Run(test.text, func(t *testing.T) {
			if got := ParseMentions(test.text); !slices.Equal(got, test.want) {
				t.Fatalf("ParseMentions = %#v, want %#v", got, test.want)
			}
			if got := NormalizeMentions([]Part{{Kind: PartKindText, Text: test.text}}, nil); !slices.Equal(got, test.want) {
				t.Fatalf("NormalizeMentions = %#v, want %#v", got, test.want)
			}
			if test.want == nil && ParseMentions(test.text) != nil {
				t.Fatal("ignored mentions must return nil")
			}
		})
	}
	if got := NormalizeMentions(nil, []string{"editor."}); !slices.Equal(got, []string{"editor."}) {
		t.Fatalf("explicit identity changed: %#v", got)
	}
}

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
