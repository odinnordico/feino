package web

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/security"
)

// permSeq generates unique permission request IDs across all active streams.
var permSeq atomic.Uint64

// SessionManager decouples the global app.Session event stream from individual
// HTTP connections.
//
// Problem: sess.Subscribe is append-only — handlers are never removed. If we
// registered one handler per HTTP connection they would accumulate and fire long
// after connections are closed.
//
// Solution: Register one global handler that forwards events to a sync.Map of
// per-stream channels. When a connection closes its channel is removed from the
// map. No handlers ever leak.
type SessionManager struct {
	sess *app.Session

	// streamSubs maps streamID → chan app.Event for active SendMessage streams.
	streamSubs sync.Map

	// activeStreamID tracks which stream is currently receiving events.
	// Only one turn runs at a time, so a single string is sufficient.
	activeStreamID atomic.Value // stores string

	// pendingPerms maps permission requestID → chan bool for blocking ReAct
	// goroutines awaiting user permission decisions.
	pendingPerms sync.Map
}

// NewSessionManager creates a SessionManager and registers the single global
// event handler on sess. The manager is ready to use immediately.
func NewSessionManager(sess *app.Session) *SessionManager {
	sm := &SessionManager{sess: sess}
	sm.activeStreamID.Store("")

	// One global subscriber — fans out to all registered stream channels.
	sess.Subscribe(func(e app.Event) {
		id, _ := sm.activeStreamID.Load().(string)
		if id == "" {
			return
		}
		if ch, ok := sm.streamSubs.Load(id); ok {
			select {
			case ch.(chan app.Event) <- e:
			default:
				// Channel full — drop rather than block the ReAct goroutine.
			}
		}
	})

	// Permission callback — called in the ReAct goroutine when the security
	// gate denies a tool call and an interactive prompt is needed.
	sess.SetPermissionCallback(func(ctx context.Context, toolName string, required, allowed security.PermissionLevel) bool {
		id, _ := sm.activeStreamID.Load().(string)
		if id == "" {
			return false
		}

		// Derive a unique request ID for this permission prompt.
		reqID := fmt.Sprintf("%s:%s:%d", id, toolName, permSeq.Add(1))

		// Create the response channel and park it before notifying the stream.
		respCh := make(chan bool, 1)
		sm.pendingPerms.Store(reqID, respCh)
		defer sm.pendingPerms.Delete(reqID)

		// Emit a PermissionRequestEvent to the active stream.
		permEvent := app.Event{
			Kind: app.EventKind("permission_request"),
			Payload: permissionRequestPayload{
				RequestID: reqID,
				ToolName:  toolName,
				Required:  required.String(),
				Allowed:   allowed.String(),
			},
		}
		if ch, ok := sm.streamSubs.Load(id); ok {
			select {
			case ch.(chan app.Event) <- permEvent:
			default:
			}
		}

		// Block until the client resolves or the turn context is cancelled.
		select {
		case <-ctx.Done():
			return false
		case approved := <-respCh:
			return approved
		}
	})

	return sm
}

// permissionRequestPayload is the Payload for the synthetic
// "permission_request" event kind emitted by the manager.
type permissionRequestPayload struct {
	RequestID string
	ToolName  string
	Required  string
	Allowed   string
}

// Subscribe creates a buffered event channel for the given streamID, registers
// it, and sets it as the active stream. The returned cancel function must be
// called when the stream closes.
//
// Only one stream should be active at a time (the session itself serialises
// turns). A new Subscribe call replaces the active stream ID.
func (sm *SessionManager) Subscribe(streamID string) (<-chan app.Event, func()) {
	ch := make(chan app.Event, 64)
	sm.streamSubs.Store(streamID, ch)
	sm.activeStreamID.Store(streamID)

	cancel := func() {
		sm.streamSubs.Delete(streamID)
		// Clear active only if it still points to this stream.
		if cur, _ := sm.activeStreamID.Load().(string); cur == streamID {
			sm.activeStreamID.Store("")
		}
		// Do NOT close ch — the global event handler may still be trying to
		// send to it (send-to-closed-channel is a panic). The channel becomes
		// unreachable after the map deletion and will be GC'd.
	}
	return ch, cancel
}

// ResolvePermission unblocks the ReAct goroutine waiting on the given
// requestID, passing the user's approval decision.
func (sm *SessionManager) ResolvePermission(requestID string, approved bool) error {
	v, ok := sm.pendingPerms.Load(requestID)
	if !ok {
		return fmt.Errorf("session_manager: no pending permission request %q", requestID)
	}
	v.(chan bool) <- approved
	return nil
}

// ActiveStreamID returns the stream ID of the currently active SendMessage
// call, or empty string when idle.
func (sm *SessionManager) ActiveStreamID() string {
	id, _ := sm.activeStreamID.Load().(string)
	return id
}
