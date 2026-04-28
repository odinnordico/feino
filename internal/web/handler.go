package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"

	feinov1 "github.com/odinnordico/feino/gen/feino/v1"
	"github.com/odinnordico/feino/gen/feino/v1/feinov1connect"
	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/credentials"
	"github.com/odinnordico/feino/internal/memory"
)

// FeinoServiceHandler implements feinov1connect.FeinoServiceHandler.
type FeinoServiceHandler struct {
	feinov1connect.UnimplementedFeinoServiceHandler

	sess    *app.Session
	sm      *SessionManager
	mhub    *metricsHub
	fileSvc *fileService
	store   credentials.Store
	mem     *memory.FileStore
	cfg     *config.Config
	cfgPath string
}

// Ensure interface compliance at compile time.
var _ feinov1connect.FeinoServiceHandler = (*FeinoServiceHandler)(nil)

// ── Phase 1 ───────────────────────────────────────────────────────────────────

// GetSessionState returns the current session state.
func (h *FeinoServiceHandler) GetSessionState(
	_ context.Context,
	_ *connect.Request[feinov1.GetSessionStateRequest],
) (*connect.Response[feinov1.GetSessionStateResponse], error) {
	state := h.sess.GetCurrentState()
	return connect.NewResponse(&feinov1.GetSessionStateResponse{
		Busy:         false,
		QueueLength:  0,
		ReactState:   string(state),
		BypassActive: h.sess.IsBypassActive(),
	}), nil
}

// ── Phase 2: Session Bridge ───────────────────────────────────────────────────

// SendMessage sends a user message and streams agent events until the turn
// completes or the client disconnects.
func (h *FeinoServiceHandler) SendMessage(
	ctx context.Context,
	req *connect.Request[feinov1.SendMessageRequest],
	stream *connect.ServerStream[feinov1.SendMessageResponse],
) error {
	streamID := uuid.New().String()
	eventCh, cancel := h.sm.Subscribe(streamID)
	defer cancel()

	text := ExpandRefs(req.Msg.GetText(), h.fileSvc)
	if err := h.sess.Send(ctx, text); err != nil {
		return connect.NewError(connect.CodeResourceExhausted, err)
	}

	for {
		select {
		case <-ctx.Done():
			h.sess.Cancel()
			return nil
		case e := <-eventCh:
			msg, done, err := eventToProto(e)
			if err != nil {
				slog.Warn("web: event_mapper error", "error", err)
				continue
			}
			if msg == nil {
				continue
			}
			if sendErr := stream.Send(msg); sendErr != nil {
				return sendErr
			}
			if done {
				return nil
			}
		}
	}
}

// CancelTurn cancels the in-flight turn, if any.
func (h *FeinoServiceHandler) CancelTurn(
	_ context.Context,
	_ *connect.Request[feinov1.CancelTurnRequest],
) (*connect.Response[feinov1.CancelTurnResponse], error) {
	h.sess.Cancel()
	return connect.NewResponse(&feinov1.CancelTurnResponse{}), nil
}

// ResolvePermission unblocks a ReAct goroutine awaiting a permission decision.
func (h *FeinoServiceHandler) ResolvePermission(
	_ context.Context,
	req *connect.Request[feinov1.ResolvePermissionRequest],
) (*connect.Response[feinov1.ResolvePermissionResponse], error) {
	if err := h.sm.ResolvePermission(req.Msg.GetRequestId(), req.Msg.GetApproved()); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&feinov1.ResolvePermissionResponse{}), nil
}

// ── Phase 3: Supporting RPCs ──────────────────────────────────────────────────

// GetHistory returns the full conversation history.
func (h *FeinoServiceHandler) GetHistory(
	_ context.Context,
	_ *connect.Request[feinov1.GetHistoryRequest],
) (*connect.Response[feinov1.GetHistoryResponse], error) {
	msgs := h.sess.History()
	return connect.NewResponse(&feinov1.GetHistoryResponse{
		Messages: messagesToProto(msgs),
	}), nil
}

// ResetSession clears conversation history.
func (h *FeinoServiceHandler) ResetSession(
	_ context.Context,
	_ *connect.Request[feinov1.ResetSessionRequest],
) (*connect.Response[feinov1.ResetSessionResponse], error) {
	_ = h.sess.Reset()
	return connect.NewResponse(&feinov1.ResetSessionResponse{}), nil
}

// GetConfig returns the active configuration (without API keys).
func (h *FeinoServiceHandler) GetConfig(
	_ context.Context,
	_ *connect.Request[feinov1.GetConfigRequest],
) (*connect.Response[feinov1.GetConfigResponse], error) {
	cfg := h.sess.Config()
	return connect.NewResponse(&feinov1.GetConfigResponse{
		Config: configToProto(cfg),
	}), nil
}

