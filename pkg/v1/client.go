package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
	"net/http"
	"strconv"
)

type Client struct {
	BaseURL string
	wire    *http.Client
}

type Envelope struct {
	ID   int64  `json:"id"`
	When string `json:"when"`
	Kind string `json:"kind"`
}

type AllEnvelopes struct {
	Envelopes []Envelope `json:"envelopes"`
}

//TODO: decode -> to resulting entity
func (c *Client) get(parent context.Context, opName, resource string, decode func(d *json.Decoder) error) error {
	ctx, span := tracer.Start(parent, "pg-cqrs.v1:"+opName)
	defer span.End()

	url := c.BaseURL + resource
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.wire.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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

func (c *Client) AllEnvelopes(parent context.Context, app, stream string) (AllEnvelopes, error) {
	var reply AllEnvelopes
	err := c.get(parent, "AllEnvelopes", "/v1/app/"+app+"/"+stream+"/all", func(d *json.Decoder) error {
		return d.Decode(&reply)
	})
	return reply, err
}

type SubmitReply struct {
	Id int64 `json:"id"`
}

func (c *Client) Submit(parent context.Context, app, stream, kind string, event interface{}) (*SubmitReply, error) {
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

	resp, err := c.wire.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		span.SetStatus(codes.Error, "unexpected 404")
		return nil, &BadResponseCode{
			URL:  url,
			Code: resp.StatusCode,
		}
	}

	out := &SubmitReply{}
	d := json.NewDecoder(resp.Body)
	if err := d.Decode(out); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "decoding error")
		return out, err
	}
	return out, nil
}

//TODO: retitle -- only ensures said stream exists
func (c *Client) NewStream(parent context.Context, app string, stream string) error {
	ctx, span := tracer.Start(parent, "pg-cqrs.v1:new-stream")
	defer span.End()

	url := c.BaseURL + "/v1/app/" + app + "/" + stream
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.wire.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		span.SetStatus(codes.Error, "unexpected response code")
		return &BadResponseCode{
			URL:  url,
			Code: resp.StatusCode,
		}
	}
	return nil
}

func (c *Client) GetEvent(parent context.Context, app string, stream string, id int64, payload interface{}) error {
	url := "/v1/app/" + app + "/" + stream + "/payload/" + strconv.FormatInt(id, 10)
	return c.get(parent, "get-payload", url, func(d *json.Decoder) error {
		return d.Decode(payload)
	})
}

func NewClient(url string) *Client {
	return &Client{
		BaseURL: url,
		wire: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

type BadResponseCode struct {
	URL  string
	Code int
}

func (b *BadResponseCode) Error() string {
	return fmt.Sprintf("bad response code %d for %s", b.Code, b.URL)
}
