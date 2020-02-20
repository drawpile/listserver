# Drawpile Session Listing Server

This server provides a simple RESTful API for publicly listing Drawpile sessions.

It does two things:

1. Receive and list session announcements (the "List at" option in Drawpile's Host dialog)
2. Proxy the live session list from a Drawpile server

The listings served by listserver are shown in Drawpile's Join dialog and can also be shown on a website.

## Installation

1. Install the listserver: `go get github.com/drawpile/listserver`
2. Write a configuration file (see `example.cfg`)
3. Run the listing server (`$GOPATH/bin/listserver -c myconfig.cfg`)

Alternatively, configuration can be passed as environment variables:

    LS_LISTEN=localhost:8081 ./listserver


Sample systemd unit file (`/etc/systemd/system/drawpile-listserver.service`):

	[Unit]
	Description=Drawpile session listing server
	After=network.target

	[Service]
	ExecStart=/home/website/go/bin/listserver -c /home/website/listserver.cfg
	User=website

	[Install]
	WantedBy=multi-user.target

## Configuration

See `example.cfg` for a sample configuration with all the possible settings.
Typically, you only need to set those you need.

The list server supports three different kinds of listings:

1. Public announcements
2. Private listings (accesssible via room code only)
3. Live server sessions

If the `includeServers` config key is set, listserver will use drawpile-srv's
web admin API to fetch that server's session list and include it in the results.

If `database` is set to `none`, listserver will be in read-only mode: sessions
cannot be listed manually, but ones fetched directly from a server will be shown.
(Note that you should always enable at least one of these options, as otherwise listserver does nothing.)

At a minimum you should set the following configuration settings:

 * `listen` (the server's local address)
 * `database` and/or `includeservers` (what to list)
 * `name` the short name of the list server shown to the user
 * `description` a short description of this server shown to the user
 * `proxyHeaders=true` since you will most likely be using nginx or apache in front of this server

Tip: on the same domain as your drawpile-srv, add this meta tag to your /index.html:

	<meta name="drawpile:list-server" content="https://your-list-server-url-here/">

When you click "Add" in Drawpile's join dialog when your server is selected, Drawpile will
fetch the root index page and automatically find and add the list server.

## Banning hosts

Hosts can be banned from announcements by adding the hostname to the `hostbans` table.
Optionally, an expiration time can be given. If NULL, the ban does not expire. The `notes`
column can be used for freeform notes about the ban.

## Using with nginx

In your nginx virtual host config, add a proxy pass location like this:

	location /listing/ {
		proxy_pass http://127.0.0.1:8080/;
		proxy_redirect default;
		proxy_set_header X-Real-IP $remote_addr;
	}

And set these settings in your listserver config file:

	listen = "127.0.0.1:8080"
	proxyHeaders = true

The proxyheaders setting is critical: the remote address
of the connection will be the address of the nginx server, so the
original client address must be passed in the HTTP header.

