package circleci

import "time"

// WebhookNotification is the response of a webhook notification by circleCI
type webhookNotification struct {
	Payload struct {
		VcsURL          string      `json:"vcs_url,omitempty"`
		BuildURL        string      `json:"build_url,omitempty"`
		BuildNum        int         `json:"build_num,omitempty"`
		Branch          string      `json:"branch,omitempty"`
		VcsRevision     string      `json:"vcs_revision,omitempty"`
		CommitterName   string      `json:"committer_name,omitempty"`
		CommitterEmail  string      `json:"committer_email,omitempty"`
		Subject         string      `json:"subject,omitempty"`
		Body            string      `json:"body,omitempty"`
		Why             string      `json:"why,omitempty"`
		DontBuild       interface{} `json:"dont_build,omitempty"`
		QueuedAt        time.Time   `json:"queued_at,omitempty"`
		StartTime       time.Time   `json:"start_time,omitempty"`
		StopTime        time.Time   `json:"stop_time,omitempty"`
		BuildTimeMillis int         `json:"build_time_millis,omitempty"`
		Username        string      `json:"username,omitempty"`
		Reponame        string      `json:"reponame,omitempty"`
		Lifecycle       string      `json:"lifecycle,omitempty"`
		Outcome         string      `json:"outcome,omitempty"`
		Status          string      `json:"status,omitempty"`
		RetryOf         interface{} `json:"retry_of,omitempty"`
		Steps           []struct {
			Name    string `json:"name,omitempty"`
			Actions []struct {
				BashCommand   interface{} `json:"bash_command,omitempty"`
				RunTimeMillis int         `json:"run_time_millis,omitempty"`
				StartTime     time.Time   `json:"start_time,omitempty"`
				EndTime       time.Time   `json:"end_time,omitempty"`
				Name          string      `json:"name,omitempty"`
				ExitCode      interface{} `json:"exit_code,omitempty"`
				Type          string      `json:"type,omitempty"`
				Index         int         `json:"index,omitempty"`
				Status        string      `json:"status,omitempty"`
			} `json:"actions,omitempty"`
		} `json:"steps,omitempty"`
	} `json:"payload"`
}
