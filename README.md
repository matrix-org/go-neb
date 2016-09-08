# Go-NEB

Go-NEB is a [Matrix](https://matrix.org) bot written in Go. It is the successor to [Matrix-NEB](https://github.com/matrix-org/Matrix-NEB), the original Matrix bot written in Python.

# Table of Contents
 * [Quick Start](#quick-start)
    * [Features](#features)
 * [Installing](#installing)
 * [Running](#running)
    * [Configuring clients](#configuring-clients)
    * [Configuring services](#configuring-services)
        * [Echo Service](#echo-service)
        * [Github Service](#github-service)
        * [Github Webhook Service](#github-webhook-service)
        * [JIRA Service](#jira-service)
        * [Giphy Service](#giphy-service)
    * [Configuring realms](#configuring-realms)
        * [Github Realm](#github-realm)
           * [Github Authentication](#github-authentication)
        * [JIRA Realm](#jira-realm)
 * [Developing](#developing)
    * [Architecture](#architecture)

# Quick Start

Clone and run (Requires Go 1.5+ and GB):

```bash
gb build github.com/matrix-org/go-neb
BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=go-neb.db BASE_URL=http://localhost:4050 bin/go-neb
```

Get a Matrix user ID and access token and give it to Go-NEB:

```bash
curl -X POST localhost:4050/admin/configureClient --data-binary '{
    "UserID": "@goneb:localhost",
    "HomeserverURL": "http://localhost:8008",
    "AccessToken": "<access_token>",
    "Sync": true,
    "AutoJoinRooms": true,
    "DisplayName": "My Bot"
}'
```

Tell it what service to run:

```bash
curl -X POST localhost:4050/admin/configureService --data-binary '{
    "Type": "echo",
    "Id": "myserviceid",
    "UserID": "@goneb:localhost",
    "Config": {}
}'
```

Invite the bot user into a Matrix room and type `!echo hello world`. It will reply with `hello world`.


## Features

### Github
 - Login with OAuth2.
 - Ability to create Github issues on any project.
 - Ability to track updates (add webhooks) to projects. This includes new issues, pull requests as well as commits.
 - Ability to expand issues when mentioned as `foo/bar#1234`.
 - Ability to assign a "default repository" for a Matrix room to allow `#1234` to automatically expand, as well as shorter issue creation command syntax.

### JIRA
 - Login with OAuth1.
 - Ability to create JIRA issues on a project.
 - Ability to expand JIRA issues when mentioned as `FOO-1234`.

### Giphy
 - Ability to query Giphy's "text-to-gif" engine.


# Installing
Go-NEB is built using Go 1.5+ and [GB](https://getgb.io/). Once you have installed Go, run the following commands:
```bash
# Install gb
go get github.com/constabulary/gb/...

# Clone the go-neb repository
git clone https://github.com/matrix-org/go-neb
cd go-neb

# Build go-neb
gb build github.com/matrix-org/go-neb
```

# Running
Go-NEB uses environment variables to configure its SQLite database and bind address. To run Go-NEB, run the following command:
```bash
BIND_ADDRESS=:4050 DATABASE_TYPE=sqlite3 DATABASE_URL=go-neb.db BASE_URL=https://public.facing.endpoint bin/go-neb
```
 - `BIND_ADDRESS` is the port to listen on.
 - `DATABASE_TYPE` MUST be "sqlite3". No other type is supported.
 - `DATABASE_URL` is where to find the database file. One will be created if it does not exist.
 - `BASE_URL` should be the public-facing endpoint that sites like Github can send webhooks to.

Go-NEB needs to be "configured" with clients and services before it will do anything useful.

## Configuring Clients
Go-NEB needs to connect as a matrix user to receive messages. Go-NEB can listen for messages as multiple matrix users. The users are configured using an HTTP API and the config is stored in the database. To create a user:
```bash
curl -X POST localhost:4050/admin/configureClient --data-binary '{
    "UserID": "@goneb:localhost:8448",
    "HomeserverURL": "http://localhost:8008",
    "AccessToken": "<access_token>",
    "Sync": true,
    "AutoJoinRooms": true,
    "DisplayName": "My Bot"
}'
```
 - `UserID` is the complete user ID of the client to connect as. The user MUST already exist.
 - `HomeserverURL` is the complete Homeserver URL for the given user ID.
 - `AccessToken` is the user's access token.
 - `Sync`, if `true`, will start a `/sync` stream so this client will receive incoming messages. This is required for services which need a live stream to the server (e.g. to respond to `!commands` and expand issues). It is not required for services which do not respond to Matrix users (e.g. webhook notifications).
 - `AutoJoinRooms`, if `true`, will automatically join rooms when an invite is received. This option is only valid when `Sync: true`.
 - `DisplayName`, if set, will set the given user's profile display name to the string given.

Go-NEB will respond with the previous configuration for this client, if one exists, as well as echo back the complete configuration for the client:

```json
{
    "OldClient": {},
    "NewClient": {
        "UserID": "@goneb:localhost:8448",
        "HomeserverURL": "http://localhost:8008",
        "AccessToken": "<access_token>",
        "Sync": true,
        "AutoJoinRooms": true,
        "DisplayName": "My Bot"
    }
}
```

## Configuring Services
Services contain all the useful functionality in Go-NEB. They require a client to operate. Services are configured using an HTTP API and the config is stored in the database. Services use one of the matrix users configured on Go-NEB to send/receive matrix messages.

Every service MUST have the following fields:
 - `Type` : The type of service. This determines which code is executed.
 - `Id` : An arbitrary string which you can use to identify this service.
 - `UserID` : A user ID of a client which has been previously configured on Go-NEB. If this user does not exist, an error will be returned.
 - `Config` : A JSON object. The contents of this object depends on the service.
 
The information about a Service can be retrieved based on their `Id` like so:
```bash
curl -X POST localhost:4050/admin/getService --data-binary '{
    "Id": "myserviceid"
}'
```
This will return:
```yaml
# HTTP 200 OK
{
    "Type": "echo",
    "Id": "myserviceid",
    "UserID": "@goneb:localhost:8448",
    "Config": {}
}
```
If the service is not found, this will return:
```yaml
# HTTP 404 Not Found
{ "message": "Service not found" }
```

If you configure an existing Service (based on ID), the entire service will be replaced with the new information.

### Echo Service
The simplest service. This will echo back any `!echo` command. To configure one:
```bash
curl -X POST localhost:4050/admin/configureService --data-binary '{
    "Type": "echo",
    "Id": "myserviceid",
    "UserID": "@goneb:localhost:8448",
    "Config": {
    }
}'
```

Then invite `@goneb:localhost:8448` to any Matrix room and it will automatically join (if the client was configured to do so). Then try typing `!echo hello world` and the bot will respond with `hello world`.

### Github Service
*Before you can set up a Github Service, you need to set up a [Github Realm](#github-realm).*

*This service [requires a client](#configuring-clients) which has `Sync: true`.*

This service will add the following command for [users who have associated their account with Github](#github-authentication):
```
!github create owner/repo "Some title" "Some description"
```

This service will also expand the following string into a short summary of the Github issue:
```
owner/repo#1234
```

You can create this service like so:

```bash
curl -X POST localhost:4050/admin/configureService --data-binary '{
    "Type": "github",
    "Id": "githubcommands",
    "UserID": "@goneb:localhost",
    "Config": {
      "RealmID": "mygithubrealm"
    }
}'
```
 - `RealmID`: The ID of the Github Realm you created earlier.
 
You can set a "default repository" for a Matrix room by sending a `m.room.bot.options` state event which has the following `content`:
```json
{
  "github": {
    "default_repo": "owner/repo"
  }
}
```
This will allow you to omit the `owner/repo` from both commands and expansions e.g `#12` will be treated as `owner/repo#12`.

### Github Webhook Service
*Before you can set up a Github Webhook Service, you need to set up a [Github Realm](#github-realm).*

This service will send notices into a Matrix room when Github sends webhook events to it. It requires a public domain which Github can reach. This service does not require a syncing client. Notices will be sent as the given `UserID`. To create this service:

```bash
curl -X POST localhost:4050/admin/configureService --data-binary '{
    "Type": "github-webhook",
    "Id": "ghwebhooks",
    "UserID": "@goneb:localhost",
    "Config": {
      "RealmID": "mygithubrealm",
      "SecretToken": "a random string",
      "ClientUserID": "@a_real_user:localhost",
      "Rooms": {
        "!wefiuwegfiuwhe:localhost": {
          "Repos": {
            "owner/repo": {
              "Events": ["push"]
            },
            "owner/another-repo": {
              "Events": ["issues"]
            }
          }
        }
      }
    }
}'
```
 - `RealmID`: The ID of the Github realm you created earlier.
 - `SecretToken`: Optional. If supplied, Go-NEB will perform security checks on incoming webhook requests using this token.
 - `ClientUserID`: The user ID of the Github user to setup webhooks as. This user MUST have [associated their user ID with a Github account](#github-authentication). Webhooks will be created using their OAuth token.
 - `Rooms`: A map of room IDs to room info.
    - `Repos`: A map of repositories to repo info.
       - `Events`: A list of webhook events to send into this room. Can be any of:
          - `push`: When users push to this repository.
          - `pull_request`: When a pull request is made to this repository.
          - `issues`: When an issue is opened/closed.
          - `issue_comment`: When an issue or pull request is commented on.
          - `pull_request_review_comment`: When a line comment is made on a pull request.

### JIRA Service
*Before you can set up a JIRA Service, you need to set up a [JIRA Realm](#jira-realm).*

TODO: Expand this section.

```
curl -X POST localhost:4050/admin/configureService --data-binary '{
    "Type": "jira",
    "Id": "jid",
    "UserID": "@goneb:localhost",
    "Config": {
        "ClientUserID": "@example:localhost",
        "Rooms": {
            "!EmwxeXCVubhskuWvaw:localhost": {
                "Realms": {
                    "jira_realm_id": {
                        "Projects": {
                            "BOTS": {
                                "Expand": true,
                                "Track": true
                            }
                        }
                    }
                }
            }
        }
    }
}'
```

### Giphy Service
A simple service that adds the ability to use the `!giphy` command. To configure one:
```bash
curl -X POST localhost:4050/admin/configureService --data-binary '{
    "Type": "giphy",
    "Id": "giphyid",
    "UserID": "@goneb:localhost",
    "Config": {
        "APIKey": "YOUR_API_KEY"
    }
}'
```
Then invite the user into a room and type `!giphy food` and it will respond with a GIF.

## Configuring Realms
Realms are how Go-NEB authenticates users on third-party websites. Every realm MUST have the following fields:
 - `ID` : An arbitrary string you can use to remember what the realm is.
 - `Type`: The type of realm. This determines what code gets executed.
 - `Config`: A JSON object. The contents depends on the realm `Type`.

They are configured like so:
```bash
curl -X POST localhost:4050/admin/configureAuthRealm --data-binary '{
    "ID": "some_arbitrary_string",
    "Type": "some_realm_type",
    "Config": {
        ...
    }
}'
```

### Github Realm
This has the `Type` of `github`. To set up this realm:
```bash
curl -X POST localhost:4050/admin/configureAuthRealm --data-binary '{
    "ID": "mygithubrealm",
    "Type": "github",
    "Config": {
        "ClientSecret": "YOUR_CLIENT_SECRET",
        "ClientID": "YOUR_CLIENT_ID",
        "StarterLink": "https://example.com/requestGithubOAuthToken"
    }
}'
```
 - `ClientSecret`: Your Github application client secret
 - `ClientID`: Your Github application client ID
 - `StarterLink`: Optional. If supplied, `!github` commands will return this link whenever someone is prompted to login to Github.

#### Github authentication
Once you have configured a Github realm, you can associate any Matrix user ID with any Github user. To do this:
```bash
curl -X POST localhost:4050/admin/requestAuthSession --data-binary '{
    "RealmID": "mygithubrealm",
    "UserID": "@real_matrix_user:localhost",
    "Config": {
        "RedirectURL": "https://optional-url.com/to/redirect/to/after/auth"
    }
}'
```
 - `UserID`: The Matrix user ID to associate with.
 - `RedirectURL`: Optional. The URL to redirect to after authentication.

This request will return an OAuth URL:
```json
{
  "URL": "https://github.com/login/oauth/authorize?client_id=abcdef&client_secret=acascacac...."
}
```

Follow this link to associate this user ID with this Github account. Once this is complete, Go-NEB will have an OAuth token for this user ID and will be able to create issues as their real Github account.

To remove this session:

```bash
curl -X POST localhost:4050/admin/removeAuthSession --data-binary '{
    "RealmID": "mygithubrealm",
    "UserID": "@real_matrix_user:localhost",
    "Config": {}
}'
```
 
### JIRA Realm
This has the `Type` of `jira`. To set up this realm:
```bash
curl -X POST localhost:4050/admin/configureAuthRealm --data-binary '{
    "ID": "jirarealm",
    "Type": "jira",
    "Config": {
        "JIRAEndpoint": "matrix.org/jira/",
        "ConsumerName": "goneb",
        "ConsumerKey": "goneb",
        "ConsumerSecret": "random_long_string",
        "PrivateKeyPEM": "-----BEGIN RSA PRIVATE KEY-----\r\nMIIEowIBAAKCAQEA39UhbOvQHEkBP9fGnhU+eSObTWBDGWygVYzbcONOlqEOTJUN\r\n8gmnellWqJO45S4jB1vLLnuXiHqEWnmaShIvbUem3QnDDqghu0gfqXHMlQr5R8ZP\r\norTt1F2idWy1wk5rVXeLKSG7uriYhDVOVS69WuefoW5v55b5YZV283v2jROjxHuj\r\ngAsJA7k6tvpYiSXApUl6YHmECfBoiwG9bwItkHwhZ\/fG9i4H8\/aOyr3WlaWbVeKX\r\n+m38lmYZvzQFRAk5ab1vzCGz4cyc\r\nTk2qmZpcjHRd1ijcOkgC23KF8lHWF5Zx0tySR+DWL1JeGm8NJxKMRJZuE8MIkJYF\r\nryE7kjspNItk6npkA3\/A4PWwElhddI4JpiuK+29mMNipRcYYy9e0vH\/igejv7ayd\r\nPLCRMQKBgBDSNWlZT0nNd2DXVqTW9p+MG72VKhDgmEwFB1acOw0lpu1XE8R1wmwG\r\nZRl\/xzri3LOW2Gpc77xu6fs3NIkzQw3v1ifYhX3OrVsCIRBbDjPQI3yYjkhGx24s\r\nVhhZ5S\/TkGk3Kw59bDC6KGqAuQAwX9req2l1NiuNaPU9rE7tf6Bk\r\n-----END RSA PRIVATE KEY-----"
    }
}'
```
 - `JIRAEndpoint`: The base URL of the JIRA installation you wish to talk to.
 - `ConsumerName`: The desired "Consumer Name" field of the "Application Links" admin page on JIRA. Generally this is the name of the service. Users will need to enter this string into their JIRA admin web form.
 - `ConsumerKey`: The desired "Consumer Key" field of the "Application Links" admin page on JIRA. Generally this is the name of the service. Users will need to enter this string into their JIRA admin web form.
 - `ConsumerSecret`: The desired "Consumer Secret" field of the "Application Links" admin page on JIRA. This should be a random long string. Users will need to enter this string into their JIRA admin web form.
 - `PrivateKeyPEM`: A string which contains the private key for performing OAuth 1.0 requests. This MUST be in PEM format. It must NOT have a password. Go-NEB will convert this into a **public** key in PEM format and return this to users. Users will need to enter the public key into their JIRA admin web form.
 - `StarterLink`: Optional. If supplied, `!jira` commands will return this link whenever someone is prompted to login to JIRA.

To generate a private key PEM: (JIRA does not support bit lengths >2048)
```bash
openssl genrsa -out privkey.pem 2048
cat privkey.pem
```

#### JIRA authentication

```
curl -X POST localhost:4050/admin/requestAuthSession --data-binary '{
    "RealmID": "jirarealm",
    "UserID": "@example:localhost",
    "Config": {
        "RedirectURL": "https://optional-url.com/to/redirect/to/after/auth"
    }
}'
```

Returns:
```json
{
    "URL":"https://jira.somewhere.com/plugins/servlet/oauth/authorize?oauth_token=7yeuierbgweguiegrTbOT"
}
```

# Developing
There's a bunch more tools this project uses when developing in order to do
things like linting. Some of them are bundled with go (fmt and vet) but some
are not. You should install the ones which are not:

```bash
go get github.com/golang/lint/golint
go get github.com/fzipp/gocyclo
```

You can then install the pre-commit hook:

```bash
./hooks/install.sh
```

    
## Architecture

```

   HOMESERVER
       |
+=============================================================+
|      |                 Go-NEB                               |
| +---------+                                                 |
| | Clients |                                                 |
| +---------+                                                 |
|      |                                                      |
| +---------+       +------------+      +--------------+      |
| | Service |-------| Auth Realm |------| Auth Session |-+    |
| +---------+       +------------+      +--------------+ |    |
|     ^                   ^              +---------------+    |
|     |                   |                                   |
+=============================================================+
      |                   |                   
    WEBHOOK            REDIRECT
    REQUEST            REQUEST
    
    
Clients      = A thing which can talk to homeservers and listen for events. /configureClient makes these.
Service      = An individual bot, configured by a user. /configureService makes these.
Auth Realm   = A place where a user can authenticate with. /configureAuthRealm makes these.
Auth Session = An individual authentication session /requestAuthSession makes these.

```


## Viewing the API docs.

```
# Start a documentation server listening on :6060
GOPATH=$GOPATH:$(pwd) godoc -v -http=localhost:6060 &

# Open up the documentation for go-neb in a browser.
sensible-browser http://localhost/pkg/github.com/matrix-org/go-neb
```
