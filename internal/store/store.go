// Package store is the SQLite persistence layer: decisions, sync state, lists,
// and examples. One file, one process at a time.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  internet_message_id TEXT,
  conversation_id TEXT,
  received_at TEXT,
  from_name TEXT,
  from_addr TEXT,
  from_domain TEXT,
  subject TEXT,
  excerpt TEXT,
  signals TEXT,
  category TEXT NOT NULL,
  confidence REAL NOT NULL,
  reason TEXT,
  relationship TEXT,
  stage TEXT NOT NULL,
  provider TEXT,
  model TEXT,
  prompt_version TEXT,
  input_tokens INTEGER DEFAULT 0,
  output_tokens INTEGER DEFAULT 0,
  cost_usd REAL DEFAULT 0,
  latency_ms INTEGER DEFAULT 0,
  action TEXT NOT NULL,
  action_at TEXT,
  mode TEXT NOT NULL,
  decided_at TEXT NOT NULL,
  corrected TEXT,
  corrected_at TEXT,
  reviewed_label TEXT,
  reviewed_at TEXT,
  reviewed_source TEXT,
  judge_label TEXT,
  judge_model TEXT,
  rule TEXT,
  target_folder TEXT,
  attempts INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS decisions_decided_at ON decisions(decided_at);
CREATE INDEX IF NOT EXISTS decisions_action ON decisions(action, corrected);
CREATE TABLE IF NOT EXISTS sync_state (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS lists (
  kind TEXT NOT NULL, value TEXT NOT NULL, source TEXT, added_at TEXT NOT NULL,
  PRIMARY KEY (kind, value)
);
CREATE TABLE IF NOT EXISTS examples (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  category TEXT NOT NULL, from_domain TEXT, subject TEXT, excerpt TEXT,
  source TEXT NOT NULL, added_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS runs (
  started_at TEXT PRIMARY KEY, finished_at TEXT, new_messages INTEGER, rule_decisions INTEGER,
  llm_decisions INTEGER, would_move INTEGER, moved INTEGER, errors INTEGER, note TEXT
);
`

// List kinds.
const (
	ListAllowAddr        = "allow_addr"
	ListAllowDomain      = "allow_domain"
	ListDenyAddr         = "deny_addr"
	ListDenyDomain       = "deny_domain"
	ListCorrespondent    = "correspondent"
	ListSentConversation = "sent_conversation"
)

// Sync state keys.
const (
	KeyDeltaLink        = "delta_link"
	KeyFolderID         = "folder_id"
	KeySubfolderID      = "subfolder_id"
	KeyInboxID          = "inbox_id"
	KeyCorrespondentsAt = "correspondents_refreshed_at"
	KeyLastRunAt        = "last_run_at"
	KeyLastRunNote      = "last_run_note"
	KeyAccountUPN       = "account_upn"
	KeyImmutableIDs     = "immutable_ids"
)

// FolderKey is the sync_state key holding a subfolder's id.
func FolderKey(name string) string { return "folder:" + name }

// Actions.
const (
	ActionNone     = "none"
	ActionMove     = "move"
	ActionDeferred = "deferred"
)

type Decision struct {
	ID                string
	InternetMessageID string
	ConversationID    string
	ReceivedAt        time.Time
	FromName          string
	FromAddr          string
	FromDomain        string
	Subject           string
	Excerpt           string
	Signals           string
	Category          string
	Confidence        float64
	Reason            string
	Relationship      string
	Stage             string
	Provider          string
	Model             string
	PromptVersion     string
	InputTokens       int64
	OutputTokens      int64
	CostUSD           float64
	LatencyMS         int64
	Action            string
	ActionAt          *time.Time
	Mode              string
	DecidedAt         time.Time
	Corrected         string
	CorrectedAt       *time.Time
	ReviewedLabel     string
	ReviewedAt        *time.Time
	ReviewedSource    string // owner | judge
	JudgeLabel        string
	JudgeModel        string
	Rule              string // which rule decided, e.g. route, known_correspondent; empty for llm
	TargetFolder      string // subfolder name for a move action
	Attempts          int
}

type Example struct {
	ID         int64
	Category   string
	FromDomain string
	Subject    string
	Excerpt    string
	Source     string
	AddedAt    time.Time
}

type RunSummary struct {
	StartedAt, FinishedAt                                              time.Time
	NewMessages, RuleDecisions, LLMDecisions, WouldMove, Moved, Errors int
	Note                                                               string
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	st := &Store{db: db}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate adds columns introduced after the first release.
func (s *Store) migrate() error {
	rows, err := s.db.Query(`PRAGMA table_info(decisions)`)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		have[name] = true
	}
	rows.Close()
	for _, col := range []string{"reviewed_source TEXT", "judge_label TEXT", "judge_model TEXT", "rule TEXT", "target_folder TEXT"} {
		name := col[:len(col)-len(" TEXT")]
		if !have[name] {
			if _, err := s.db.Exec(`ALTER TABLE decisions ADD COLUMN ` + col); err != nil {
				return fmt.Errorf("migrate %s: %w", name, err)
			}
		}
	}
	return nil
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }
func tsp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}
func parseTS(v sql.NullString) *time.Time {
	if !v.Valid || v.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return nil
	}
	return &t
}

// --- sync state ---

func (s *Store) GetState(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM sync_state WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetState(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_state(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// --- lists ---

func (s *Store) AddToList(ctx context.Context, kind, value, source string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO lists(kind,value,source,added_at) VALUES(?,?,?,?)`, kind, value, source, ts(time.Now()))
	return err
}

