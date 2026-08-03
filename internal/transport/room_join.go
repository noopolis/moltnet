package transport

import (
	"net/http"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func handleJoinRoom(service Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		var payload protocol.JoinRoomRequest
		if err := decodeJSON(response, request, &payload); err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}

		room, err := service.JoinRoomContext(request.Context(), request.PathValue("roomID"), payload)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}

		writeJSON(response, http.StatusOK, room)
	}
}
