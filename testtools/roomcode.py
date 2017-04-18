#!/usr/bin/python3
"""Drawpile list server test tool: get session details by join code

Usage: roomcode.py <list server url> <roomcode>
"""

import argparse
import requests
import json
import sys
import datetime
import time

def get_session_info(server_url, roomcode):
    """Query session info

    Params:
    server_url -- API root address of the listing server
    roomcode   -- Session room code

    Returns:
    Server response
    """
    if server_url[-1] != '/':
        server_url += '/'

    r = requests.get(server_url + "join/" + roomcode)

    return r.json()

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument("url", help="server URL")
    parser.add_argument("roomcode", help="session room code")
    args = parser.parse_args()

    response = get_session_info(args.url, args.roomcode)

    json.dump(response, indent=2, sort_keys=True, fp=sys.stdout)

