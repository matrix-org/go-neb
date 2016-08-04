package realms

import (
	"github.com/matrix-org/go-neb/types"
)

type githubRealm struct {
	id              string
	ClientSecret    string
	ClientID        string
	WebhookEndpoint string
}

func (r *githubRealm) ID() string {
	return r.id
}

func (r *githubRealm) Type() string {
	return "github"
}

func init() {
	types.RegisterAuthRealm(func(realmID string) types.AuthRealm {
		return &githubRealm{id: realmID}
	})
}
