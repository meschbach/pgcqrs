package v1

type Submitted struct {
	//ID is the Envelope ID of the event.
	ID int64
}

type Envelope struct {
	ID   int64  `json:"id"`
	When string `json:"when"`
	Kind string `json:"kind"`
}
