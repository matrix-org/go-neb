package github

import (
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
)

// AuthModule for github
type AuthModule struct {
	Database *database.ServiceDB
}

// Type of the auth module
func (*AuthModule) Type() string {
	return "github"
}

// Process a third-party auth request
func (am *AuthModule) Process(tpa types.ThirdPartyAuth) (err error) {
	_, err = am.Database.StoreThirdPartyAuth(tpa)
	return
}
