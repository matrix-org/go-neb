package github

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	log "github.com/Sirupsen/logrus"
	gogithub "github.com/google/go-github/github"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/services/github/client"
	"github.com/matrix-org/go-neb/services/github/webhook"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

// WebhookServiceType of the Github Webhook service.
const WebhookServiceType = "github-webhook"

// WebhookService contains the Config fields for the Github Webhook Service.
//
// Before you can set up a Github Service, you need to set up a Github Realm. This
// service does not require a syncing client.
//
// This service will send notices into a Matrix room when Github sends webhook events
// to it. It requires a public domain which Github can reach. Notices will be sent
// as the service user ID, not the ClientUserID.
//
// Example request:
//   {
//       ClientUserID: "@alice:localhost",
//       RealmID: "github-realm-id",
//       Rooms: {
//           "!qmElAGdFYCHoCJuaNt:localhost": {
//               Repos: {
//                   "matrix-org/go-neb": {
//                       Events: ["push", "issues", "pull_request", "labels"]
//                   }
//               }
//           }
//       }
//   }
type WebhookService struct {
	types.DefaultService
	webhookEndpointURL string
	// The user ID to create/delete webhooks as.
	ClientUserID string
	// The ID of an existing "github" realm. This realm will be used to obtain
	// the Github credentials of the ClientUserID.
	RealmID string
	// A map from Matrix room ID to Github "owner/repo"-style repositories.
	Rooms map[string]struct {
		// A map of "owner/repo"-style repositories to the events to listen for.
		Repos map[string]struct { // owner/repo => { events: ["push","issue","pull_request"] }
			// The webhook events to listen for. Currently supported:
			//    push : When users push to this repository.
			//    pull_request : When a pull request is made to this repository.
			//    issues : When an issue is opened/edited/closed/reopened.
			//    issue_comment : When an issue or pull request is commented on.
			//    pull_request_review_comment : When a line comment is made on a pull request.
			//    labels : When any issue or pull request is labeled/unlabeled. Unique to Go-NEB.
			//    milestones : When any issue or pull request is milestoned/demilestoned. Unique to Go-NEB.
			//    assignments : When any issue or pull request is assigned/unassigned. Unique to Go-NEB.
			// Most of these events are directly from: https://developer.github.com/webhooks/#events
			Events []string
		}
	}
	// Optional. The secret token to supply when creating the webhook. If supplied,
	// Go-NEB will perform security checks on incoming webhook requests using this token.
	SecretToken string
}

// OnReceiveWebhook receives requests from Github and possibly sends requests to Matrix as a result.
//
// If the "owner/repo" string in the webhook request case-insensitively matches a repo in this Service
// config AND the event type matches an event type registered for that repo, then a message will be sent
// into Matrix.
//
// If the "owner/repo" string doesn't exist in this Service config, then the webhook will be deleted from
// Github.
func (s *WebhookService) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *gomatrix.Client) {
	evType, repo, msg, err := webhook.OnReceiveRequest(req, s.SecretToken)
	if err != nil {
		w.WriteHeader(err.Code)
		return
	}
	logger := log.WithFields(log.Fields{
		"event": evType,
		"repo":  *repo.FullName,
	})
	repoExistsInConfig := false
	for roomID, roomConfig := range s.Rooms {
		for ownerRepo, repoConfig := range roomConfig.Repos {
			if !strings.EqualFold(*repo.FullName, ownerRepo) {
				continue
			}
			repoExistsInConfig = true // even if we don't notify for it.
			notifyRoom := false
			for _, notifyType := range repoConfig.Events {
				if evType == notifyType {
					notifyRoom = true
					break
				}
			}
			if notifyRoom {
				logger.WithFields(log.Fields{
					"message": msg,
					"room_id": roomID,
				}).Print("Sending notification to room")
				if _, e := cli.SendMessageEvent(roomID, "m.room.message", msg); e != nil {
					logger.WithError(e).WithField("room_id", roomID).Print(
						"Failed to send notification to room.")
				}
			}
		}
	}

	if !repoExistsInConfig {
		segs := strings.Split(*repo.FullName, "/")
		if len(segs) != 2 {
			logger.Error("Received event with malformed owner/repo.")
			w.WriteHeader(400)
			return
		}
		if err := s.deleteHook(segs[0], segs[1]); err != nil {
			logger.WithError(err).Print("Failed to delete webhook")
		} else {
			logger.Info("Deleted webhook")
		}
	}

	w.WriteHeader(200)
}

