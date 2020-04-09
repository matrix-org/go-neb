package webhook

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/google/go-github/github"
	"github.com/matrix-org/gomatrix"
	"github.com/matrix-org/util"
	log "github.com/sirupsen/logrus"
)

// OnReceiveRequest processes incoming github webhook requests and returns a
// matrix message to send, along with parsed repo information.
// The secretToken, if supplied, will be used to verify the request is from
// Github. If it isn't, an error is returned.
func OnReceiveRequest(r *http.Request, secretToken string) (string, *github.Repository, *gomatrix.HTMLMessage, *util.JSONResponse) {
	// Verify the HMAC signature if NEB was configured with a secret token
	eventType := r.Header.Get("X-GitHub-Event")
	signatureSHA1 := r.Header.Get("X-Hub-Signature")
	content, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.WithError(err).Print("Failed to read Github webhook body")
		resErr := util.MessageResponse(400, "Failed to parse body")
		return "", nil, nil, &resErr
	}
	// Verify request if a secret token has been supplied.
	if secretToken != "" {
		sigHex := strings.Split(signatureSHA1, "=")[1]
		var sigBytes []byte
		sigBytes, err = hex.DecodeString(sigHex)
		if err != nil {
			log.WithError(err).WithField("X-Hub-Signature", sigHex).Print(
				"Failed to decode signature as hex.")
			resErr := util.MessageResponse(400, "Failed to decode signature")
			return "", nil, nil, &resErr
		}

		if !checkMAC([]byte(content), sigBytes, []byte(secretToken)) {
			log.WithFields(log.Fields{
				"X-Hub-Signature": signatureSHA1,
			}).Print("Received Github event which failed MAC check.")
			resErr := util.MessageResponse(403, "Bad signature")
			return "", nil, nil, &resErr
		}
	}

	log.WithFields(log.Fields{
		"event_type": eventType,
		"signature":  signatureSHA1,
	}).Print("Received Github event")

	if eventType == "ping" {
		// Github will send a "ping" event when the webhook is first created. We need
		// to return a 200 in order for the webhook to be marked as "up" (this doesn't
		// affect delivery, just the tick/cross status flag).
		res := util.MessageResponse(200, "pong")
		return "", nil, nil, &res
	}

	htmlStr, repo, refinedType, err := parseGithubEvent(eventType, content)
	if err != nil {
		log.WithError(err).Print("Failed to parse github event")
		resErr := util.MessageResponse(500, "Failed to parse github event")
		return "", nil, nil, &resErr
	}

	msg := gomatrix.GetHTMLMessage("m.notice", htmlStr)

	return refinedType, repo, &msg, nil
}

// checkMAC reports whether messageMAC is a valid HMAC tag for message.
func checkMAC(message, messageMAC, key []byte) bool {
	mac := hmac.New(sha1.New, key)
	mac.Write(message)
	expectedMAC := mac.Sum(nil)
	return hmac.Equal(messageMAC, expectedMAC)
}

// parseGithubEvent parses a github event type and JSON data and returns an explanatory
// HTML string, the github repository and the refined event type, or an error.
func parseGithubEvent(eventType string, data []byte) (string, *github.Repository, string, error) {
	if eventType == "pull_request" {
		var ev github.PullRequestEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return "", nil, eventType, err
		}
		refinedEventType := refineEventType(eventType, ev.Action)
		return pullRequestHTMLMessage(ev), ev.Repo, refinedEventType, nil
	} else if eventType == "issues" {
		var ev github.IssuesEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return "", nil, eventType, err
		}
		refinedEventType := refineEventType(eventType, ev.Action)
		return issueHTMLMessage(ev), ev.Repo, refinedEventType, nil
	} else if eventType == "push" {
		var ev github.PushEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return "", nil, eventType, err
		}

		// The 'push' event repository format is subtly different from normal, so munge the bits we need.
		fullName := *ev.Repo.Owner.Name + "/" + *ev.Repo.Name
		repo := github.Repository{
			Owner: &github.User{
				Login: ev.Repo.Owner.Name,
			},
			Name:     ev.Repo.Name,
			FullName: &fullName,
		}
		return pushHTMLMessage(ev), &repo, eventType, nil
	} else if eventType == "issue_comment" {
		var ev github.IssueCommentEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return "", nil, eventType, err
		}
		return issueCommentHTMLMessage(ev), ev.Repo, eventType, nil
	} else if eventType == "pull_request_review_comment" {
		var ev github.PullRequestReviewCommentEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return "", nil, eventType, err
		}
		return prReviewCommentHTMLMessage(ev), ev.Repo, eventType, nil
	}
	return "", nil, eventType, fmt.Errorf("Unrecognized event type")
}

func refineEventType(eventType string, action *string) string {
	if action == nil {
		return eventType
	}
	a := *action
	if a == "assigned" || a == "unassigned" {
		return "assignments"
	} else if a == "milestoned" || a == "demilestoned" {
		return "milestones"
	} else if a == "labeled" || a == "unlabeled" {
		return "labels"
	}
	return eventType
}

