package improve

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// AuditEntry represents one finding's lifecycle in the audit log.
type AuditEntry struct {
	RunID          string `json:"run_id"`
	FindingID      string `json:"finding_id"`
	Source         string `json:"source"`
	Category       string `json:"category"`
	Title          string `json:"title"`
	Relevance      int    `json:"relevance"`
	Impact         int    `json:"impact"`
	Risk           int    `json:"risk"`
	Disposition    string `json:"disposition"`
	PRURL          string `json:"pr_url,omitempty"`
	PRStatus       string `json:"pr_status,omitempty"`
	TestsPassed    bool   `json:"tests_passed"`
	FilesChanged   int    `json:"files_changed,omitempty"`
	LinesChanged   int    `json:"lines_changed,omitempty"`
	Error          string `json:"error,omitempty"`
	Reasoning      string `json:"reasoning"`
	SecurityReview string `json:"security_review,omitempty"`
	LicenseCheck   string `json:"license_check,omitempty"`
}

// RunSummary holds per-run metadata.
type RunSummary struct {
	RunID            string    `json:"run_id"`
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at"`
	SourcesScraped   int       `json:"sources_scraped"`
	FindingsTotal    int       `json:"findings_total"`
	FindingsRelevant int       `json:"findings_relevant"`
	FindingsAnalyzed int       `json:"findings_analyzed"`
	PRsCreated       int       `json:"prs_created"`
	PRsProposed      int       `json:"prs_proposed"`
	Errors           []string  `json:"errors"`
	EmailSent        bool      `json:"email_sent"`
}

// AuditLog reads and writes the JSONL audit log.
type AuditLog struct {
	dir  string
	path string
}

// NewAuditLog creates an audit log writer for the given directory.
func NewAuditLog(dir string) *AuditLog {
	return &AuditLog{
		dir:  dir,
		path: filepath.Join(dir, "changelog.jsonl"),
	}
}

// Append writes a single entry to the JSONL file.
func (a *AuditLog) Append(entry AuditEntry) error {
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}

	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write entry: %w", err)
	}
	return f.Sync()
}

// ReadAll returns all entries in the audit log.
func (a *AuditLog) ReadAll() ([]AuditEntry, error) {
	return a.readFiltered(func(_ AuditEntry) bool { return true })
}

// ReadSince returns entries with RunID timestamps after the given time.
func (a *AuditLog) ReadSince(since time.Time) ([]AuditEntry, error) {
	return a.readFiltered(func(e AuditEntry) bool {
		t, err := time.Parse(time.RFC3339, e.RunID)
		if err != nil {
			return false
		}
		return t.After(since) || t.Equal(since)
	})
}

func (a *AuditLog) readFiltered(keep func(AuditEntry) bool) ([]AuditEntry, error) {
	f, err := os.Open(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	var entries []AuditEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry AuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			log.Printf("[improve] malformed audit log line in %s: %v", a.path, err)
			continue
		}
		if keep(entry) {
			entries = append(entries, entry)
		}
	}
	return entries, scanner.Err()
}

// SaveRunSummary writes a run summary JSON file.
func SaveRunSummary(runsDir, date string, summary RunSummary) error {
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return fmt.Errorf("create runs dir: %w", err)
	}

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}

	return os.WriteFile(filepath.Join(runsDir, date+".json"), data, 0o644)
}

// LoadRunSummary reads a run summary JSON file.
func LoadRunSummary(runsDir, date string) (RunSummary, error) {
	data, err := os.ReadFile(filepath.Join(runsDir, date+".json"))
	if err != nil {
		return RunSummary{}, err
	}

	var summary RunSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return RunSummary{}, fmt.Errorf("unmarshal summary: %w", err)
	}
	return summary, nil
}

// IsRunComplete checks if today's run has already completed successfully.
func IsRunComplete(runsDir, date string) bool {
	summary, err := LoadRunSummary(runsDir, date)
	if err != nil {
		return false
	}
	return summary.EmailSent
}
