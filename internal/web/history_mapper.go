package web

import (
	feinov1 "github.com/odinnordico/feino/gen/feino/v1"
	"github.com/odinnordico/feino/internal/model"
)

// messagesToProto converts a slice of model.Message into proto HistoryMessage
// values for the GetHistory RPC response.
func messagesToProto(msgs []model.Message) []*feinov1.HistoryMessage {
	out := make([]*feinov1.HistoryMessage, 0, len(msgs))
	for _, msg := range msgs {
		pm := &feinov1.HistoryMessage{
			Role: string(msg.GetRole()),
		}
		for part := range msg.GetParts().Iterator() {
			hp := partToHistoryPart(part)
			if hp != nil {
				pm.Parts = append(pm.Parts, hp)
			}
		}
		out = append(out, pm)
	}
	return out
}

func partToHistoryPart(part model.MessagePart) *feinov1.HistoryPart {
	switch p := part.(type) {
	case *model.TextMessagePart:
		text, _ := p.GetContent().(string)
		return &feinov1.HistoryPart{
			Content: &feinov1.HistoryPart_Text{Text: text},
		}
	case *model.ThoughtPart:
		text, _ := p.GetContent().(string)
		return &feinov1.HistoryPart{
			Content: &feinov1.HistoryPart_Thought{Thought: text},
		}
	case *model.ToolCallPart:
		tc, _ := p.GetContent().(model.ToolCall)
		return &feinov1.HistoryPart{
			Content: &feinov1.HistoryPart_ToolCall{
				ToolCall: &feinov1.ToolCall{
					Id:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			},
		}
	case *model.ToolResultPart:
		tr, _ := p.GetContent().(model.ToolResult)
		return &feinov1.HistoryPart{
			Content: &feinov1.HistoryPart_ToolResult{
				ToolResult: &feinov1.ToolResult{
					CallId:  tr.CallID,
					Name:    tr.Name,
					Content: tr.Content,
					IsError: tr.IsError,
				},
			},
		}
	default:
		return nil
	}
}
