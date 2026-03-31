package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
)

// HTTPTransportLayer implements Transport using HTTP/REST.
type HTTPTransportLayer struct {
	BaseURL string
	wire    *http.Client
}

// AllEnvelopes represents a collection of all event envelopes.
type AllEnvelopes struct {
	Envelopes []Envelope `json:"envelopes"`
}

func (c *HTTPTransportLayer) post(parent context.Context, opName, resource string, requestEntity, responseEntity any) error {
	ctx, span := tracer.Start(parent, "pg-cqrs.v1:"+opName)
	defer span.End()

	requestEntityBytes, err := json.Marshal(requestEntity)
	if err != nil {
		return err
	}

	url := c.BaseURL + resource
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(requestEntityBytes))
	if err != nil {
		return err
	}

	// todo: investigate SSRF
	// nolint
	resp, err := c.wire.Do(req)
	if err != nil {
		return &TransportError{err}
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != 200 {
		span.SetStatus(codes.Error, "unexpected response code")
		return &BadResponseCode{
			URL:  url,
			Code: resp.StatusCode,
		}
	}

	return json.NewDecoder(resp.Body).Decode(responseEntity)
}

// TODO: decode -> to resulting entity
func (c *HTTPTransportLayer) get(parent context.Context, opName, resource string, decode func(d *json.Decoder) error) error {
	ctx, span := tracer.Start(parent, "pg-cqrs.v1:"+opName)
	defer span.End()

	url := c.BaseURL + resource
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}

	// todo: review in closer detail for SSRF
	// nolint
	resp, err := c.wire.Do(req)
	if err != nil {
		return &TransportError{err}
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != 200 {
		span.SetStatus(codes.Error, "unexpected response code")
		return &BadResponseCode{
			URL:  url,
			Code: resp.StatusCode,
		}
	}

	d := json.NewDecoder(resp.Body)
	return decode(d)
}

// AllEnvelopes returns all event envelopes for the given app and stream.
func (c *HTTPTransportLayer) AllEnvelopes(parent context.Context, app, stream string) ([]Envelope, error) {
	var reply AllEnvelopes
	err := c.get(parent, "AllEnvelopes", "/v1/app/"+app+"/"+stream+"/all", func(d *json.Decoder) error {
		return d.Decode(&reply)
	})
	return reply.Envelopes, err
}

// SubmitReply represents the response after submitting an event.
type SubmitReply struct {
	ID int64 `json:"id"`
}

// Submit sends an event to the remote service.
func (c *HTTPTransportLayer) Submit(parent context.Context, app, stream, kind string, event interface{}) (*Submitted, error) {
	ctx, span := tracer.Start(parent, "pg-cqrs.v1:submit")
	defer span.End()

	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + "/v1/app/" + app + "/" + stream + "/submit/" + kind
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	// todo: review in more detail
	// nolint
	resp, err := c.wire.Do(req)
	if err != nil {
		return nil, &TransportError{err}
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode == 404 {
		span.SetStatus(codes.Error, "unexpected 404")
		return nil, errors.Join(&BadResponseCode{URL: url, Code: resp.StatusCode}, err)
	}

	out := &SubmitReply{}
	d := json.NewDecoder(resp.Body)
	if err := d.Decode(out); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "decoding error")
		return nil, err
	}
	return &Submitted{ID: out.ID}, err
}

// EnsureStream ensures the given stream exists on the remote service.
func (c *HTTPTransportLayer) EnsureStream(parent context.Context, app, stream string) error {
	ctx, span := tracer.Start(parent, "pg-cqrs.v1:ensure-stream")
	defer span.End()

	url := c.BaseURL + "/v1/app/" + app + "/" + stream
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, http.NoBody)
	if err != nil {
		return err
	}

	// todo: investigate SSRF
	// nolint
	resp, err := c.wire.Do(req)
	if err != nil {
		return &TransportError{err}
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != 200 {
		span.SetStatus(codes.Error, "unexpected response code")
		return &BadResponseCode{
			URL:  url,
			Code: resp.StatusCode,
		}
	}
	return nil
}

// GetEvent retrieves a specific event from the remote service.
func (c *HTTPTransportLayer) GetEvent(parent context.Context, app, stream string, id int64, payload interface{}) error {
	url := "/v1/app/" + app + "/" + stream + "/payload/" + strconv.FormatInt(id, 10)
	return c.get(parent, "get-payload", url, func(d *json.Decoder) error {
		return d.Decode(payload)
	})
}

// Query performs a query against the remote service.
func (c *HTTPTransportLayer) Query(parent context.Context, domain, stream string, query WireQuery, out *WireQueryResult) error {
	url := "/v1/app/" + domain + "/" + stream + "/query"
	return c.post(parent, "query", url, query, out)
}

