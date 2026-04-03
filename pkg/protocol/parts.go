package protocol

const (
	PartKindText  = "text"
	PartKindURL   = "url"
	PartKindData  = "data"
	PartKindFile  = "file"
	PartKindImage = "image"
	PartKindAudio = "audio"
)

func IsKnownPartKind(kind string) bool {
	switch kind {
	case PartKindText, PartKindURL, PartKindData, PartKindFile, PartKindImage, PartKindAudio:
		return true
	default:
		return false
	}
}
