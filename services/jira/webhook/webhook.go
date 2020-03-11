package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	gojira "github.com/andygrunwald/go-jira"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/realms/jira"
	"github.com/matrix-org/util"
	log "github.com/sirupsen/logrus"
)

type jiraWebhook struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Events  []string `json:"events"`
	Filter  string   `json:"jqlFilter"`
	Exclude bool     `json:"excludeIssueDetails"`
	// These fields are populated on GET
	Enabled bool `json:"enabled"`
}

// Event represents an incoming JIRA webhook event
type Event struct {
	WebhookEvent string       `json:"webhookEvent"`
	Timestamp    int64        `json:"timestamp"`
	User         gojira.User  `json:"user"`
	Issue        gojira.Issue `json:"issue"`
}

// RegisterHook checks to see if this user is allowed to track the given projects and then tracks them.
func RegisterHook(jrealm *jira.Realm, projects []string, userID, webhookEndpointURL string) error {
	// Tracking means that a webhook may need to be created on the remote JIRA installation.
	// We need to make sure that the user has permission to do this. If they don't, it may still be okay if
	// there is an existing webhook set up for this installation by someone else, *PROVIDED* that the projects
	// they wish to monitor are "public" (accessible by not logged in users).
	//
	// The methodology for this is as follows:
	//  - If they don't have a JIRA token for the remote install, fail.
	//  - Try to GET /webhooks. If this succeeds:
	//      * The user is an admin (only admins can GET webhooks)
	//      * If there is a NEB webhook already then return success.
	//      * Else create the webhook and then return success (if creation fails then fail).
	//  - Else:
	//      * The user is NOT an admin.
	//      * Are ALL the projects in the config public? If yes:
	//         - Is there an existing config for this remote JIRA installation? If yes:
	//              * Another user has setup a webhook. We can't check if the webhook is still alive though,
	//                return success.
	//         - Else:
	//              * There is no existing NEB webhook for this JIRA installation. The user cannot create a
	//                webhook to the JIRA installation, so fail.
	//      * Else:
	//         - There are private projects in the config and the user isn't an admin, so fail.
	logger := log.WithFields(log.Fields{
		"realm_id": jrealm.ID(),
		"jira_url": jrealm.JIRAEndpoint,
		"user_id":  userID,
	})
	cli, err := jrealm.JIRAClient(userID, false)
	if err != nil {
		logger.WithError(err).Print("No JIRA client exists")
		return err // no OAuth token on this JIRA endpoint
	}
	wh, forbidden, err := getWebhook(cli, webhookEndpointURL)
	if err != nil {
		if !forbidden {
			logger.WithError(err).Print("Failed to GET webhook")
			return err
		}
		// User is not a JIRA admin (cannot GET webhooks)
		// The only way this is going to end well for this request is if all the projects
		// are PUBLIC. That is, they can be accessed directly without an access token.
		err = checkProjectsArePublic(jrealm, projects, userID)
		if err != nil {
			logger.WithError(err).Print("Failed to assert that all projects are public")
			return err
		}

		// All projects that wish to be tracked are public, but the user cannot create
		// webhooks. The only way this will work is if we already have a webhook for this
		// JIRA endpoint.
		if !jrealm.HasWebhook {
			logger.Print("No webhook exists for this realm.")
			return fmt.Errorf("Not authorised to create webhook: not an admin")
		}
		return nil
	}

	// The user is probably an admin (can query webhooks endpoint)

	if wh != nil {
		logger.Print("Webhook already exists")
		return nil // we already have a NEB webhook :D
	}
	return createWebhook(jrealm, webhookEndpointURL, userID)
}

// OnReceiveRequest is called when JIRA hits NEB with an update.
// Returns the project key and webhook event, or an error.
func OnReceiveRequest(req *http.Request) (string, *Event, *util.JSONResponse) {
	// extract the JIRA webhook event JSON
	defer req.Body.Close()
	var whe Event
	err := json.NewDecoder(req.Body).Decode(&whe)
	if err != nil {
		resErr := util.MessageResponse(400, "Failed to parse request JSON")
		return "", nil, &resErr
	}

	if err != nil {
		resErr := util.MessageResponse(400, "Failed to parse JIRA URL")
		return "", nil, &resErr
	}
	projKey := strings.Split(whe.Issue.Key, "-")[0]
	projKey = strings.ToUpper(projKey)
	return projKey, &whe, nil
}

func createWebhook(jrealm *jira.Realm, webhookEndpointURL, userID string) error {
	cli, err := jrealm.JIRAClient(userID, false)
	if err != nil {
		return err
	}

	req, err := cli.NewRequest("POST", "rest/webhooks/1.0/webhook", jiraWebhook{
		Name:    "Go-NEB",
		URL:     webhookEndpointURL,
		Events:  []string{"jira:issue_created", "jira:issue_deleted", "jira:issue_updated"},
		Filter:  "",
		Exclude: false,
	})
	if err != nil {
		return err
	}
	res, err := cli.Do(req, nil)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Creating webhook returned HTTP %d", res.StatusCode)
	}
	log.WithFields(log.Fields{
		"status_code": res.StatusCode,
		"realm_id":    jrealm.ID(),
		"jira_url":    jrealm.JIRAEndpoint,
	}).Print("Created webhook")

	// mark this on the realm and persist it.
	jrealm.HasWebhook = true
	_, err = database.GetServiceDB().StoreAuthRealm(jrealm)
	return err
}

// Get an existing JIRA webhook. Returns the hook if it exists, or an error along with a bool
// which indicates if the request to retrieve the hook is not 2xx. If it is not 2xx, it is
// forbidden (different JIRA deployments return different codes ranging from 401/403/404/500).
func getWebhook(cli *gojira.Client, webhookEndpointURL string) (*jiraWebhook, bool, error) {
	req, err := cli.NewRequest("GET", "rest/webhooks/1.0/webhook", nil)
	if err != nil {
		return nil, false, fmt.Errorf("Failed to prepare webhook request")
	}
	var webhookList []jiraWebhook
	res, err := cli.Do(req, &webhookList)
	if err != nil {
		return nil, false, fmt.Errorf("Failed to query webhooks")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, true, fmt.Errorf("Querying webhook returned HTTP %d", res.StatusCode)
	}
	log.Print("Retrieved ", len(webhookList), " webhooks")
	var nebWH *jiraWebhook
	for _, wh := range webhookList {
		if wh.URL == webhookEndpointURL {
			nebWH = &wh
			break
		}
	}
	return nebWH, false, nil
}

func checkProjectsArePublic(jrealm *jira.Realm, projects []string, userID string) error {
	publicCli, err := jrealm.JIRAClient("", true)
	if err != nil {
		return fmt.Errorf("Cannot create public JIRA client")
	}
	for _, projectKey := range projects {
		// check you can query this project with a public client
		req, err := publicCli.NewRequest("GET", "rest/api/2/project/"+projectKey, nil)
		if err != nil {
			return fmt.Errorf("Failed to create project URL for project %s", projectKey)
		}
		res, err := publicCli.Do(req, nil)
		if err != nil {
			return fmt.Errorf("Failed to query project %s", projectKey)
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return fmt.Errorf("Project %s is not public. (HTTP %d)", projectKey, res.StatusCode)
		}
	}
	return nil
}