// QueryBatch performs a batch query against the remote service.
func (c *HTTPTransportLayer) QueryBatch(parent context.Context, domain, stream string, query WireQuery, out *WireBatchResults) error {
	url := "/v1/app/" + domain + "/" + stream + "/query-batch"
	return c.post(parent, "query-batch", url, query, out)
}

// QueryBatchR2 performs an R2 batch query against the remote service.
func (c *HTTPTransportLayer) QueryBatchR2(parent context.Context, domain, stream string, query *WireBatchR2Request, out *WireBatchR2Result) error {
	url := "/v1/app/" + domain + "/" + stream + "/query-batch-r2"
	return c.post(parent, "query-batch-r2", url, query, out)
}

// SetPosition sets the consumer's position in a stream.
func (c *HTTPTransportLayer) SetPosition(parent context.Context, domain, stream, consumer string, eventID int64) (*SetPositionResult, error) {
	url := fmt.Sprintf("/v1/domains/%s/streams/%s/positions/%s", domain, stream, consumer)
	type setPositionRequest struct {
		EventID int64 `json:"eventID"`
	}
	type setPositionResponse struct {
		CurrentEventID  int64  `json:"currentEventID"`
		PreviousEventID *int64 `json:"previousEventID,omitempty"`
	}
	var resp setPositionResponse
	err := c.post(parent, "setPosition", url, setPositionRequest{EventID: eventID}, &resp)
	if err != nil {
		return nil, err
	}
	return &SetPositionResult{
		PreviousEventID: resp.PreviousEventID,
		CurrentEventID:  resp.CurrentEventID,
	}, nil
}

// GetPosition gets the consumer's position in a stream.
func (c *HTTPTransportLayer) GetPosition(parent context.Context, domain, stream, consumer string) (eventID int64, found bool, err error) {
	url := fmt.Sprintf("/v1/domains/%s/streams/%s/positions/%s", domain, stream, consumer)
	type getPositionResponse struct {
		EventID int64 `json:"eventID"`
		Found   bool  `json:"found"`
	}
	var resp getPositionResponse
	err = c.get(parent, "getPosition", url, func(d *json.Decoder) error {
		return d.Decode(&resp)
	})
	if err != nil {
		return 0, false, err
	}
	return resp.EventID, resp.Found, nil
}

// ListConsumers lists all consumers for a stream.
func (c *HTTPTransportLayer) ListConsumers(parent context.Context, domain, stream string) ([]string, error) {
	url := fmt.Sprintf("/v1/domains/%s/streams/%s/positions", domain, stream)
	type listConsumersResponse struct {
		Consumers []string `json:"consumers"`
	}
	var resp listConsumersResponse
	err := c.get(parent, "listConsumers", url, func(d *json.Decoder) error {
		return d.Decode(&resp)
	})
	if err != nil {
		return nil, err
	}
	return resp.Consumers, nil
}

// DeletePosition deletes the consumer's position in a stream.
func (c *HTTPTransportLayer) DeletePosition(parent context.Context, domain, stream, consumer string) error {
	ctx, span := tracer.Start(parent, "pg-cqrs.v1:deletePosition")
	defer span.End()

	url := c.BaseURL + fmt.Sprintf("/v1/domains/%s/streams/%s/positions/%s", domain, stream, consumer)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, http.NoBody)
	if err != nil {
		return err
	}

	resp, err := c.wire.Do(req)
	if err != nil {
		return &TransportError{err}
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != 200 {
		return &BadResponseCode{URL: url, Code: resp.StatusCode}
	}
	return nil
}

// Watch sets up a watch on the remote service.
func (c *HTTPTransportLayer) Watch(_ context.Context, _ *ipc.QueryIn) (WatchInternal, error) {
	return nil, errors.New("not implemented")
}

// Meta retrieves metadata from the remote service.
func (c *HTTPTransportLayer) Meta(parent context.Context) (WireMetaV1, error) {
	var meta WireMetaV1
	url := "/v1/app"
	err := c.get(parent, "meta", url, func(d *json.Decoder) error {
		return d.Decode(&meta)
	})
	return meta, err
}

// NewHTTPTransport creates a new HTTPTransportLayer.
func NewHTTPTransport(url string) *HTTPTransportLayer {
	return &HTTPTransportLayer{
		BaseURL: url,
		wire: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// BadResponseCode represents an error for an unexpected HTTP response code.
type BadResponseCode struct {
	URL  string
	Code int
}

func (b *BadResponseCode) Error() string {
	return fmt.Sprintf("bad response code %d for %s", b.Code, b.URL)
}
