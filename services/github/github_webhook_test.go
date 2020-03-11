package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/testutils"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

var roomID = "!testroom:id"

func TestGithubWebhook(t *testing.T) {
	database.SetServiceDB(&database.NopStorage{})

	// Intercept message sending to Matrix and mock responses
	msgs := []gomatrix.TextMessage{}
	matrixTrans := struct{ testutils.MockTransport }{}
	matrixTrans.RT = func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.String(), "/send/m.room.message") {
			return nil, fmt.Errorf("Unhandled URL: %s", req.URL.String())
		}
		var msg gomatrix.TextMessage
		if err := json.NewDecoder(req.Body).Decode(&msg); err != nil {
			return nil, fmt.Errorf("Failed to decode request JSON: %s", err)
		}
		msgs = append(msgs, msg)
		return &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewBufferString(`{"event_id":"$yup:event"}`)),
		}, nil
	}
	matrixCli, _ := gomatrix.NewClient("https://hyrule", "@ghwebhook:hyrule", "its_a_secret")
	matrixCli.Client = &http.Client{Transport: matrixTrans}

	// create the service
	ghwh := makeService(t)
	if ghwh == nil {
		t.Fatal("TestGithubWebhook Failed to create service")
	}

	// inject the webhook event request
	req, err := http.NewRequest(
		"POST", "https://neb.endpoint/gh-webhook-service", bytes.NewBufferString(`
			{
			  "action": "closed",
			  "issue": {
			    "url": "https://api.github.com/repos/DummyAccount/reponame/issues/15",
			    "repository_url": "https://api.github.com/repos/DummyAccount/reponame",
			    "labels_url": "https://api.github.com/repos/DummyAccount/reponame/issues/15/labels{/name}",
			    "comments_url": "https://api.github.com/repos/DummyAccount/reponame/issues/15/comments",
			    "events_url": "https://api.github.com/repos/DummyAccount/reponame/issues/15/events",
			    "html_url": "https://github.com/DummyAccount/reponame/issues/15",
			    "id": 159196956,
			    "number": 15,
			    "title": "aaaaaa",
			    "user": {
			      "login": "DummyAccount",
			      "id": 7190048,
			      "avatar_url": "https://avatars.githubusercontent.com/u/7190048?v=3",
			      "gravatar_id": "",
			      "url": "https://api.github.com/users/DummyAccount",
			      "html_url": "https://github.com/DummyAccount",
			      "followers_url": "https://api.github.com/users/DummyAccount/followers",
			      "following_url": "https://api.github.com/users/DummyAccount/following{/other_user}",
			      "gists_url": "https://api.github.com/users/DummyAccount/gists{/gist_id}",
			      "starred_url": "https://api.github.com/users/DummyAccount/starred{/owner}{/repo}",
			      "subscriptions_url": "https://api.github.com/users/DummyAccount/subscriptions",
			      "organizations_url": "https://api.github.com/users/DummyAccount/orgs",
			      "repos_url": "https://api.github.com/users/DummyAccount/repos",
			      "events_url": "https://api.github.com/users/DummyAccount/events{/privacy}",
			      "received_events_url": "https://api.github.com/users/DummyAccount/received_events",
			      "type": "User",
			      "site_admin": false
			    },
			    "labels": [

			    ],
			    "state": "closed",
			    "locked": false,
			    "assignee": null,
			    "milestone": null,
			    "comments": 1,
			    "created_at": "2016-06-08T15:40:44Z",
			    "updated_at": "2016-06-08T15:41:36Z",
			    "closed_at": "2016-06-08T15:41:36Z",
			    "body": ""
			  },
			  "repository": {
			    "id": 21138172,
			    "name": "reponame",
			    "full_name": "DummyAccount/reponame",
			    "owner": {
			      "login": "DummyAccount",
			      "id": 7190048,
			      "avatar_url": "https://avatars.githubusercontent.com/u/7190048?v=3",
			      "gravatar_id": "",
			      "url": "https://api.github.com/users/DummyAccount",
			      "html_url": "https://github.com/DummyAccount",
			      "followers_url": "https://api.github.com/users/DummyAccount/followers",
			      "following_url": "https://api.github.com/users/DummyAccount/following{/other_user}",
			      "gists_url": "https://api.github.com/users/DummyAccount/gists{/gist_id}",
			      "starred_url": "https://api.github.com/users/DummyAccount/starred{/owner}{/repo}",
			      "subscriptions_url": "https://api.github.com/users/DummyAccount/subscriptions",
			      "organizations_url": "https://api.github.com/users/DummyAccount/orgs",
			      "repos_url": "https://api.github.com/users/DummyAccount/repos",
			      "events_url": "https://api.github.com/users/DummyAccount/events{/privacy}",
			      "received_events_url": "https://api.github.com/users/DummyAccount/received_events",
			      "type": "User",
			      "site_admin": false
			    },
			    "private": false,
			    "html_url": "https://github.com/DummyAccount/reponame",
			    "description": "Android Development Device Monitor",
			    "fork": false,
			    "url": "https://api.github.com/repos/DummyAccount/reponame",
			    "forks_url": "https://api.github.com/repos/DummyAccount/reponame/forks",
			    "keys_url": "https://api.github.com/repos/DummyAccount/reponame/keys{/key_id}",
			    "collaborators_url": "https://api.github.com/repos/DummyAccount/reponame/collaborators{/collaborator}",
			    "teams_url": "https://api.github.com/repos/DummyAccount/reponame/teams",
			    "hooks_url": "https://api.github.com/repos/DummyAccount/reponame/hooks",
			    "issue_events_url": "https://api.github.com/repos/DummyAccount/reponame/issues/events{/number}",
			    "events_url": "https://api.github.com/repos/DummyAccount/reponame/events",
			    "assignees_url": "https://api.github.com/repos/DummyAccount/reponame/assignees{/user}",
			    "branches_url": "https://api.github.com/repos/DummyAccount/reponame/branches{/branch}",
			    "tags_url": "https://api.github.com/repos/DummyAccount/reponame/tags",
			    "blobs_url": "https://api.github.com/repos/DummyAccount/reponame/git/blobs{/sha}",
			    "git_tags_url": "https://api.github.com/repos/DummyAccount/reponame/git/tags{/sha}",
			    "git_refs_url": "https://api.github.com/repos/DummyAccount/reponame/git/refs{/sha}",
			    "trees_url": "https://api.github.com/repos/DummyAccount/reponame/git/trees{/sha}",
			    "statuses_url": "https://api.github.com/repos/DummyAccount/reponame/statuses/{sha}",
			    "languages_url": "https://api.github.com/repos/DummyAccount/reponame/languages",
			    "stargazers_url": "https://api.github.com/repos/DummyAccount/reponame/stargazers",
			    "contributors_url": "https://api.github.com/repos/DummyAccount/reponame/contributors",
			    "subscribers_url": "https://api.github.com/repos/DummyAccount/reponame/subscribers",
			    "subscription_url": "https://api.github.com/repos/DummyAccount/reponame/subscription",
			    "commits_url": "https://api.github.com/repos/DummyAccount/reponame/commits{/sha}",
			    "git_commits_url": "https://api.github.com/repos/DummyAccount/reponame/git/commits{/sha}",
			    "comments_url": "https://api.github.com/repos/DummyAccount/reponame/comments{/number}",
			    "issue_comment_url": "https://api.github.com/repos/DummyAccount/reponame/issues/comments{/number}",
			    "contents_url": "https://api.github.com/repos/DummyAccount/reponame/contents/{+path}",
			    "compare_url": "https://api.github.com/repos/DummyAccount/reponame/compare/{base}...{head}",
			    "merges_url": "https://api.github.com/repos/DummyAccount/reponame/merges",
			    "archive_url": "https://api.github.com/repos/DummyAccount/reponame/{archive_format}{/ref}",
			    "downloads_url": "https://api.github.com/repos/DummyAccount/reponame/downloads",
			    "issues_url": "https://api.github.com/repos/DummyAccount/reponame/issues{/number}",
			    "pulls_url": "https://api.github.com/repos/DummyAccount/reponame/pulls{/number}",
			    "milestones_url": "https://api.github.com/repos/DummyAccount/reponame/milestones{/number}",
			    "notifications_url": "https://api.github.com/repos/DummyAccount/reponame/notifications{?since,all,participating}",
			    "labels_url": "https://api.github.com/repos/DummyAccount/reponame/labels{/name}",
			    "releases_url": "https://api.github.com/repos/DummyAccount/reponame/releases{/id}",
			    "deployments_url": "https://api.github.com/repos/DummyAccount/reponame/deployments",
			    "created_at": "2014-06-23T18:51:33Z",
			    "updated_at": "2015-07-22T07:42:19Z",
			    "pushed_at": "2015-07-22T07:42:19Z",
			    "git_url": "git://github.com/DummyAccount/reponame.git",
			    "ssh_url": "git@github.com:DummyAccount/reponame.git",
			    "clone_url": "https://github.com/DummyAccount/reponame.git",
			    "svn_url": "https://github.com/DummyAccount/reponame",
			    "homepage": null,
			    "size": 725,
			    "stargazers_count": 0,
			    "watchers_count": 0,
			    "language": "Java",
			    "has_issues": true,
			    "has_downloads": true,
			    "has_wiki": true,
			    "has_pages": false,
			    "forks_count": 1,
			    "mirror_url": null,
			    "open_issues_count": 0,
			    "forks": 1,
			    "open_issues": 0,
			    "watchers": 0,
			    "default_branch": "master"
			  },
			  "sender": {
			    "login": "DummyAccount",
			    "id": 7190048,
			    "avatar_url": "https://avatars.githubusercontent.com/u/7190048?v=3",
			    "gravatar_id": "",
			    "url": "https://api.github.com/users/DummyAccount",
			    "html_url": "https://github.com/DummyAccount",
			    "followers_url": "https://api.github.com/users/DummyAccount/followers",
			    "following_url": "https://api.github.com/users/DummyAccount/following{/other_user}",
			    "gists_url": "https://api.github.com/users/DummyAccount/gists{/gist_id}",
			    "starred_url": "https://api.github.com/users/DummyAccount/starred{/owner}{/repo}",
			    "subscriptions_url": "https://api.github.com/users/DummyAccount/subscriptions",
			    "organizations_url": "https://api.github.com/users/DummyAccount/orgs",
			    "repos_url": "https://api.github.com/users/DummyAccount/repos",
			    "events_url": "https://api.github.com/users/DummyAccount/events{/privacy}",
			    "received_events_url": "https://api.github.com/users/DummyAccount/received_events",
			    "type": "User",
			    "site_admin": false
			  }
			}
		`),
	)
	if err != nil {
		t.Fatalf("TestGithubWebhook Failed to create webhook request: %s", err)
	}
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("Content-Type", "application/json")
	mockWriter := httptest.NewRecorder()
	ghwh.OnReceiveWebhook(mockWriter, req, matrixCli)

	// check response
	if mockWriter.Code != 200 {
		t.Fatalf("TestGithubWebhook Expected response 200 OK, got %d", mockWriter.Code)
	}
	if len(msgs) != 1 {
		t.Fatalf("TestGithubWebhook Expected sent 1 msg, sent %d", len(msgs))
	}
}

func makeService(t *testing.T) *WebhookService {
	srv, err := types.CreateService("id", WebhookServiceType, "@ghwebhook:hyrule", []byte(
		`{
			"ClientUserID": "@alice:hyrule",
			"RealmID": "ghrealm",
			"Rooms":{
				"`+roomID+`": {
					"Repos": {
						"DummyAccount/reponame": {
							"Events": ["issues"]
						}
					}
				}
			}
		}`,
	))
	if err != nil {
		t.Error("Failed to create GH webhook service: ", err)
		return nil
	}
	return srv.(*WebhookService)
}
