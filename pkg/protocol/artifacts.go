package protocol

import "time"

type Artifact struct {
	ID        string    `json:"id"`
	NetworkID string    `json:"network_id,omitempty"`
	FQID      string    `json:"fqid,omitempty"`
	MessageID string    `json:"message_id"`
	Target    Target    `json:"target"`
	PartIndex int       `json:"part_index"`
	Kind      string    `json:"kind"`
	MediaType string    `json:"media_type,omitempty"`
	Filename  string    `json:"filename,omitempty"`
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ArtifactFilter struct {
	RoomID   string
	ThreadID string
	DMID     string
}

func (f ArtifactFilter) Scoped() bool {
	return f.RoomID != "" || f.ThreadID != "" || f.DMID != ""
}

type ArtifactPage struct {
	Artifacts []Artifact `json:"artifacts"`
	Page      PageInfo   `json:"page"`
}
