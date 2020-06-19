package http

import (
	"encoding/json"
	"net/http"
)

type router struct {
	routes []*route
}

type route struct {
	method  string
	path    string
	handler appHandler
}

type appHandler func(http.ResponseWriter, *http.Request) *httpError

func (fn appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e := fn(w, r); e != nil { // e is *httpError, not os.Error.
		if e.Message == "" {
			e.Message = e.err.Error()
		}
		w.WriteHeader(e.Status)
		if w.Header().Get("Content-Type") == jsonMediaType {
			out, err := json.Marshal(e)
			if err != nil {
				panic(err)
			}
			w.Write(out)
		} else {
			w.Write([]byte(e.Message))
		}
	}
}

func notFoundHandler(w http.ResponseWriter, r *http.Request) *httpError {
	writeJSONHeader(w)
	return &httpError{
		Status:  http.StatusNotFound,
		Message: "Resource not found",
	}
}

func (r *router) route(method, path string, handler appHandler) *route {
	route := route{
		method:  method,
		path:    path,
		handler: handler,
	}
	r.routes = append(r.routes, &route)
	return &route
}

func (r *router) handler() http.Handler {
	return appHandler(func(w http.ResponseWriter, req *http.Request) *httpError {
		for _, route := range r.routes {
			if route.match(req) {
				return route.handler(w, req)
			}
		}
		return notFoundHandler(w, req)
	})
}

func (r *route) match(req *http.Request) bool {
	if req.Method != r.method {
		return false
	}
	if r.path != req.URL.Path {
		return false
	}
	return true
}
