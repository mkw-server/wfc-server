# wfc-server

`wfc-server` is MKW-Server's fork of [WiiLink's](https://wiilink.ca/) [wfc-server](https://github.com/WiiLink24/wfc-server), a WIP open source server replacement for Nintendo Wi-Fi Connection. It has been modified to support MKW-Server's match-making system and [mkw-server](https://github.com/mkw-server/mkw-server) instance managing.

## Current Support

- Matchmaking (No server sorting yet)
- Adding Friends

## Setup

You will need:

- PostgreSQL

1. Create a PostgreSQL database. Note the database name, username, and password.
2. Use the `schema.sql` found in the root of this repo and import it into your PostgreSQL database.
3. Copy `config-example.xml` to `config.xml` and insert all the correct data.
4. Run `go build`. The resulting executable `wwfc` is the executable of the server.
