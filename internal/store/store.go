package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/zmorgan/streams/internal/stream"
)

// Store handles disk persistence for streams.
// Layout: <root>/streams/<stream-id>/stream.json + snapshots.jsonl
type Store struct {
	Root string // e.g. ~/.streams
}

// streamData is the JSON-serializable form of a stream.
type streamData struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Task          string    `json:"task"`
	Mode          string    `json:"mode"`
	Status        string    `json:"status"`
	Pipeline      []string  `json:"pipeline"`
	PipelineIndex int       `json:"pipeline_index"`
	Breakpoints   []int    `json:"breakpoints,omitempty"`
	IterStep      string    `json:"iter_step"`
	Iteration     int       `json:"iteration"`
	Converged     bool      `json:"converged"`
	BeadsParentID string    `json:"beads_parent_id"`
	BaseSHA       string    `json:"base_sha"`
	Branch        string    `json:"branch"`
	WorkTree      string    `json:"worktree"`
	SessionID     string          `json:"session_id,omitempty"`
	Notify        *notifyData     `json:"notify,omitempty"`
	LastError     *errData        `json:"last_error,omitempty"`
	PendingRevise *pendingReviseData `json:"pending_revise,omitempty"`
	Guidance      []guidanceData    `json:"guidance,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

type pendingReviseData struct {
	TargetPhaseIndex int    `json:"target_phase_index"`
	Feedback         string `json:"feedback,omitempty"`
}

type guidanceData struct {
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

type notifyData struct {
	Bell   bool `json:"bell,omitempty"`
	Flash  bool `json:"flash,omitempty"`
	System bool `json:"system,omitempty"`
}

type errData struct {
	Kind    string `json:"kind"`
	Step    string `json:"step"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

// Delete removes the stream directory from disk.
func (s *Store) Delete(id string) error {
	dir := streamDir(s.Root, id)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	return nil
}

func streamDir(root, id string) string {
	return filepath.Join(root, "streams", id)
}

// Save writes stream metadata to stream.json and appends any new snapshots
// to snapshots.jsonl. It tracks how many snapshots have been persisted so
// only new ones are appended.
func (s *Store) Save(st *stream.Stream, lastPersistedSnaps int) (int, error) {
	dir := streamDir(s.Root, st.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return lastPersistedSnaps, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// Write stream.json (full rewrite).
	data := toStreamData(st)
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return lastPersistedSnaps, fmt.Errorf("marshal stream: %w", err)
	}
	streamPath := filepath.Join(dir, "stream.json")
	if err := os.WriteFile(streamPath, raw, 0o644); err != nil {
		return lastPersistedSnaps, fmt.Errorf("write %s: %w", streamPath, err)
	}

	// Append new snapshots to snapshots.jsonl.
	snaps := st.Snapshots
	if len(snaps) > lastPersistedSnaps {
		snapPath := filepath.Join(dir, "snapshots.jsonl")
		f, err := os.OpenFile(snapPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return lastPersistedSnaps, fmt.Errorf("open %s: %w", snapPath, err)
		}
		defer f.Close()

		for i := lastPersistedSnaps; i < len(snaps); i++ {
			line, err := json.Marshal(snaps[i])
			if err != nil {
				return i, fmt.Errorf("marshal snapshot %d: %w", i, err)
			}
			line = append(line, '\n')
			if _, err := f.Write(line); err != nil {
				return i, fmt.Errorf("write snapshot %d: %w", i, err)
			}
		}
		return len(snaps), nil
	}

	return lastPersistedSnaps, nil
}

// LoadAll reads all stream directories under root and reconstructs state.
func (s *Store) LoadAll() ([]*stream.Stream, error) {
	streamsDir := filepath.Join(s.Root, "streams")
	entries, err := os.ReadDir(streamsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", streamsDir, err)
	}

	var streams []*stream.Stream
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		st, err := s.Load(entry.Name())
		if err != nil {
			slog.Warn("skipping corrupt stream directory", "id", entry.Name(), "err", err)
			continue
		}
		streams = append(streams, st)
	}
	return streams, nil
}

// Load reads a single stream from disk.
func (s *Store) Load(id string) (*stream.Stream, error) {
	dir := streamDir(s.Root, id)

	// Read stream.json.
	raw, err := os.ReadFile(filepath.Join(dir, "stream.json"))
	if err != nil {
		return nil, fmt.Errorf("read stream.json: %w", err)
	}
	var data streamData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("unmarshal stream.json: %w", err)
	}

	st := fromStreamData(data)

	// Read snapshots.jsonl.
	snapPath := filepath.Join(dir, "snapshots.jsonl")
	snapFile, err := os.Open(snapPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("open snapshots.jsonl: %w", err)
	}
	if err == nil {
		defer snapFile.Close()
		scanner := bufio.NewScanner(snapFile)
		for scanner.Scan() {
			var snap stream.Snapshot
			if err := json.Unmarshal(scanner.Bytes(), &snap); err != nil {
				return nil, fmt.Errorf("unmarshal snapshot: %w", err)
			}
			st.Snapshots = append(st.Snapshots, snap)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("scan snapshots.jsonl: %w", err)
		}
	}

	return st, nil
}

