#!/usr/bin/python3
"""Drawpile list server test tool: announce and maintain a random session

Usage: runtest.py <list server url>
"""

import argparse
import sys
import json
import time

from announce import make_random_announcement
from update import update_announcement, unlist_announcement

def run_test(server_url, host=''):
    # First, create the announcement
    print ("Announcing random session at", server_url)
    code, response = make_random_announcement(server_url, host=host)

    if code != 200:
        print("Error:", code, response)
        sys.exit(1)

    announcement = json.loads(response)
    if 'id' not in announcement or 'key' not in announcement:
        print("Invalid reply", response)
        sys.exit(1)

    if 'message' in announcement:
        print ('Message:', announcement['message'])

    refresh = int(announcement.get('expires', '10')) - 1

    # Keep refreshing the announcement until interrupted
    print ("Refreshing announcement every", refresh, "minutes... (until Ctrl+C is pressed)")

    while True:
        try:
            time.sleep(refresh * 60 + 30)
        except KeyboardInterrupt:
            break
        
        print ("Refreshing listing", announcement["id"], "at", server_url)
        code, response = update_announcement(
            server_url,
            announcement["id"],
            announcement["key"]
            )

        if code != 200:
            print("Refresh error", code, response)
            sys.exit(1)

        jsonresp = json.loads(response)

        if 'message' in jsonresp:
            print("Message:", jsonresp['message'])

    # Unlist once interrupted
    print ("\nUnlisting", announcement["id"], "at", server_url)
    code, response = unlist_announcement(
        server_url,
        announcement["id"],
        announcement["key"]
        )

    if code not in (200, 204):
        print("Unlist error:", code, response)
        sys.exit(1)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument("url", help="server URL")
    parser.add_argument("--host", "-H", default='', help="Hostname to announce")
    args = parser.parse_args()

    run_test(args.url, host=args.host)

