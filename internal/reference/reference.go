package reference

import "context"

type Item struct {
	ID      int    `json:"id"`
	Sorting int    `json:"sorting"`
	Name    string `json:"name"`
}

type Provider interface {
	Categories(context.Context) ([]Item, error)
	Municipalities(context.Context) ([]Item, error)
}

type listResponse struct {
	Items []Item `json:"items"`
}
