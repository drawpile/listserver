#!/usr/bin/python3
"""Drawpile list server test tool: make a random announcement

Usage: announce.py [-H hostname] <list server url>

Prints the server response. Exit value is nonzero if server returned an error.
"""

import argparse
import requests
import sys
import uuid
import random
import json

def make_random_announcement(server_url, host='', port=27750, protocol='dp:4.20.1', nsfm=False, private=False, verbose=False):
    """Make a random announcement at the given server.

    Params:
    server_url -- API root address of the listing server
    host       -- The host name to announce
    port       -- The port number to announce
    protocol   -- The protocol version to announce
    verbose     -- If true, the request body is printed to stderr

    Returns:
    (response code, response body) tuple
    """
    if server_url[-1] != '/':
        server_url += '/'

    reqdata = json.dumps({
        'id': str(uuid.uuid4()),
        'host': host,
        'port': port,
        'protocol': protocol,
        'title': 'Test: ',
        'users': random.randint(1,255),
        'password': random.randint(1, 3) == 1,
        'nsfm': nsfm,
        'owner': 'tester',
        'private': private,
        }, sort_keys=True, indent=2)
    if verbose:
        print(reqdata, file=sys.stderr)

    r = requests.post(server_url + "sessions/", data=reqdata)

    return (r.status_code, r.text)

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument("url", help="server URL")
    parser.add_argument("--host", "-H", default='', help="Hostname to announce")
    parser.add_argument("--port", "-p", default='27750', help="Port to announce")
    parser.add_argument("--protocol", default='dp:4.20.1', help="Protocol version to announce")
    parser.add_argument("--nsfm", default=False, action="store_true", help="Set the NSFM tag")
    parser.add_argument("--private", default=False, action="store_true", help="Private listing")
    parser.add_argument("--verbose", "-v", default=False, action="store_true", help="Print request to stderr")
    args = parser.parse_args()

    code, body = make_random_announcement(
            args.url,
            host=args.host,
            port=int(args.port),
            protocol=args.protocol,
            nsfm=args.nsfm,
            private=args.private,
            verbose=args.verbose,
            )

    print (body)
    if code != 200:
        sys.exit(1)

