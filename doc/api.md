Drawpile session listing web API
---------------------------------

This document describes the REST style API for publicly announcing Drawpile sessions.

## Definition of terms

* **Server** is the HTTP server providing the API
* **Host** is the Drawpile server where the session is running
* **Client** is the API consumer (which can be either a Drawpile client or server)

## Server API

The API returns JSON encoded responses. All POST and PUT requests must
have the content type `application/json`.

The given URLs are relative to the API root URL, which is server specific.

### API version

`GET /`

Returns (200 OK):

    {
        "api_name": "drawpile-session-list",
        "version": "1.6",
        "name": "listing name",
        "description": "listing description",
        "favicon": "favicon URL" (optional),
        "read_only": true|false (optional, default is false),
        "source": "URL for the server source code" (optional)
        "public": true|false (optional, default is true),
        "private": true|false (optional, default is !read_only)
    }

When a list is added to Drawpile, it makes a request to this URL to make
sure the API is of the correct type and version.

The `api_name` should be `drawpile-session-list`.
The version uses a semantic versioning scheme. The major number changes when
backwards incompatible changes are made, and the minor version when the changes
are backwards compatible. E.g. a client for version `1.0` will be able to use
API versions `1.1` or `1.9`, but not `2.0`. A client for `1.1`, however, might
not be able to use API `1.0`.

The `name` is the name that will be shown in the list selection dropdown.
The `description` text is a longer description (few sentences at most) of the list.

If the `read_only` field is present and its value is `true`, sessions from this list
will be shown in the application's Join dialog, but sessions cannot be announced here.
[Pubsrvproxy](https://github.com/drawpile/pubsrvproxy) can be used as a read-only
list server.

If the `public` field is set to `false`, this list server does not return public session
lists. If the `private` field is set to `false`, this list server does not support private listings.
(At least one of these should be `true`.)

For backward compatibility, the default value for `private` depends on whether the server is read-only.
For read-only servers, `private` is false by default. The client can use the public and private fields
to disable the relevant actions in the user interface when this server is selected.

### Session list

`GET /sessions/`

Returns (200 OK):

    [
    {
        "host": "host address",
        "port": "host port",
        "id": "session ID",
        "roomcode": "short unique ID code", (if exists for this listing)
        "protocol": "protocol version",
        "title": "session title",
        "users": number of users,
        "usernames": ["list of user names", ...],
        "password": true/false (is the session password protected),
        "nsfm": true/false (Not Suitable For Minors),
        "owner": "username",
        "started": "YYYY-MM-DD HH:MM:SS" (timestamp in ISO 8601 format, UTC+0 timezone)
    }, ...
    ]

The `host`, `port` and `id` fields form a unique key.

The username list may be empty. Password protected sessions will not typically list users.

The `roomcode` field is optional. If it is present and not empty, it contains a random
code that can be used to fetch the host, port and ID of the session. A room code is always
exactly 5 letters long and consists of characters in the range A-Z.

The following query parameters can be used:

* `?title=substring` filter sessions to those whose title contains the given substring
* `?protocol=version` show only sessions with the given protocol version (comma separated list accepted)
* `?nsfm=true` show also sessions tagged "Not Suitable For Minors"

If public listings are disabled on this server, this endpoint returns HTTP 403 Forbidden, or 404 Not Found.

### Joining information

`GET /join/:roomcode`

Returns (200 OK):
    {
        "host": "host address",
        "port": "host port",
        "id": "session ID",
    }

Fetch information needed to join a session using the room code. For private listings,
this is the only way to get the server address.

A 404 Not Found error is returned if the given code is not found on the server.

A 403 Forbidden error is returned if private listings are disabled on this server.

### Session announcement

`POST /sessions/`

The request body:

    {
        "host": "host address",
        "port": "host port",
        "id": "session id",
        "protocol": "protocol version",
        "owner": "username",
        "title": "session title",
        "users": user count,
        "usernames": ["list of user names", ...],
        "password": boolean (is the session password protected),
        "nsfm": boolean,
        "private": boolean
    }

The `id`, `protocol`, `owner` and `title` fields are required.
The title may be empty, but this is not recommended. Untitled sessions may be hidden.

When the `host` field is omitted or left blank, the IP address of the client
making the request will be used. If a host field is given, it must resolve to an
IP address matching the client's address.

If no port is specified, the default (27750) is used.

The server may optionally check that the session exists by connecting to the
announced host.

The `owner` field is the name of the user who started the session.

The `nsfm` field is used to inform that the session will contain material not
suitable for minors. The server may also implicitly apply the tag based on
words appearing in the title.

If the `private` field is present and set to `true`, the session will not be
listed in the public list, but is still accessible by room code.

The `usernames` field contains a list of logged in users. This is an optional
field. Typically only sessions open to public will provide it.

Successful response (200 OK):

    {
        "status": "ok",
        "id": listing ID number,
        "roomcode": "randomly generated room code" (optional)
        "key": "update key",
        "expires": expiration time in minutes,
        "private": true,
        "message": "welcome message" (optional)
    }

The `roomcode` is described above. The server may be configured not to generate
room codes, in which case the field is empty or omitted.

The update key is a random string that is used as a password when refreshing this session.

The `expires` value is the number of minutes after which the listing will expire
unless refreshed. This field was added in API version 1.3. Older clients (pre 2.0)
have a fixed 5 minute refresh interval. To retain compatibility,
the expiration time should be at least 6 minutes.

If the listing was made private, the `private` field is included and set to true.

Error (422 Unprocessable Entity):

    {
        "status": "error",
        "message": "human readable error message",
    }

Error 403 (Forbidden) means public listings are disabled on this server.

### Refreshing an announcement

The announcement should be refreshed every few minutes to let the server know the session is still active.

`PUT /sessions/:id/`

The `id` is the listing ID number. The HTTP header `X-Update-Key` should be set to the update key.

The request body:

    {
        "title": "new title",
        "users": new user count,
        "usernames": ["user name list", ...],
        "password": is a password required (true/false),
        "owner": "session owner's name",
        "nsfm": true,
        "private": false
    }

All fields are optional. If the values have not changed since the last refresh,
an empty request (`{ }`) can be sent to just update the activity timestamp.

Successful response (200 OK):

    {
        "status": "ok"
    }

Error 404 Not Found is returned when the session listing is not found,
it has expired or the update key was wrong.

### Batch refresh

Batch refresh is a way to refresh multiple sessions with a single query.
It is useful for servers hosting many sessions, since it avoids the
overhead of opening multiple HTTP connections.

`PUT /sessions/`

The request body:

    {
        "listing ID": {
            "updatekey": "listing update key",
            ... refresh fields (same as in individual refresh) ...
        },
        ...
    }

A 200 OK response is returned even if none of the refreshes were successful:

    {
        "status": "ok",
        "responses": {
            "listing ID": "ok" | "error",
            ...
        }
    }

### Unlisting an announcement

If an announcement is not refreshed within its expiration time,
it is unlisted automatically. Announcements can also be explicitly
unlisted with a DELETE request.

The HTTP header `X-Update-Key` must be set.

`DELETE /sessions/:id/`

Returns 204 No Content on success.  
Returns the same errors as the Refresh call.

## History

Version 1.6

 * Added `public` and `private` server info fields

Version 1.5

 * Added batch refresh endpoint

Version 1.4

 * Added `roomcode` field and endpoint
 * Added option to register private sessions (visible by roomcode only)

Version 1.3

 * Added `expires` response field

Version 1.2

 * Added `usernames` field

Version 1.1

 * Added NSFM tag
 * Added "message" field to successful announcement reply

Version 1.0

 * First version

