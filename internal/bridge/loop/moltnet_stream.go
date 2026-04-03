package loop

import (
	"context"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const maxBufferedAttachmentFrames = 64

type attachmentFrameResult struct {
	err   error
	frame protocol.AttachmentFrame
}

func (c *MoltnetClient) streamFrames(
	ctx context.Context,
	connection *websocket.Conn,
	readTimeout time.Duration,
	write func(protocol.AttachmentFrame) error,
) <-chan attachmentFrameResult {
	results := make(chan attachmentFrameResult, maxBufferedAttachmentFrames)

	go func() {
		defer close(results)

		for {
			frame, err := c.readFrame(connection, readTimeout)
			if err != nil {
				sendAttachmentFrameResult(ctx, results, attachmentFrameResult{err: err})
				return
			}

			switch frame.Op {
			case protocol.AttachmentOpPing:
				if err := write(protocol.AttachmentFrame{
					Op:      protocol.AttachmentOpPong,
					Version: protocol.AttachmentProtocolV1,
				}); err != nil {
					sendAttachmentFrameResult(ctx, results, attachmentFrameResult{
						err: fmt.Errorf("write attachment pong: %w", err),
					})
					return
				}
			case protocol.AttachmentOpEvent, protocol.AttachmentOpError:
				if !sendAttachmentFrameResult(ctx, results, attachmentFrameResult{frame: frame}) {
					return
				}
			default:
				if !sendAttachmentFrameResult(ctx, results, attachmentFrameResult{
					err: fmt.Errorf("unexpected attachment frame op %q", frame.Op),
				}) {
					return
				}
				return
			}
		}
	}()

	return results
}

func sendAttachmentFrameResult(
	ctx context.Context,
	results chan<- attachmentFrameResult,
	result attachmentFrameResult,
) bool {
	select {
	case <-ctx.Done():
		return false
	case results <- result:
		return true
	}
}