func pullRequestHTMLMessage(p github.PullRequestEvent) string {
	var actionTarget string
	if p.PullRequest.Assignee != nil && p.PullRequest.Assignee.Login != nil {
		actionTarget = fmt.Sprintf(" to %s", *p.PullRequest.Assignee.Login)
	}
	return fmt.Sprintf(
		"[<u>%s</u>] %s %s <b>pull request #%d</b>: %s [%s]%s - %s",
		html.EscapeString(*p.Repo.FullName),
		html.EscapeString(*p.Sender.Login),
		html.EscapeString(*p.Action),
		*p.Number,
		html.EscapeString(*p.PullRequest.Title),
		html.EscapeString(*p.PullRequest.State),
		html.EscapeString(actionTarget),
		html.EscapeString(*p.PullRequest.HTMLURL),
	)
}

func issueHTMLMessage(p github.IssuesEvent) string {
	var actionTarget string
	if p.Issue.Assignee != nil && p.Issue.Assignee.Login != nil {
		actionTarget = fmt.Sprintf(" to %s", *p.Issue.Assignee.Login)
	}
	action := html.EscapeString(*p.Action)
	if p.Label != nil && (*p.Action == "labeled" || *p.Action == "unlabeled") {
		action = *p.Action + " [" + html.EscapeString(*p.Label.Name) + "] to"
	}
	return fmt.Sprintf(
		"[<u>%s</u>] %s %s <b>issue #%d</b>: %s [%s]%s - %s",
		html.EscapeString(*p.Repo.FullName),
		html.EscapeString(*p.Sender.Login),
		action,
		*p.Issue.Number,
		html.EscapeString(*p.Issue.Title),
		html.EscapeString(*p.Issue.State),
		html.EscapeString(actionTarget),
		html.EscapeString(*p.Issue.HTMLURL),
	)
}

func issueCommentHTMLMessage(p github.IssueCommentEvent) string {
	var kind string
	if p.Issue.PullRequestLinks == nil {
		kind = "issue"
	} else {
		kind = "pull request"
	}

	return fmt.Sprintf(
		"[<u>%s</u>] %s commented on %s's <b>%s #%d</b>: %s - %s",
		html.EscapeString(*p.Repo.FullName),
		html.EscapeString(*p.Comment.User.Login),
		html.EscapeString(*p.Issue.User.Login),
		kind,
		*p.Issue.Number,
		html.EscapeString(*p.Issue.Title),
		html.EscapeString(*p.Issue.HTMLURL),
	)
}

func prReviewCommentHTMLMessage(p github.PullRequestReviewCommentEvent) string {
	assignee := "None"
	if p.PullRequest.Assignee != nil {
		assignee = html.EscapeString(*p.PullRequest.Assignee.Login)
	}
	return fmt.Sprintf(
		"[<u>%s</u>] %s made a line comment on %s's <b>pull request #%d</b> (assignee: %s): %s - %s",
		html.EscapeString(*p.Repo.FullName),
		html.EscapeString(*p.Sender.Login),
		html.EscapeString(*p.PullRequest.User.Login),
		*p.PullRequest.Number,
		assignee,
		html.EscapeString(*p.PullRequest.Title),
		html.EscapeString(*p.Comment.HTMLURL),
	)
}

func pushHTMLMessage(p github.PushEvent) string {
	// /refs/heads/alice/branch-name => alice/branch-name
	branch := strings.Replace(*p.Ref, "refs/heads/", "", -1)

	// this branch was deleted, no HeadCommit object and deleted=true
	if p.HeadCommit == nil && p.Deleted != nil && *p.Deleted {
		return fmt.Sprintf(
			`[<u>%s</u>] %s <b><font color="red">deleted</font> %s</b>`,
			html.EscapeString(*p.Repo.FullName),
			html.EscapeString(*p.Pusher.Name),
			html.EscapeString(branch),
		)
	}

	if p.Commits != nil && len(p.Commits) > 1 {
		// multi-commit message
		// [<repo>] <username> pushed <num> commits to <branch>: <git.io link>
		// <up to 3 commits>
		var cList []string
		for _, c := range p.Commits {
			cList = append(cList, fmt.Sprintf(
				`%s: %s`,
				html.EscapeString(nameForAuthor(c.Author)),
				html.EscapeString(*c.Message),
			))
		}
		return fmt.Sprintf(
			`[<u>%s</u>] %s pushed %d commits to <b>%s</b>: %s<br>%s`,
			html.EscapeString(*p.Repo.FullName),
			html.EscapeString(nameForAuthor(p.HeadCommit.Committer)),
			len(p.Commits),
			html.EscapeString(branch),
			html.EscapeString(*p.HeadCommit.URL),
			strings.Join(cList, "<br>"),
		)
	}

	// single commit message
	// [<repo>] <username> pushed to <branch>: <msg> - <git.io link>
	return fmt.Sprintf(
		`[<u>%s</u>] %s pushed to <b>%s</b>: %s  - %s`,
		html.EscapeString(*p.Repo.FullName),
		html.EscapeString(nameForAuthor(p.HeadCommit.Committer)),
		html.EscapeString(branch),
		html.EscapeString(*p.HeadCommit.Message),
		html.EscapeString(*p.HeadCommit.URL),
	)
}

func nameForAuthor(a *github.CommitAuthor) string {
	if a == nil {
		return ""
	}
	if a.Login != nil { // prefer to use their GH username than the name they commited as
		return *a.Login
	}
	return *a.Name
}