func (s *Store) RemoveFromList(ctx context.Context, kind, value string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM lists WHERE kind=? AND value=?`, kind, value)
	return err
}

func (s *Store) InList(ctx context.Context, kind, value string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM lists WHERE kind=? AND value=?`, kind, value).Scan(&n)
	return n > 0, err
}

func (s *Store) List(ctx context.Context, kind string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT value FROM lists WHERE kind=? ORDER BY value`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ReplaceList atomically swaps the contents of a list kind.
func (s *Store) ReplaceList(ctx context.Context, kind, source string, values []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM lists WHERE kind=?`, kind); err != nil {
		return err
	}
	now := ts(time.Now())
	for _, v := range values {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO lists(kind,value,source,added_at) VALUES(?,?,?,?)`, kind, v, source, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- decisions ---

const decisionCols = `id, internet_message_id, conversation_id, received_at, from_name, from_addr, from_domain, subject, excerpt, signals,
category, confidence, reason, relationship, stage, provider, model, prompt_version, input_tokens, output_tokens, cost_usd, latency_ms,
action, action_at, mode, decided_at, corrected, corrected_at, reviewed_label, reviewed_at, reviewed_source, judge_label, judge_model, rule, target_folder, attempts`

func (s *Store) UpsertDecision(ctx context.Context, d Decision) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO decisions (`+decisionCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
 category=excluded.category, confidence=excluded.confidence, reason=excluded.reason, relationship=excluded.relationship,
 stage=excluded.stage, provider=excluded.provider, model=excluded.model, prompt_version=excluded.prompt_version,
 input_tokens=excluded.input_tokens, output_tokens=excluded.output_tokens, cost_usd=excluded.cost_usd, latency_ms=excluded.latency_ms,
 action=excluded.action, action_at=excluded.action_at, mode=excluded.mode, decided_at=excluded.decided_at, attempts=excluded.attempts,
 excerpt=excluded.excerpt, signals=excluded.signals, rule=excluded.rule, target_folder=excluded.target_folder`,
		d.ID, d.InternetMessageID, d.ConversationID, ts(d.ReceivedAt), d.FromName, d.FromAddr, d.FromDomain, d.Subject, d.Excerpt, d.Signals,
		d.Category, d.Confidence, d.Reason, d.Relationship, d.Stage, d.Provider, d.Model, d.PromptVersion, d.InputTokens, d.OutputTokens, d.CostUSD, d.LatencyMS,
		d.Action, tsp(d.ActionAt), d.Mode, ts(d.DecidedAt), nullIfEmpty(d.Corrected), tsp(d.CorrectedAt), nullIfEmpty(d.ReviewedLabel), tsp(d.ReviewedAt), nullIfEmpty(d.ReviewedSource), nullIfEmpty(d.JudgeLabel), nullIfEmpty(d.JudgeModel), nullIfEmpty(d.Rule), nullIfEmpty(d.TargetFolder), d.Attempts)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanDecision(sc interface{ Scan(...any) error }) (Decision, error) {
	var d Decision
	var receivedAt, decidedAt string
	var actionAt, correctedAt, reviewedAt, corrected, reviewedLabel, reviewedSource, judgeLabel, judgeModel, rule, targetFolder sql.NullString
	err := sc.Scan(&d.ID, &d.InternetMessageID, &d.ConversationID, &receivedAt, &d.FromName, &d.FromAddr, &d.FromDomain, &d.Subject, &d.Excerpt, &d.Signals,
		&d.Category, &d.Confidence, &d.Reason, &d.Relationship, &d.Stage, &d.Provider, &d.Model, &d.PromptVersion, &d.InputTokens, &d.OutputTokens, &d.CostUSD, &d.LatencyMS,
		&d.Action, &actionAt, &d.Mode, &decidedAt, &corrected, &correctedAt, &reviewedLabel, &reviewedAt, &reviewedSource, &judgeLabel, &judgeModel, &rule, &targetFolder, &d.Attempts)
	if err != nil {
		return d, err
	}
	d.ReceivedAt, _ = time.Parse(time.RFC3339, receivedAt)
	d.DecidedAt, _ = time.Parse(time.RFC3339, decidedAt)
	d.ActionAt, d.CorrectedAt, d.ReviewedAt = parseTS(actionAt), parseTS(correctedAt), parseTS(reviewedAt)
	d.Corrected, d.ReviewedLabel = corrected.String, reviewedLabel.String
	d.ReviewedSource, d.JudgeLabel, d.JudgeModel = reviewedSource.String, judgeLabel.String, judgeModel.String
	d.Rule, d.TargetFolder = rule.String, targetFolder.String
	return d, nil
}

func (s *Store) GetDecision(ctx context.Context, id string) (Decision, bool, error) {
	d, err := scanDecision(s.db.QueryRowContext(ctx, `SELECT `+decisionCols+` FROM decisions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, false, nil
	}
	return d, err == nil, err
}