func toStreamData(st *stream.Stream) streamData {
	d := streamData{
		ID:            st.ID,
		Name:          st.Name,
		Task:          st.Task,
		Mode:          st.Mode.String(),
		Status:        st.GetStatus().String(),
		Pipeline:      st.Pipeline,
		PipelineIndex: st.PipelineIndex,
		Breakpoints:   st.Breakpoints,
		IterStep:      st.IterStep.String(),
		Iteration:     st.GetIteration(),
		Converged:     st.Converged,
		BeadsParentID: st.BeadsParentID,
		SessionID:     st.GetSessionID(),
		BaseSHA:       st.BaseSHA,
		Branch:        st.Branch,
		WorkTree:      st.WorkTree,
		CreatedAt:     st.CreatedAt,
		UpdatedAt:     st.UpdatedAt,
	}
	n := st.GetNotify()
	if n.Bell || n.Flash || n.System {
		d.Notify = &notifyData{Bell: n.Bell, Flash: n.Flash, System: n.System}
	}
	if st.LastError != nil {
		d.LastError = &errData{
			Kind:    st.LastError.Kind.String(),
			Step:    st.LastError.Step.String(),
			Message: st.LastError.Message,
			Detail:  st.LastError.Detail,
		}
	}
	if pr := st.GetPendingRevise(); pr != nil {
		d.PendingRevise = &pendingReviseData{
			TargetPhaseIndex: pr.TargetPhaseIndex,
			Feedback:         pr.Feedback,
		}
	}
	guidance := st.GetGuidance()
	if len(guidance) > 0 {
		d.Guidance = make([]guidanceData, len(guidance))
		for i, g := range guidance {
			d.Guidance[i] = guidanceData{Text: g.Text, Timestamp: g.Timestamp}
		}
	}
	return d
}

func fromStreamData(d streamData) *stream.Stream {
	st := &stream.Stream{
		ID:            d.ID,
		Name:          d.Name,
		Task:          d.Task,
		Mode:          parseMode(d.Mode),
		Pipeline:      d.Pipeline,
		PipelineIndex: d.PipelineIndex,
		Breakpoints:   d.Breakpoints,
		IterStep:      parseIterStep(d.IterStep),
		Converged:     d.Converged,
		BeadsParentID: d.BeadsParentID,
		SessionID:     d.SessionID,
		BaseSHA:       d.BaseSHA,
		Branch:        d.Branch,
		WorkTree:      d.WorkTree,
		CreatedAt:     d.CreatedAt,
		UpdatedAt:     d.UpdatedAt,
	}
	st.SetStatus(parseStatus(d.Status))
	st.SetIteration(d.Iteration)

	if d.LastError != nil {
		st.LastError = &stream.LoopError{
			Kind:    parseErrorKind(d.LastError.Kind),
			Step:    parseIterStep(d.LastError.Step),
			Message: d.LastError.Message,
			Detail:  d.LastError.Detail,
		}
	}
	if len(d.Guidance) > 0 {
		st.Guidance = make([]stream.Guidance, len(d.Guidance))
		for i, g := range d.Guidance {
			st.Guidance[i] = stream.Guidance{Text: g.Text, Timestamp: g.Timestamp}
		}
	}
	if d.PendingRevise != nil {
		st.PendingRevise = &stream.PendingRevise{
			TargetPhaseIndex: d.PendingRevise.TargetPhaseIndex,
			Feedback:         d.PendingRevise.Feedback,
		}
	}
	if d.Notify != nil {
		st.Notify = stream.NotifySettings{
			Bell:   d.Notify.Bell,
			Flash:  d.Notify.Flash,
			System: d.Notify.System,
		}
	}
	return st
}

func parseMode(s string) stream.Mode {
	switch s {
	case "Pairing":
		return stream.ModePairing
	default:
		return stream.ModeAutonomous
	}
}

func parseStatus(s string) stream.Status {
	switch s {
	case "Running":
		return stream.StatusRunning
	case "Paused":
		return stream.StatusPaused
	case "Stopped":
		return stream.StatusStopped
	default:
		return stream.StatusCreated
	}
}

func parseIterStep(s string) stream.IterStep {
	switch s {
	case "Autosquash":
		return stream.StepAutosquash
	case "Review":
		return stream.StepReview
	case "Checkpoint":
		return stream.StepCheckpoint
	case "Guidance":
		return stream.StepGuidance
	default:
		return stream.StepImplement
	}
}

func parseErrorKind(s string) stream.ErrorKind {
	switch s {
	case "Budget":
		return stream.ErrBudget
	case "Autosquash":
		return stream.ErrAutosquash
	case "NoProgress":
		return stream.ErrNoProgress
	case "Infra":
		return stream.ErrInfra
	default:
		return stream.ErrRuntime
	}
}
