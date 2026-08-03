package prismhttp

import (
	"net/http"

	"github.com/ChiaYuChang/prism/internal/http/middleware"
)

// Router is a small ServeMux wrapper that supports grouped subroutes with
// inherited middleware, without introducing a router framework dependency.
type Router struct {
	mux *http.ServeMux
	mws []middleware.Middleware
}

// NewRouter returns an empty router with the supplied middleware chain.
func NewRouter(mws ...middleware.Middleware) *Router {
	return &Router{
		mux: http.NewServeMux(),
		mws: append([]middleware.Middleware(nil), mws...),
	}
}

// Use appends middleware to routes registered after the call.
func (r *Router) Use(mws ...middleware.Middleware) {
	r.mws = append(r.mws, mws...)
}

// Handle registers a handler on the underlying mux.
func (r *Router) Handle(pattern string, handler http.Handler) {
	r.mux.Handle(pattern, handler)
}

// HandleFunc registers a handler function on the underlying mux.
func (r *Router) HandleFunc(pattern string, handler http.HandlerFunc) {
	r.Handle(pattern, handler)
}

// Route mounts a child router at prefix. Middleware passed to Route applies to
// the child subtree; parent middleware still wraps the mounted child handler.
func (r *Router) Route(prefix string, fn func(*Router), mws ...middleware.Middleware) {
	child := NewRouter(mws...)
	fn(child)
	r.mux.Handle(prefix+"/", http.StripPrefix(prefix, child.Handler()))
}

// Handler returns the router's HTTP handler with its middleware chain applied.
func (r *Router) Handler() http.Handler {
	return middleware.Chain(r.mws...)(r.mux)
}