// FindDecisionByPrefix resolves the short id the owner sees, which is the
// last characters of the message id (Graph ids share a long common prefix).
func (s *Store) FindDecisionByPrefix(ctx context.Context, short string) (Decision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+decisionCols+` FROM decisions WHERE id LIKE ? OR id = ? ORDER BY decided_at DESC LIMIT 2`, "%"+short, short)
	if err != nil {
		return Decision{}, err
	}
	defer rows.Close()
	var found []Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return Decision{}, err
		}
		found = append(found, d)
	}
	switch len(found) {
	case 0:
		return Decision{}, fmt.Errorf("no decision with id ending %q", short)
	case 1:
		return found[0], nil
	default:
		return Decision{}, fmt.Errorf("id %q is ambiguous; give more characters", short)
	}
}

func (s *Store) HasDecision(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM decisions WHERE id=? AND action<>?`, id, ActionDeferred).Scan(&n)
	return n > 0, err
}

type DecisionFilter struct {
	Limit        int
	Unreviewed   bool
	Since        time.Time
	OnlyAction   string
	OnlyLLMStage bool
	Reviewed     bool
	Uncorrected  bool
	Category     string
}

func (s *Store) ListDecisions(ctx context.Context, f DecisionFilter) ([]Decision, error) {
	q := `SELECT ` + decisionCols + ` FROM decisions WHERE 1=1`
	var args []any
	if f.Unreviewed {
		q += ` AND reviewed_label IS NULL AND stage='llm'`
	}
	if f.Reviewed {
		q += ` AND reviewed_label IS NOT NULL`
	}
	if !f.Since.IsZero() {
		q += ` AND decided_at >= ?`
		args = append(args, ts(f.Since))
	}
	if f.OnlyAction != "" {
		q += ` AND action = ?`
		args = append(args, f.OnlyAction)
	}
	if f.OnlyLLMStage {
		q += ` AND stage='llm'`
	}
	if f.Uncorrected {
		q += ` AND corrected IS NULL`
	}
	if f.Category != "" {
		q += ` AND category = ?`
		args = append(args, f.Category)
	}
	q += ` ORDER BY decided_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ConversationHasCorrespondence reports whether an earlier decision in the same
// conversation was correspondence (by rule, by the model with strong evidence,
// or confirmed by the owner). Used so a short reply in a live thread is never
// flagged on its own.
func (s *Store) ConversationHasCorrespondence(ctx context.Context, conversationID string) (bool, error) {
	if conversationID == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM decisions WHERE conversation_id=? AND (
		(category='correspondence' AND (stage='rule' OR relationship='strong' OR reviewed_label='correspondence'))
		OR reviewed_label='correspondence' OR corrected='fp')`, conversationID).Scan(&n)
	return n > 0, err
}

// PendingCandidates returns uncorrected decisions that have not been actioned:
// shadow would-moves, and model decisions whose category may now have a folder.
func (s *Store) PendingCandidates(ctx context.Context, since time.Time) ([]Decision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+decisionCols+` FROM decisions
		WHERE action_at IS NULL AND corrected IS NULL AND action <> ? AND stage IN ('llm','rule') AND decided_at >= ?
		ORDER BY decided_at`, ActionDeferred, ts(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) MarkCorrected(ctx context.Context, id, kind string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE decisions SET corrected=?, corrected_at=? WHERE id=?`, kind, ts(time.Now()), id)
	return err
}

