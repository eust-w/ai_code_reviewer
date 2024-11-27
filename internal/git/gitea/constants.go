package gitea

// Gitea webhook event types
const (
	// Event types
	EventPullRequest = "pull_request"
	EventPush        = "push"
	EventPing        = "ping"

	// Action types for pull requests
	HookIssueOpened       = "opened"
	HookIssueSynchronized = "synchronized"
	HookIssueClosed       = "closed"
	HookIssueMerged       = "merged"
)

// HookPullRequestEvent represents a pull request webhook event from Gitea
type HookPullRequestEvent struct {
	Action        string `json:"action"`
	Number        int    `json:"number"`
	PullRequest   struct {
		ID              int64  `json:"id"`
		Number          int    `json:"number"`
		User            User   `json:"user"`
		Title           string `json:"title"`
		Body            string `json:"body"`
		Labels          []Label `json:"labels"`
		State           string `json:"state"`
		Merged          bool   `json:"merged"`
		Base            Ref    `json:"base"`
		Head            Ref    `json:"head"`
		MergeBase       string `json:"merge_base"`
		HTMLURL         string `json:"html_url"`
		DiffURL         string `json:"diff_url"`
		Additions       int    `json:"additions"`
		Deletions       int    `json:"deletions"`
		ChangedFiles    int    `json:"changed_files"`
		CreatedAt       string `json:"created_at"`
		UpdatedAt       string `json:"updated_at"`
	} `json:"pull_request"`
	Repository struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Private  bool   `json:"private"`
		Owner    User   `json:"owner"`
	} `json:"repository"`
	Sender User `json:"sender"`
}

// Ref represents a Git reference
type Ref struct {
	Sha    string `json:"sha"`
	Ref    string `json:"ref"`
	Name   string `json:"label"`
	RepoID int64  `json:"repo_id"`
}

// User represents a Gitea user
type User struct {
	ID              int64  `json:"id"`
	Login           string `json:"login"`
	Email           string `json:"email,omitempty"`
	FullName        string `json:"full_name,omitempty"`
	Username        string `json:"username"`
	AvatarURL       string `json:"avatar_url,omitempty"`
	HTMLURL         string `json:"html_url,omitempty"`
}

// Label represents a Gitea label
type Label struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	URL         string `json:"url"`
}
