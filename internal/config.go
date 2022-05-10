package internal

type PGStorage struct {
	DatabaseURL string `json:"url"`
}

type Storage struct {
	Primary PGStorage `json:"primary"`
}