// UpdateConfig applies partial config changes and persists them.
func (h *FeinoServiceHandler) UpdateConfig(
	_ context.Context,
	req *connect.Request[feinov1.UpdateConfigRequest],
) (*connect.Response[feinov1.UpdateConfigResponse], error) {
	current := h.sess.Config()
	updated := protoToConfig(req.Msg.GetConfig(), current)

	if err := h.sess.UpdateConfig(updated); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	// Persist to disk when a path is known.
	if h.cfgPath != "" {
		if err := config.Save(h.cfgPath, updated); err != nil {
			slog.Warn("web: failed to persist config", "error", err)
		}
	}
	return connect.NewResponse(&feinov1.UpdateConfigResponse{
		Config:  configToProto(updated),
		Message: "config updated",
	}), nil
}

// GetConfigYAML returns the active config serialised as YAML.
func (h *FeinoServiceHandler) GetConfigYAML(
	_ context.Context,
	_ *connect.Request[feinov1.GetConfigYAMLRequest],
) (*connect.Response[feinov1.GetConfigYAMLResponse], error) {
	cfg := h.sess.Config()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("marshal config: %w", err))
	}
	return connect.NewResponse(&feinov1.GetConfigYAMLResponse{Yaml: string(data)}), nil
}

// ── Memory RPCs ───────────────────────────────────────────────────────────────

// ListMemories returns stored memory entries, optionally filtered by category
// or a search query.
func (h *FeinoServiceHandler) ListMemories(
	_ context.Context,
	req *connect.Request[feinov1.ListMemoriesRequest],
) (*connect.Response[feinov1.ListMemoriesResponse], error) {
	if h.mem == nil {
		return connect.NewResponse(&feinov1.ListMemoriesResponse{}), nil
	}

	var (
		entries []memory.Entry
		err     error
	)
	switch {
	case req.Msg.GetQuery() != "":
		entries, err = h.mem.Search(req.Msg.GetQuery())
	case req.Msg.GetCategory() != "":
		entries, err = h.mem.ByCategory(memory.Category(req.Msg.GetCategory()))
	default:
		entries, err = h.mem.All()
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protos := make([]*feinov1.MemoryEntryProto, len(entries))
	for i, e := range entries {
		protos[i] = memoryEntryToProto(&e)
	}
	return connect.NewResponse(&feinov1.ListMemoriesResponse{Entries: protos}), nil
}

// WriteMemory creates a new memory entry.
func (h *FeinoServiceHandler) WriteMemory(
	_ context.Context,
	req *connect.Request[feinov1.WriteMemoryRequest],
) (*connect.Response[feinov1.WriteMemoryResponse], error) {
	if h.mem == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("memory store not initialised"))
	}
	e, err := h.mem.Write(memory.Category(req.Msg.GetCategory()), req.Msg.GetContent())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&feinov1.WriteMemoryResponse{Entry: memoryEntryToProto(&e)}), nil
}

// UpdateMemory replaces the content of an existing memory entry.
func (h *FeinoServiceHandler) UpdateMemory(
	_ context.Context,
	req *connect.Request[feinov1.UpdateMemoryRequest],
) (*connect.Response[feinov1.UpdateMemoryResponse], error) {
	if h.mem == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("memory store not initialised"))
	}
	e, err := h.mem.Update(req.Msg.GetId(), req.Msg.GetContent())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&feinov1.UpdateMemoryResponse{Entry: memoryEntryToProto(&e)}), nil
}

// DeleteMemory removes a memory entry by ID.
func (h *FeinoServiceHandler) DeleteMemory(
	_ context.Context,
	req *connect.Request[feinov1.DeleteMemoryRequest],
) (*connect.Response[feinov1.DeleteMemoryResponse], error) {
	if h.mem == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("memory store not initialised"))
	}
	if err := h.mem.Delete(req.Msg.GetId()); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&feinov1.DeleteMemoryResponse{}), nil
}

// memoryEntryToProto converts a memory.Entry to its proto representation.
func memoryEntryToProto(e *memory.Entry) *feinov1.MemoryEntryProto {
	return &feinov1.MemoryEntryProto{
		Id:        e.ID,
		Category:  string(e.Category),
		Content:   e.Content,
		CreatedAt: timestamppb.New(e.CreatedAt),
		UpdatedAt: timestamppb.New(e.UpdatedAt),
	}
}

// ── File RPCs ─────────────────────────────────────────────────────────────────

