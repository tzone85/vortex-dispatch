package state

import "time"

// Requirement represents a high-level user requirement that gets broken into stories.
type Requirement struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	RepoPath    string    `json:"repo_path"`
	CreatedAt   time.Time `json:"created_at"`
}

// ReqFilter specifies criteria for filtering requirements from the projection store.
type ReqFilter struct {
	RepoPath        string
	ExcludeArchived bool
}

// Story represents a single unit of work derived from a requirement.
type Story struct {
	ID                 string    `json:"id"`
	ReqID              string    `json:"req_id"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	AcceptanceCriteria string    `json:"acceptance_criteria"`
	Complexity         int       `json:"complexity"`
	Status             string    `json:"status"`
	AgentID            string    `json:"agent_id"`
	Branch             string    `json:"branch"`
	PRUrl              string    `json:"pr_url"`
	PRNumber           int       `json:"pr_number"`
	OwnedFiles         []string  `json:"owned_files"`
	WaveHint           string    `json:"wave_hint"`
	Wave               int       `json:"wave"`
	EscalationTier     int       `json:"escalation_tier"`
	SplitDepth         int       `json:"split_depth"`
	CreatedAt          time.Time `json:"created_at"`
	MergedAt           time.Time `json:"merged_at"`
}

// Agent represents an AI agent that can work on stories.
type Agent struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	Model          string    `json:"model"`
	Runtime        string    `json:"runtime"`
	Status         string    `json:"status"`
	CurrentStoryID string    `json:"current_story_id"`
	SessionName    string    `json:"session_name"`
	CreatedAt      time.Time `json:"created_at"`
}

// StoryFilter specifies criteria for filtering stories from the projection store.
type StoryFilter struct {
	Status string
	ReqID  string
}
