package client

import (
	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

// TrimmedRepository represents a cut-down version of github.Repository with only the keys the end-user is
// likely to want.
type TrimmedRepository struct {
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	Private     *bool             `json:"private"`
	HTMLURL     *string           `json:"html_url"`
	CreatedAt   *github.Timestamp `json:"created_at"`
	UpdatedAt   *github.Timestamp `json:"updated_at"`
	PushedAt    *github.Timestamp `json:"pushed_at"`
	Fork        *bool             `json:"fork"`
	FullName    *string           `json:"full_name"`
	Permissions *map[string]bool  `json:"permissions"`
}

// TrimRepository trims a github repo into important fields only.
func TrimRepository(repo *github.Repository) TrimmedRepository {
	return TrimmedRepository{
		Name:        repo.Name,
		Description: repo.Description,
		Private:     repo.Private,
		HTMLURL:     repo.HTMLURL,
		CreatedAt:   repo.CreatedAt,
		UpdatedAt:   repo.UpdatedAt,
		PushedAt:    repo.PushedAt,
		Permissions: repo.Permissions,
		Fork:        repo.Fork,
		FullName:    repo.FullName,
	}
}

// New returns a github Client which can perform Github API operations.
// If `token` is empty, a non-authenticated client will be created. This should be
// used sparingly where possible as you only get 60 requests/hour like that (IP locked).
func New(token string) *github.Client {
	var tokenSource oauth2.TokenSource
	if token != "" {
		tokenSource = oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
	}
	httpCli := oauth2.NewClient(oauth2.NoContext, tokenSource)
	return github.NewClient(httpCli)
}