// UploadFile stores a file upload and returns an opaque token.
func (h *FeinoServiceHandler) UploadFile(
	_ context.Context,
	req *connect.Request[feinov1.UploadFileRequest],
) (*connect.Response[feinov1.UploadFileResponse], error) {
	resp, err := h.fileSvc.Upload(req.Msg.GetFilename(), req.Msg.GetContent())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(resp), nil
}

// ListFiles lists directory entries on the server.
func (h *FeinoServiceHandler) ListFiles(
	_ context.Context,
	req *connect.Request[feinov1.ListFilesRequest],
) (*connect.Response[feinov1.ListFilesResponse], error) {
	base := h.sess.Config().Context.WorkingDir
	entries, base, err := listEntries(base, req.Msg.GetPath(), req.Msg.GetDirsOnly())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&feinov1.ListFilesResponse{
		Entries: entries,
		Base:    base,
	}), nil
}

// ── Plugin RPC ────────────────────────────────────────────────────────────────

// ReloadPlugins rescans the plugins directory and hot-swaps the tool set.
func (h *FeinoServiceHandler) ReloadPlugins(
	_ context.Context,
	_ *connect.Request[feinov1.ReloadPluginsRequest],
) (*connect.Response[feinov1.ReloadPluginsResponse], error) {
	n, err := h.sess.ReloadPlugins()
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&feinov1.ReloadPluginsResponse{Count: int32(n)}), nil
}

// ── Bypass mode RPCs ──────────────────────────────────────────────────────────

// SetBypassMode activates unsafe bypass mode.
func (h *FeinoServiceHandler) SetBypassMode(
	_ context.Context,
	req *connect.Request[feinov1.SetBypassModeRequest],
) (*connect.Response[feinov1.SetBypassModeResponse], error) {
	var until time.Time
	if req.Msg.GetSessionLong() {
		until = time.Time{} // time.Time zero value means session-long
	} else if d := req.Msg.GetDurationSec(); d > 0 {
		until = time.Now().Add(time.Duration(d) * time.Second)
	} else {
		until = time.Time{}
	}
	h.sess.SetBypassMode(until)
	return connect.NewResponse(&feinov1.SetBypassModeResponse{}), nil
}

// ClearBypassMode deactivates bypass mode.
func (h *FeinoServiceHandler) ClearBypassMode(
	_ context.Context,
	_ *connect.Request[feinov1.ClearBypassModeRequest],
) (*connect.Response[feinov1.ClearBypassModeResponse], error) {
	h.sess.ClearBypassMode()
	return connect.NewResponse(&feinov1.ClearBypassModeResponse{}), nil
}

// GetBypassState returns whether bypass mode is currently active.
func (h *FeinoServiceHandler) GetBypassState(
	_ context.Context,
	_ *connect.Request[feinov1.GetBypassStateRequest],
) (*connect.Response[feinov1.GetBypassStateResponse], error) {
	active := h.sess.IsBypassActive()
	return connect.NewResponse(&feinov1.GetBypassStateResponse{Active: active}), nil
}

// ── Language / Theme RPCs ─────────────────────────────────────────────────────

// SetLanguage updates the UI language preference and persists it.
func (h *FeinoServiceHandler) SetLanguage(
	_ context.Context,
	req *connect.Request[feinov1.SetLanguageRequest],
) (*connect.Response[feinov1.SetLanguageResponse], error) {
	cfg := h.sess.Config()
	cfg.UI.Language = req.Msg.GetCode()
	if err := h.sess.UpdateConfig(cfg); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	if h.cfgPath != "" {
		_ = config.Save(h.cfgPath, cfg)
	}
	return connect.NewResponse(&feinov1.SetLanguageResponse{}), nil
}

// SetTheme updates the UI theme preference and persists it.
func (h *FeinoServiceHandler) SetTheme(
	_ context.Context,
	req *connect.Request[feinov1.SetThemeRequest],
) (*connect.Response[feinov1.SetThemeResponse], error) {
	cfg := h.sess.Config()
	cfg.UI.Theme = req.Msg.GetTheme()
	if err := h.sess.UpdateConfig(cfg); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	if h.cfgPath != "" {
		_ = config.Save(h.cfgPath, cfg)
	}
	return connect.NewResponse(&feinov1.SetThemeResponse{}), nil
}

// ── Streaming Metrics RPC ─────────────────────────────────────────────────────

// StreamMetrics streams usage and latency metrics to the client.
func (h *FeinoServiceHandler) StreamMetrics(
	ctx context.Context,
	_ *connect.Request[feinov1.StreamMetricsRequest],
	stream *connect.ServerStream[feinov1.StreamMetricsResponse],
) error {
	subID := uuid.New().String()
	metricsCh, cancel := h.mhub.Subscribe(subID)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt := <-metricsCh:
			if err := stream.Send(evt); err != nil {
				return err
			}
		}
	}
}
