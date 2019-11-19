# Drawpile Session Listing Server

This server provides a simple RESTful API for publicly listing Drawpile sessions.

## Installation

1. Install the listserver: `go get github.com/drawpile/listserver`
2. Create a PostgreSQL database
3. Initialize it with `doc/createdb.sql`
4. Write a configuration file (see `example.cfg`)
5. Run the listing server (`$GOPATH/bin/listserver`)

Sample systemd unit file (`/etc/systemd/system/drawpile-listserver.service`):

	[Unit]
	Description=Drawpile session listing server
	Requires=postgresql.service
	After=network.target postgresql.service

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

If the `includeservers` config key is set, listserver will use drawpile-srv's
web admin API to fetch that server's session list and include it in the results.

If no `database` is configured, listserver will be in read-only mode: sessions
cannot be listed manually, but it will show sessions fetched directly from a server.
(Note that you should always enable at least one of these options: otherwise listserver does nothing.)

At a minimum you should set the following configuration settings:

 * `listen` (the server's local address)
 * `database` and/or `includeservers` (what to list)
 * `name` the short name of the list server shown to the user
 * `description` a short description of this server shown to the user
 * `remoteAddressHeader` since you will most likely be using nginx or apache in front of this server

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
	remoteAddressHeader = "X-Real-Ip"

The remote address header setting is critical: the remote address
of the connection will be the address of the nginx server, so the
original client address must be passed in the HTTP header.

## Changelog

2019-03-24 Version 1.5

 * Added CheckServer option: the list server can check if the listed address is actually running a reachable Drawpile server

2017-02-15 Version 1.0

 * First release (succeeds the old PHP and Python based implementations)
 * Implements API spec version 1.2