// Register will create webhooks for the repos specified in Rooms
//
// The hooks made are a delta between the old service and the current configuration. If all webhooks are made,
// Register() succeeds. If any webhook fails to be created, Register() fails. A delta is used to allow clients to incrementally
// build up the service config without recreating the hooks every time a change is made.
//
// Hooks are deleted when this service receives a webhook event from Github for a repo which has no user configurations.
//
// Hooks can get out of sync if a user manually deletes a hook in the Github UI. In this case, toggling the repo configuration will
// force NEB to recreate the hook.
func (s *WebhookService) Register(oldService types.Service, client *gomatrix.Client) error {
	if s.RealmID == "" || s.ClientUserID == "" {
		return fmt.Errorf("RealmID and ClientUserID is required")
	}
	realm, err := s.loadRealm()
	if err != nil {
		return err
	}

	// In order to register the GH service as a client, you must have authed with GH.
	cli := s.githubClientFor(s.ClientUserID, false)
	if cli == nil {
		return fmt.Errorf(
			"User %s does not have a Github auth session with realm %s.", s.ClientUserID, realm.ID())
	}

	// Fetch the old service list and work out the difference between the two services.
	var oldRepos []string
	if oldService != nil {
		old, ok := oldService.(*WebhookService)
		if !ok {
			log.WithFields(log.Fields{
				"service_id":   oldService.ServiceID(),
				"service_type": oldService.ServiceType(),
			}).Print("Cannot cast old github service to WebhookService")
			// non-fatal though, we'll just make the hooks
		} else {
			oldRepos = old.repoList()
		}
	}

	reposForWebhooks := s.repoList()

	// Add hooks for the newly added repos but don't remove hooks for the removed repos: we'll clean those out later
	newRepos, removedRepos := difference(reposForWebhooks, oldRepos)
	if len(reposForWebhooks) == 0 && len(removedRepos) == 0 {
		// The user didn't specify any webhooks. This may be a bug or it may be
		// a conscious decision to remove all webhooks for this service. Figure out
		// which it is by checking if we'd be removing any webhooks.
		return fmt.Errorf("No webhooks specified.")
	}
	for _, r := range newRepos {
		logger := log.WithField("repo", r)
		err := s.createHook(cli, r)
		if err != nil {
			logger.WithError(err).Error("Failed to create webhook")
			return err
		}
		logger.Info("Created webhook")
	}

	if err := s.joinWebhookRooms(client); err != nil {
		return err
	}

	log.Infof("%+v", s)

	return nil
}

// PostRegister cleans up removed repositories from the old service by
// working out the delta between the old and new hooks.
func (s *WebhookService) PostRegister(oldService types.Service) {
	// Fetch the old service list
	var oldRepos []string
	if oldService != nil {
		old, ok := oldService.(*WebhookService)
		if !ok {
			log.WithFields(log.Fields{
				"service_id":   oldService.ServiceID(),
				"service_type": oldService.ServiceType(),
			}).Print("Cannot cast old github service to WebhookService")
			return
		}
		oldRepos = old.repoList()
	}

	newRepos := s.repoList()

	// Register() handled adding the new repos, we just want to clean up after ourselves
	_, removedRepos := difference(newRepos, oldRepos)
	for _, r := range removedRepos {
		segs := strings.Split(r, "/")
		if err := s.deleteHook(segs[0], segs[1]); err != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: err,
				"repo":       r,
			}).Warn("Failed to remove webhook")
		}
	}

	// If we are not tracking any repos any more then we are back to square 1 and not doing anything
	// so remove ourselves from the database. This is safe because this is still within the critical
	// section for this service.
	if len(newRepos) == 0 {
		logger := log.WithFields(log.Fields{
			"service_type": s.ServiceType(),
			"service_id":   s.ServiceID(),
		})
		logger.Info("Removing service as no webhooks are registered.")
		if err := database.GetServiceDB().DeleteService(s.ServiceID()); err != nil {
			logger.WithError(err).Error("Failed to delete service")
		}
	}
}

func (s *WebhookService) joinWebhookRooms(client *gomatrix.Client) error {
	for roomID := range s.Rooms {
		if _, err := client.JoinRoom(roomID, "", nil); err != nil {
			// TODO: Leave the rooms we successfully joined?
			return err
		}
	}
	return nil
}

// Returns a list of "owner/repos"
func (s *WebhookService) repoList() []string {
	var repos []string
	if s.Rooms == nil {
		return repos
	}
	for _, roomConfig := range s.Rooms {
		for ownerRepo := range roomConfig.Repos {
			if strings.Count(ownerRepo, "/") != 1 {
				log.WithField("repo", ownerRepo).Error("Bad owner/repo key in config")
				continue
			}
			exists := false
			for _, r := range repos {
				if r == ownerRepo {
					exists = true
					break
				}
			}
			if !exists {
				repos = append(repos, ownerRepo)
			}
		}
	}
	return repos
}

