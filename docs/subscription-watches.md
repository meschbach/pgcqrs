# Subscription Watches
A client may choose to watch a stream for events meeting specific criteria.  This is useful in some systems
such as GraphQL where clients will subscribe to specific entities.

## Implementation
Behind the scenes the PGCQRS uses NATS for the initial filtering.

## Additional Ideas?
* Dynamically changing subscriptions?
