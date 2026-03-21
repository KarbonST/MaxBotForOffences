package reference

import (
	"context"
	"sync"
	"time"
)

type CachedProvider struct {
	next Provider
	ttl  time.Duration

	mu             sync.Mutex
	categories     cachedList
	municipalities cachedList
}

type cachedList struct {
	items       []Item
	expiresAt   time.Time
	initialized bool
}

func NewCachedProvider(next Provider, ttl time.Duration) *CachedProvider {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	return &CachedProvider{
		next: next,
		ttl:  ttl,
	}
}

func (p *CachedProvider) Categories(ctx context.Context) ([]Item, error) {
	return p.load(ctx, &p.categories, p.next.Categories)
}

func (p *CachedProvider) Municipalities(ctx context.Context) ([]Item, error) {
	return p.load(ctx, &p.municipalities, p.next.Municipalities)
}

func (p *CachedProvider) load(ctx context.Context, state *cachedList, loader func(context.Context) ([]Item, error)) ([]Item, error) {
	now := time.Now()

	p.mu.Lock()
	if state.initialized && now.Before(state.expiresAt) {
		items := cloneItems(state.items)
		p.mu.Unlock()
		return items, nil
	}
	p.mu.Unlock()

	items, err := loader(ctx)
	if err != nil {
		return nil, err
	}

	cloned := cloneItems(items)

	p.mu.Lock()
	state.items = cloned
	state.expiresAt = now.Add(p.ttl)
	state.initialized = true
	p.mu.Unlock()

	return cloneItems(cloned), nil
}

func cloneItems(items []Item) []Item {
	if len(items) == 0 {
		return nil
	}

	cloned := make([]Item, len(items))
	copy(cloned, items)
	return cloned
}
