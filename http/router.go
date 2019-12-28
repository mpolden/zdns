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

type appHandler func(http.ResponseWriter, *http.Request) (interface{}, *httpError)

func (fn appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, e := fn(w, r)
	w.Header().Set("Content-Type", "application/json")
	if e != nil { // e is *Error, not os.Error.
		if e.Message == "" {
			e.Message = e.err.Error()
		}
		out, err := json.Marshal(e)
		if err != nil {
			panic(err)
		}
		w.WriteHeader(e.Status)
		w.Write(out)
	} else if data != nil {
		out, err := json.Marshal(data)
		if err != nil {
			panic(err)
		}
		w.Write(out)
	}
}

func newRouter() *router { return &router{} }

func notFoundHandler(w http.ResponseWriter, r *http.Request) (interface{}, *httpError) {
	return nil, &httpError{
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
	return appHandler(func(w http.ResponseWriter, req *http.Request) (interface{}, *httpError) {
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
