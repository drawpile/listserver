#!/usr/bin/python3
"""Drawpile list server test tool: get announcement list

Usage: getlist.py <list server url>

Prints a formatted table of sessions
"""

import argparse
import requests
import json
import sys
import datetime
import time

try:
    import prettytable
    has_prettytable = True
except ImportError:
    has_prettytable = False
    print ("warning: PrettyTable package not installed", file=sys.stderr)

def get_session_list(server_url, nsfm=False, protocol='', title=''):
    """Query the session list

    Params:
    server_url -- API root address of the listing server
    nsfm       -- Include NSFM sessions
    protocol   -- Filter by protocol version (comma separated list accepted)
    title      -- Filter by title

    Returns:
    list of sessions
    """
    if server_url[-1] != '/':
        server_url += '/'

    params = {
        'protocol': protocol,
        'title': title,
        'nsfm': 'true' if nsfm else 'false'
    }
    r = requests.get(server_url + "sessions/", params)

    return r.json()

def print_table(sessions):
    """Print a pretty session list table"""
    table = prettytable.PrettyTable(("Host", "Port", "Id", "Room", "Owner", "Users", "⚑", "Title", "Age"))
    for s in sessions:
        table.add_row((
            s['host'],
            s['port'],
            s['id'],
            s.get('roomcode', ''),
            s['owner'],
            s['users'],
            ('P' if s['password'] else '') + ('X' if s['nsfm'] else ''),
            s['title'],
            _age(s['started']),
        ))

    print(table)

def _age(timestr):
    ts = datetime.datetime.strptime(timestr, '%Y-%m-%dT%H:%M:%SZ')
    d = datetime.datetime.utcnow() - ts

    mins = d.seconds // 60
    hours = mins // 60
    mins -= hours*60

    return "{}:{:02}".format(hours,mins)

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument("url", help="server URL")
    parser.add_argument("--nsfm", default=False, action="store_true", help="Show NSFM sessions")
    parser.add_argument("--protocol", default="", help="Filter by protocol (comma separated list accepted)")
    parser.add_argument("--title", default="", help="Filter by title")
    parser.add_argument("--json", default=False, action="store_true", help="Print results in JSON format")
    args = parser.parse_args()

    sessions = get_session_list(
            args.url,
            protocol=args.protocol,
            nsfm=args.nsfm,
            title=args.title,
            )

    if args.json or not has_prettytable:
        json.dump(sessions, indent=2, sort_keys=True, fp=sys.stdout)
    else:
        print_table(sessions)

