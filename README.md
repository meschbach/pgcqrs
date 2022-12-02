# Postgres Persistent Event Storage
Poor man's event storage in Postgres.  You should probably prefer something like [Watermill](https://watermill.io/) over
this.  I just needed some super simple to store & recall events in Postgres without having to deal with migrations and
such for each prototypical application.

## Features
Really simple, honestly.
* Stores all events into Postgres
* Separate streams for each application
* TLS

## Usage in production
You probably should not as the following really needs to be implemented:
* Security: Authentication and Authorization. 
* DevXP is not the best.

### Setup
Run the migrator using `migrator primary` with the proper credentials.  Then you may start the service.

## Releases History
* v0.7.0 (not released yet) - Removes `kind` column.  Results in smaller database, faster inserts and queries.
* v0.6.0 - batch query version 2
