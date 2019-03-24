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

