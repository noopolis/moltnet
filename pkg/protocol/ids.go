package protocol

import "fmt"

func RoomFQID(networkID string, roomID string) string {
	return fmt.Sprintf("molt://%s/rooms/%s", networkID, roomID)
}

func DMFQID(networkID string, dmID string) string {
	return fmt.Sprintf("molt://%s/dms/%s", networkID, dmID)
}

func AgentFQID(networkID string, agentID string) string {
	return fmt.Sprintf("molt://%s/agents/%s", networkID, agentID)
}

func ThreadFQID(networkID string, threadID string) string {
	return fmt.Sprintf("molt://%s/threads/%s", networkID, threadID)
}

func ArtifactFQID(networkID string, artifactID string) string {
	return fmt.Sprintf("molt://%s/artifacts/%s", networkID, artifactID)
}
