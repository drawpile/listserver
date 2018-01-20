#!/usr/bin/python3
"""Drawpile list server test tool: update or unlist an announcement

Usage: update.py <announcement response file(s)> <list server url> [--unlist]

If more than one announcement file is given, a batch update is made.

The announcement response file is the output of announce.py. It should be a
JSON document like:

    {
        "id": listing id
        "key": "update key",
        "updates": { ... updated fields ... }
    }

The "updates" fields is optional. Updates can be overridden with command line args.

Prints the server response. Exit value is nonzero if server returned an error.
"""

import argparse
import requests
import sys
import uuid
import random
import json

def update_announcement(server_url, listing_id, update_key, updates={}, verbose=False):
    """Update an announcement on the server

    Params:
    server_url -- API root address of the listing server
    listing_id -- Id of the listing
    update_key -- The update key
    updates    -- Updated fields to pass to the server
    verbose    -- If true, the request body is printed to stderr

    Returns:
    (response code, response body) tuple
    """
    if server_url[-1] != '/':
        server_url += '/'

    reqdata = json.dumps(updates, sort_keys=True, indent=2)
    if verbose:
        print(reqdata, file=sys.stderr)

    r = requests.put(
        "{}sessions/{}".format(server_url, listing_id),
        data=reqdata,
        headers={'X-Update-Key': update_key},
        )

    return (r.status_code, r.text)


def batch_update_announcements(server_url, listings, verbose=False):
    """Update multiple announcements on the server

    Params:
    server_url -- API root address of the listing server
    listings   -- listings to update (see below)
    verbose    -- If true, the request body is printed to stderr

    The listings parameter should be a list of dicts like this:

        {
            id: listing ID,
            key: listing update key,
            updates: a dict of fields to update (may be empty)
        }

    Returns:
    (response code, response body) tuple
    """
    if server_url[-1] != '/':
        server_url += '/'

    reqdata = json.dumps(
        {
            l['id']: {
                'updatekey': l['key'],
                **l['updates']
            } for l in listings
        },
        sort_keys=True, indent=2)

    if verbose:
        print(reqdata, file=sys.stderr)

    r = requests.put(
        server_url + "sessions/",
        data=reqdata
        )

    return (r.status_code, r.text)


def unlist_announcement(server_url, listing_id, update_key):
    """Unlist an announcement

    Params:
    server_url -- API root address of the listing server
    listing_id -- Id of the listing
    update_key -- The update key
    
    Returns:
    (response code, response body)
    """
    if server_url[-1] != '/':
        server_url += '/'

    r = requests.delete(
        "{}sessions/{}".format(server_url, listing_id),
        headers={'X-Update-Key': update_key},
        )

    return (r.status_code, r.text)

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument("announcement", nargs='+', help="announcement response file(s)")
    parser.add_argument("url", help="server URL")
    parser.add_argument("--title", help="Update title")
    parser.add_argument("--users", help="Update user count")
    parser.add_argument("--usernames", help="Update username list")
    parser.add_argument("--nsfm", help="Update the NSFM tag")
    parser.add_argument("--password", help="Update the password required tag")
    parser.add_argument("--verbose", "-v", default=False, action="store_true", help="Print request to stderr")
    parser.add_argument("--unlist", default=False, action="store_true", help="Unlist this session")
    args = parser.parse_args()

    # Read response files
    announcements = []
    for infile in args.announcement:
        with open(infile, 'r') as f:
            announcement = json.load(f)

            # Make sure the required fiels are set
            if not announcement.get('id', ''):
                print(infile, ": Listing ID not set in response file!", file=sys.stderr)
                sys.exit(1)

            if not announcement.get('key', ''):
                print(infile, ": Update key not set in response file!", file=sys.stderr)
                sys.exit(1)

            # Override updates
            updates = {}
            if args.nsfm is not None:
                updates['nsfm'] = args.nsfm == 'true'

            if args.password is not None:
                updates['password'] = args.password == 'true'

            if args.title is not None:
                updates['title'] = args.title

            if args.users is not None:
                updates['users'] = int(args.user)

            if args.usernames is not None:
                updates['usernames'] = args.usernames

            if 'updates' in announcement:
                announcement['updates'].update(updates)
            else:
                announcement['updates'] = updates

            announcements.append(announcement)

    # Make the request
    if args.unlist:
        for announcement in announcements:
            code, body = unlist_announcement(
                    args.url,
                    announcement["id"],
                    announcement["key"],
                    )

    else:
        if len(announcements) > 1:
            code, body = batch_update_announcements(
                    args.url,
                    announcements,
                    verbose=args.verbose,
                    )

        else:
            code, body = update_announcement(
                    args.url,
                    announcement["id"],
                    announcement["key"],
                    updates=updates,
                    verbose=args.verbose,
                    )

    print (body)
    if code not in (200, 204):
        sys.exit(1)

