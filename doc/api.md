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
        "version": "1.2",
        "name": "listing name",
        "description": "listing description",
        "favicon": "favicon URL" (optional),
        "source": "URL for the server source code" (optional)
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

### Session list

`GET /sessions/`

Returns (200 OK):

    [
    {
        "host": "host address",
        "port": "host port",
        "id": "session ID",
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

The following query parameters can be used:

* `?title=substring` filter sessions to those whose title contains the given substring
* `?protocol=version` show only sessions with the given protocol version (comma separated list accepted)
* `?nsfm=true` show also sessions tagged "Not Suitable For Minors"

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
        "nsfm": boolean
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

The `usernames` field contains a list of logged in users. This is an optional
field. Typically only sessions open to public will provide it.

Successful response (200 OK):

    {
        "status": "ok",
        "id": listing ID number,
        "key": "update key",
        "expires": expiration time in minutes,
        "message": "welcome message" (optional)
    }

The update key is a random string that is used as a password when refreshing this session.

The `expires` value is the number of minutes after which the listing will expire
unless refreshed. This field was added in API version 1.3. Older clients (pre 2.0)
have a fixed 5 minute refresh interval. To retain compatibility,
the expiration time should be at least 6 minutes.

Error (422 Unprocessable Entity):

    {
        "status": "error",
        "message": "human readable error message",
    }

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
        "nsfm": true
    }

All fields are optional. If the values have not changed since the last refresh,
an empty request (`{ }`) can be sent to just update the activity timestamp.

Successful response (200 OK):

    {
        "status": "ok"
    }

Error 404 Not Found is returned when the session listing is not found,
it has expired or the update key was wrong.

### Unlisting an announcement

If an announcement is not refreshed within its expiration time,
it is unlisted automatically. Announcements can also be explicitly
unlisted with a DELETE request.

The HTTP header `X-Update-Key` must be set.

`DELETE /sessions/:id/`

Returns 204 No Content on success.  
Returns the same errors as the Refresh call.

## History

Version 1.3

 * Added `expires` response field

Version 1.2

 * Added `usernames` field

Version 1.1

 * Added NSFM tag
 * Added "message" field to successful announcement reply

Version 1.0

 * First version

