package auth

import (
	"github.com/matrix-org/go-neb/auth/github"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
)

// RegisterModules registers all known modules so they can be retrieved via
// type.GetAuthModule
func RegisterModules(db *database.ServiceDB) {
	types.RegisterAuthModule(&github.AuthModule{Database: db})
}