func (s *Store) MarkReviewed(ctx context.Context, id, label, source string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE decisions SET reviewed_label=?, reviewed_at=?, reviewed_source=? WHERE id=?`, label, ts(time.Now()), source, id)
	return err
}

// SetJudge records a judge model's opinion without marking the row reviewed.
func (s *Store) SetJudge(ctx context.Context, id, label, model string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE decisions SET judge_label=?, judge_model=? WHERE id=?`, label, model, id)
	return err
}

// ReviewedSources counts reviewed rows by who reviewed them.
func (s *Store) ReviewedSources(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(reviewed_source,'owner'), COUNT(*) FROM decisions WHERE reviewed_label IS NOT NULL GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

func (s *Store) SetAction(ctx context.Context, id, action string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE decisions SET action=?, action_at=? WHERE id=?`, action, ts(at), id)
	return err
}

// CountLLMCallsSince supports the daily budget.
func (s *Store) CountLLMCallsSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM decisions WHERE stage='llm' AND decided_at >= ?`, ts(since)).Scan(&n)
	return n, err
}

// CountUnreviewed returns the number of LLM decisions nobody has marked yet.
func (s *Store) CountUnreviewed(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM decisions WHERE stage='llm' AND reviewed_label IS NULL`).Scan(&n)
	return n, err
}

func (s *Store) CostSince(ctx context.Context, since time.Time) (float64, error) {
	var c sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT SUM(cost_usd) FROM decisions WHERE decided_at >= ?`, ts(since)).Scan(&c)
	return c.Float64, err
}

// ConfirmedColdFromDomain counts uncorrected cold_outreach decisions for a domain,
// used for denylist promotion.
func (s *Store) ConfirmedColdFromDomain(ctx context.Context, domain string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM decisions WHERE from_domain=? AND category='cold_outreach' AND corrected IS NULL AND action='move'`, domain).Scan(&n)
	return n, err
}

// --- examples ---

func (s *Store) AddExample(ctx context.Context, e Example) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO examples(category,from_domain,subject,excerpt,source,added_at) VALUES(?,?,?,?,?,?)`,
		e.Category, e.FromDomain, e.Subject, e.Excerpt, e.Source, ts(time.Now()))
	return err
}

func (s *Store) Examples(ctx context.Context, limit int) ([]Example, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,category,from_domain,subject,excerpt,source,added_at FROM examples ORDER BY (source='correction') DESC, added_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Example
	for rows.Next() {
		var e Example
		var at string
		if err := rows.Scan(&e.ID, &e.Category, &e.FromDomain, &e.Subject, &e.Excerpt, &e.Source, &at); err != nil {
			return nil, err
		}
		e.AddedAt, _ = time.Parse(time.RFC3339, at)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) PruneExamples(ctx context.Context, keep int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM examples WHERE id NOT IN (
	  SELECT id FROM examples ORDER BY (source='correction') DESC, added_at DESC LIMIT ?)`, keep)
	return err
}

// --- runs ---

func (s *Store) RecordRun(ctx context.Context, r RunSummary) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO runs(started_at,finished_at,new_messages,rule_decisions,llm_decisions,would_move,moved,errors,note) VALUES(?,?,?,?,?,?,?,?,?)`,
		ts(r.StartedAt), ts(r.FinishedAt), r.NewMessages, r.RuleDecisions, r.LLMDecisions, r.WouldMove, r.Moved, r.Errors, r.Note)
	return err
}

func (s *Store) LastRun(ctx context.Context) (RunSummary, bool, error) {
	var r RunSummary
	var st, fin string
	err := s.db.QueryRowContext(ctx, `SELECT started_at,finished_at,new_messages,rule_decisions,llm_decisions,would_move,moved,errors,note FROM runs ORDER BY started_at DESC LIMIT 1`).
		Scan(&st, &fin, &r.NewMessages, &r.RuleDecisions, &r.LLMDecisions, &r.WouldMove, &r.Moved, &r.Errors, &r.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	r.StartedAt, _ = time.Parse(time.RFC3339, st)
	r.FinishedAt, _ = time.Parse(time.RFC3339, fin)
	return r, true, nil
}

// Counts returns category counts since a time, for status.
func (s *Store) Counts(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT category, COUNT(*) FROM decisions WHERE decided_at >= ? GROUP BY category`, ts(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		out[c] = n
	}
	return out, rows.Err()
}
