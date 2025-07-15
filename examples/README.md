# Examples

These serve a dual purpose.  As executable example documentation of usage and as an acceptance suite ensuring compliance
with the system.

* [simple](simple): Shows entering and retrieving events.
* [By Kind](bykind): v1 Query to retrieve all events by a specific kind.

## Query Version 2

* [query2](query2): An example of using the query2 interface to match a subset of keys within an event

## Query Version 1
In Query Version 1 clients query for properties of documents in two forms: individual property matches and example
documents.  Each will return a set of envelopes which may be individually queried for the values.

**DEPRECATION NOTE:** _Query Version 1_ should be considered deprecated.  It was built to return envelopes with a client
querying for individual events.  This resulted in high latency with the often use case was to immediately materialize
those envelopes.  Might be a good use case for batch processing.

* [query](query): Finds envelopes of matching events with a property `word` of a specific value of the target kind.
* [queryInt](queryInt): Find envelopes matching events with a document with the given example document matching an
integer.
* [queryBatch](queryBatch): Shows processing events by matching queries of a specific kind and subset of a document.
