package store

import (
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func collectArtifacts(messages []protocol.Message) []protocol.Artifact {
	artifacts := make([]protocol.Artifact, 0)

	for _, message := range messages {
		for index, part := range message.Parts {
			if !isArtifactPart(part) {
				continue
			}

			artifactID := fmt.Sprintf("art_%s_%d", message.ID, index)
			artifacts = append(artifacts, protocol.Artifact{
				ID:        artifactID,
				NetworkID: message.NetworkID,
				FQID:      protocol.ArtifactFQID(message.NetworkID, artifactID),
				MessageID: message.ID,
				Target:    message.Target,
				PartIndex: index,
				Kind:      part.Kind,
				MediaType: part.MediaType,
				Filename:  part.Filename,
				URL:       part.URL,
				CreatedAt: message.CreatedAt,
			})
		}
	}

	return artifacts
}

func isArtifactPart(part protocol.Part) bool {
	return part.Kind != protocol.PartKindText || part.URL != "" || part.Filename != "" || part.MediaType != ""
}

type artifactItem struct{ protocol.Artifact }

func (a artifactItem) GetID() string { return a.Artifact.ID }

func pageArtifactsResult(artifacts []protocol.Artifact, page protocol.PageRequest) (protocol.ArtifactPage, error) {
	if page.Before == "" && page.After == "" {
		limit := page.Limit
		if limit <= 0 || len(artifacts) <= limit {
			return protocol.ArtifactPage{
				Artifacts: append([]protocol.Artifact(nil), artifacts...),
				Page:      protocol.PageInfo{},
			}, nil
		}

		selected := append([]protocol.Artifact(nil), artifacts[len(artifacts)-limit:]...)
		return protocol.ArtifactPage{
			Artifacts: selected,
			Page: protocol.PageInfo{
				HasMore:    true,
				NextBefore: selected[0].ID,
			},
		}, nil
	}

	items := make([]artifactItem, 0, len(artifacts))
	for _, artifact := range artifacts {
		items = append(items, artifactItem{Artifact: artifact})
	}
	selected, info, err := paginateByID(items, page)
	if err != nil {
		return protocol.ArtifactPage{}, err
	}
	values := make([]protocol.Artifact, 0, len(selected))
	for _, item := range selected {
		values = append(values, item.Artifact)
	}
	return protocol.ArtifactPage{
		Artifacts: values,
		Page:      info,
	}, nil
}
