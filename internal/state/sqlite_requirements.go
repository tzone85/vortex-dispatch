package state

import (
	"database/sql"
	"fmt"
	"strings"
)

// sqlite_requirements.go contains all requirement-focused query helpers for
// SQLiteStore.  The four projection helpers added in Task 10 live here to keep
// the main sqlite.go under the 800-line style cap.  All functions are methods
// on *SQLiteStore; same package means no import changes are needed in callers.

// updateReqStatusByReqID is like updateReqStatus but reads the requirement ID
// from the "req_id" payload key (used by events that are not themselves tied
// to a specific story, e.g. EventPlanRejected).
func (s *SQLiteStore) updateReqStatusByReqID(payload map[string]any, status string) error {
	id := payloadStr(payload, "req_id")
	_, err := s.db.Exec(
		`UPDATE requirements SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status, id,
	)
	return err
}

func (s *SQLiteStore) projectReviewModeSet(payload map[string]any) error {
	id := payloadStr(payload, "req_id")
	mode := payloadStr(payload, "mode")
	_, err := s.db.Exec(
		`UPDATE requirements SET review_mode = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		mode, id,
	)
	return err
}

func (s *SQLiteStore) projectReqEstimated(payload map[string]any) error {
	// The REQ_ESTIMATED event does not carry a dedicated "req_id" field in the
	// current estimator; the requirement is identified via the "project" field
	// which maps to the project name, not the requirement ID. We store the
	// estimation numbers without requiring a row match so callers that save
	// the event before the requirement row exists do not error. In practice the
	// event is emitted standalone (not tied to a req row) so we do a best-effort
	// update using whichever ID fields are present.
	reqID := payloadStr(payload, "requirement")
	if reqID == "" {
		reqID = payloadStr(payload, "req_id")
	}
	hoursLow := payloadFloat(payload, "hours_low")
	hoursHigh := payloadFloat(payload, "hours_high")
	costLow := payloadFloat(payload, "quote_low")
	costHigh := payloadFloat(payload, "quote_high")
	_, err := s.db.Exec(
		`UPDATE requirements SET
		   estimated_hours_low = ?, estimated_hours_high = ?,
		   estimated_cost_low = ?, estimated_cost_high = ?,
		   updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		hoursLow, hoursHigh, costLow, costHigh, reqID,
	)
	return err
}

func (s *SQLiteStore) projectRecoveryCompleted(evt Event) error {
	// RECOVERY_COMPLETED is emitted per-resume run (not per-requirement) so
	// it carries no req_id. Record the timestamp on the most-recently active
	// non-terminal requirement as an informational marker. We use a sub-select
	// to work around SQLite's lack of ORDER BY in UPDATE.
	// A no-op when no matching row exists (valid — recovery may run on a blank
	// workspace).
	_, err := s.db.Exec(
		`UPDATE requirements SET recovered_at = ?
		 WHERE id = (
		   SELECT id FROM requirements
		   WHERE status IN ('planned','paused','analyzed')
		   ORDER BY created_at DESC
		   LIMIT 1
		 )`,
		evt.Timestamp,
	)
	return err
}

// payloadFloat extracts a float64 value from a decoded JSON payload map.
// It handles both float64 (JSON numbers) and int values.
func payloadFloat(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

// --- requirement query helpers ---

// GetRequirement returns a single requirement by ID.
func (s *SQLiteStore) GetRequirement(id string) (Requirement, error) {
	var req Requirement
	var recoveredAt sql.NullTime
	err := s.db.QueryRow(
		`SELECT id, title, description, status, repo_path,
		        review_mode, estimated_hours_low, estimated_hours_high,
		        estimated_cost_low, estimated_cost_high, recovered_at,
		        created_at
		 FROM requirements WHERE id = ?`,
		id,
	).Scan(
		&req.ID, &req.Title, &req.Description, &req.Status, &req.RepoPath,
		&req.ReviewMode, &req.EstimatedHoursLow, &req.EstimatedHoursHigh,
		&req.EstimatedCostLow, &req.EstimatedCostHigh, &recoveredAt,
		&req.CreatedAt,
	)
	if err != nil {
		return Requirement{}, fmt.Errorf("get requirement %s: %w", id, err)
	}
	if recoveredAt.Valid {
		req.RecoveredAt = recoveredAt.Time
	}
	return req, nil
}

// ListRequirements returns all requirements ordered by creation time.
func (s *SQLiteStore) ListRequirements() ([]Requirement, error) {
	return s.ListRequirementsFiltered(ReqFilter{})
}

// ListRequirementsFiltered returns requirements matching the given filter,
// ordered by creation time.
func (s *SQLiteStore) ListRequirementsFiltered(filter ReqFilter) ([]Requirement, error) {
	query := `SELECT id, title, description, status, repo_path,
	                 review_mode, estimated_hours_low, estimated_hours_high,
	                 estimated_cost_low, estimated_cost_high, recovered_at,
	                 created_at FROM requirements`
	var conditions []string
	var args []any

	if filter.RepoPath != "" {
		conditions = append(conditions, "repo_path = ?")
		args = append(args, filter.RepoPath)
	}
	if filter.ExcludeArchived {
		conditions = append(conditions, "status != 'archived'")
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list requirements: %w", err)
	}
	defer rows.Close()

	var reqs []Requirement
	for rows.Next() {
		var req Requirement
		var recoveredAt sql.NullTime
		if err := rows.Scan(
			&req.ID, &req.Title, &req.Description, &req.Status, &req.RepoPath,
			&req.ReviewMode, &req.EstimatedHoursLow, &req.EstimatedHoursHigh,
			&req.EstimatedCostLow, &req.EstimatedCostHigh, &recoveredAt,
			&req.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan requirement: %w", err)
		}
		if recoveredAt.Valid {
			req.RecoveredAt = recoveredAt.Time
		}
		reqs = append(reqs, req)
	}
	return reqs, rows.Err()
}