func (s *WebhookService) createHook(cli *gogithub.Client, ownerRepo string) error {
	o := strings.Split(ownerRepo, "/")
	owner := o[0]
	repo := o[1]
	// make a hook for all GH events since we'll filter it when we receive webhook requests
	name := "web" // https://developer.github.com/v3/repos/hooks/#create-a-hook
	cfg := map[string]interface{}{
		"content_type": "json",
		"url":          s.webhookEndpointURL,
	}
	if s.SecretToken != "" {
		cfg["secret"] = s.SecretToken
	}
	events := []string{"push", "pull_request", "issues", "issue_comment", "pull_request_review_comment"}
	_, res, err := cli.Repositories.CreateHook(owner, repo, &gogithub.Hook{
		Name:   &name,
		Config: cfg,
		Events: events,
	})

	if res.StatusCode == 422 {
		errResponse, ok := err.(*gogithub.ErrorResponse)
		if !ok {
			return err
		}
		for _, ghErr := range errResponse.Errors {
			if strings.Contains(ghErr.Message, "already exists") {
				log.WithField("repo", ownerRepo).Print("422 : Hook already exists")
				return nil
			}
		}
		return err
	}

	return err
}

func (s *WebhookService) deleteHook(owner, repo string) error {
	logger := log.WithFields(log.Fields{
		"endpoint": s.webhookEndpointURL,
		"repo":     owner + "/" + repo,
	})
	logger.Info("Removing hook")

	cli := s.githubClientFor(s.ClientUserID, false)
	if cli == nil {
		logger.WithField("user_id", s.ClientUserID).Print("Cannot delete webhook: no authenticated client exists for user ID.")
		return fmt.Errorf("no authenticated client exists for user ID")
	}

	// Get a list of webhooks for this owner/repo and find the one which has the
	// same endpoint URL which is what github uses to determine equivalence.
	hooks, _, err := cli.Repositories.ListHooks(owner, repo, nil)
	if err != nil {
		return err
	}
	var hook *gogithub.Hook
	for _, h := range hooks {
		if h.Config["url"] == nil {
			logger.Print("Ignoring nil config.url")
			continue
		}
		hookURL, ok := h.Config["url"].(string)
		if !ok {
			logger.Print("Ignoring non-string config.url")
			continue
		}
		if hookURL == s.webhookEndpointURL {
			hook = h
			break
		}
	}
	if hook == nil {
		return fmt.Errorf("Failed to find hook with endpoint: %s", s.webhookEndpointURL)
	}

	_, err = cli.Repositories.DeleteHook(owner, repo, *hook.ID)
	return err
}

func sameRepos(a *WebhookService, b *WebhookService) bool {
	getRepos := func(s *WebhookService) []string {
		r := make(map[string]bool)
		for _, roomConfig := range s.Rooms {
			for ownerRepo := range roomConfig.Repos {
				r[ownerRepo] = true
			}
		}
		var rs []string
		for k := range r {
			rs = append(rs, k)
		}
		return rs
	}
	aRepos := getRepos(a)
	bRepos := getRepos(b)

	if len(aRepos) != len(bRepos) {
		return false
	}

	sort.Strings(aRepos)
	sort.Strings(bRepos)
	for i := 0; i < len(aRepos); i++ {
		if aRepos[i] != bRepos[i] {
			return false
		}
	}
	return true
}

// difference returns the elements that are only in the first list and
// the elements that are only in the second. As a side-effect this sorts
// the input lists in-place.
func difference(a, b []string) (onlyA, onlyB []string) {
	sort.Strings(a)
	sort.Strings(b)
	for {
		if len(b) == 0 {
			onlyA = append(onlyA, a...)
			return
		}
		if len(a) == 0 {
			onlyB = append(onlyB, b...)
			return
		}
		xA := a[0]
		xB := b[0]
		if xA < xB {
			onlyA = append(onlyA, xA)
			a = a[1:]
		} else if xA > xB {
			onlyB = append(onlyB, xB)
			b = b[1:]
		} else {
			a = a[1:]
			b = b[1:]
		}
	}
}

func (s *WebhookService) githubClientFor(userID string, allowUnauth bool) *gogithub.Client {
	token, err := getTokenForUser(s.RealmID, userID)
	if err != nil {
		log.WithFields(log.Fields{
			log.ErrorKey: err,
			"user_id":    userID,
			"realm_id":   s.RealmID,
		}).Print("Failed to get token for user")
	}
	if token != "" {
		return client.New(token)
	} else if allowUnauth {
		return client.New("")
	} else {
		return nil
	}
}

func (s *WebhookService) loadRealm() (types.AuthRealm, error) {
	if s.RealmID == "" {
		return nil, fmt.Errorf("Missing RealmID")
	}
	// check realm exists
	realm, err := database.GetServiceDB().LoadAuthRealm(s.RealmID)
	if err != nil {
		return nil, err
	}
	// make sure the realm is of the type we expect
	if realm.Type() != "github" {
		return nil, fmt.Errorf("Realm is of type '%s', not 'github'", realm.Type())
	}
	return realm, nil
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &WebhookService{
			DefaultService:     types.NewDefaultService(serviceID, serviceUserID, WebhookServiceType),
			webhookEndpointURL: webhookEndpointURL,
		}
	})
}
