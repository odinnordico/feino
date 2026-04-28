package web

import (
	"fmt"

	feinov1 "github.com/odinnordico/feino/gen/feino/v1"
	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/tokens"
)

// eventToProto converts a single app.Event into a protobuf SendMessageResponse.
// done is true when the stream should be closed after sending this event
// (CompleteEvent, ErrorEvent).
// Returns (nil, false, nil) for event kinds that should be silently skipped.
func eventToProto(e app.Event) (msg *feinov1.SendMessageResponse, done bool, err error) {
	switch e.Kind {
	case app.EventPartReceived:
		return partReceivedEvent(e.Payload)
	case app.EventStateChanged:
		return stateChangedEvent(e.Payload)
	case app.EventUsageUpdated:
		return usageUpdatedEvent(e.Payload)
	case app.EventComplete:
		return completeEvent(e.Payload)
	case app.EventError:
		return errorEvent(e.Payload)
	case app.EventKind("permission_request"):
		return permissionRequestEvent(e.Payload)
	default:
		return nil, false, nil // unknown kinds are silently skipped
	}
}

// ── per-kind converters ───────────────────────────────────────────────────────

func partReceivedEvent(payload any) (*feinov1.SendMessageResponse, bool, error) {
	part, ok := payload.(model.MessagePart)
	if !ok {
		return nil, false, fmt.Errorf("event_mapper: expected model.MessagePart, got %T", payload)
	}

	switch p := part.(type) {
	case *model.ThoughtPart:
		text, _ := p.GetContent().(string)
		return &feinov1.SendMessageResponse{
			Event: &feinov1.SendMessageResponse_ThoughtReceived{
				ThoughtReceived: &feinov1.ThoughtReceivedEvent{Text: text},
			},
		}, false, nil
	case *model.ToolCallPart:
		tc, _ := p.GetContent().(model.ToolCall)
		return &feinov1.SendMessageResponse{
			Event: &feinov1.SendMessageResponse_ToolCall{
				ToolCall: &feinov1.ToolCallEvent{
					CallId:    tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			},
		}, false, nil
	case *model.ToolResultPart:
		tr, _ := p.GetContent().(model.ToolResult)
		return &feinov1.SendMessageResponse{
			Event: &feinov1.SendMessageResponse_ToolResult{
				ToolResult: &feinov1.ToolResultEvent{
					CallId:  tr.CallID,
					Name:    tr.Name,
					Content: tr.Content,
					IsError: tr.IsError,
				},
			},
		}, false, nil
	default:
		// Text or unknown part → PartReceivedEvent
		text := ""
		if content, ok := part.GetContent().(string); ok {
			text = content
		}
		return &feinov1.SendMessageResponse{
			Event: &feinov1.SendMessageResponse_PartReceived{
				PartReceived: &feinov1.PartReceivedEvent{Text: text},
			},
		}, false, nil
	}
}

func stateChangedEvent(payload any) (*feinov1.SendMessageResponse, bool, error) {
	state := fmt.Sprintf("%v", payload) // agent.ReActState is a string alias
	return &feinov1.SendMessageResponse{
		Event: &feinov1.SendMessageResponse_StateChanged{
			StateChanged: &feinov1.StateChangedEvent{State: state},
		},
	}, false, nil
}

func usageUpdatedEvent(payload any) (*feinov1.SendMessageResponse, bool, error) {
	meta, ok := payload.(tokens.UsageMetadata)
	if !ok {
		return nil, false, fmt.Errorf("event_mapper: expected tokens.UsageMetadata, got %T", payload)
	}
	return &feinov1.SendMessageResponse{
		Event: &feinov1.SendMessageResponse_UsageUpdated{
			UsageUpdated: &feinov1.UsageUpdatedEvent{
				Usage: &feinov1.UsageMetadata{
					PromptTokens:     int32(meta.PromptTokens),
					CompletionTokens: int32(meta.CompletionTokens),
					TotalTokens:      int32(meta.TotalTokens),
				},
			},
		},
	}, false, nil
}

func completeEvent(payload any) (*feinov1.SendMessageResponse, bool, error) {
	finalText := ""
	if msg, ok := payload.(model.Message); ok {
		finalText = msg.GetTextContent()
	}
	return &feinov1.SendMessageResponse{
		Event: &feinov1.SendMessageResponse_Complete{
			Complete: &feinov1.CompleteEvent{FinalText: finalText},
		},
	}, true, nil // done=true → close stream
}

func errorEvent(payload any) (*feinov1.SendMessageResponse, bool, error) {
	msg := "unknown error"
	if err, ok := payload.(error); ok {
		msg = err.Error()
	}
	return &feinov1.SendMessageResponse{
		Event: &feinov1.SendMessageResponse_Error{
			Error: &feinov1.ErrorEvent{Message: msg, Code: "internal"},
		},
	}, true, nil // done=true → close stream
}

func permissionRequestEvent(payload any) (*feinov1.SendMessageResponse, bool, error) {
	p, ok := payload.(permissionRequestPayload)
	if !ok {
		return nil, false, fmt.Errorf("event_mapper: expected permissionRequestPayload, got %T", payload)
	}
	return &feinov1.SendMessageResponse{
		Event: &feinov1.SendMessageResponse_PermissionRequest{
			PermissionRequest: &feinov1.PermissionRequestEvent{
				RequestId: p.RequestID,
				ToolName:  p.ToolName,
				Required:  p.Required,
				Allowed:   p.Allowed,
			},
		},
	}, false, nil
}
