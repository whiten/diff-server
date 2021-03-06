package loghttp

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"

	lh "github.com/motemen/go-loghttp"
	zl "github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"

	// Import go-loghttp/global to override default http transport.
	_ "github.com/motemen/go-loghttp/global"
)

func init() {
	lh.DefaultLogRequest = func(req *http.Request) {
		dumped, err := httputil.DumpRequest(req, true)
		if err != nil {
			zlog.Err(err).Stack().Msg("Could not dump request")
			return
		}
		// TODO: Properly contextualize these logs.
		zlog.Debug().
			Timestamp().
			Str("method", req.Method).
			Str("url", req.URL.String()).
			Bytes("dump", dumped).
			Msg("Outgoing request -->")
	}

	lh.DefaultLogResponse = func(resp *http.Response) {
		dumped, err := httputil.DumpResponse(resp, true)
		if err != nil {
			zlog.Err(err).Stack().Msg("Could not dump response")
			return
		}
		zlog.Debug().
			Timestamp().
			Str("method", resp.Request.Method).
			Str("url", resp.Request.URL.String()).
			Int("status", resp.StatusCode).
			Bytes("dump", dumped).
			Msg("Outgoing request <--")
	}
}

// Wrap wraps the given handler with a Handler that logs HTTP requests
// and responses.
func Wrap(handler http.Handler, l zl.Logger) Handler {
	return Handler{wrapped: handler, l: l}
}

// Handler is a wrapper for http.Handlers that logs the HTTP request and
// response. It logs full request headers but logging full response headers
// seems like more work (eg
// https://stackoverflow.com/questions/29319783/logging-responses-to-incoming-http-requests-inside-http-handlefunc)
// so we settle for logging the response status code and response body for now.
type Handler struct {
	wrapped http.Handler
	l       zl.Logger
}

// ServeHTTP logs the request, calls the underlying handler, and logs the response.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dumped, err := httputil.DumpRequest(r, true)
	if err != nil {
		h.l.Err(err).Stack().Msg("Could not dump request")
		http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
		return
	}

	ll := h.l.With().
		Str("method", r.Method).
		Str("req", r.URL.String()).
		Logger()

	ll.Debug().
		Bytes("dump", dumped).
		Msg("Incoming request -->")

	rl := &responseLogger{ResponseWriter: w, status: 200}
	h.wrapped.ServeHTTP(rl, r)
	body, err := maybeUnzip(rl.responseBody.Bytes())
	if err != nil {
		ll.Err(err).Stack().Msg("Could not unzip")
		return
	}
	ll.Debug().
		Int("status", rl.status).
		Bytes("body", body).
		Msg("Incoming request <--")
}

func maybeUnzip(b []byte) ([]byte, error) {
	if http.DetectContentType(b) != "application/x-gzip" {
		return b, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return ioutil.ReadAll(zr)
}

type responseLogger struct {
	http.ResponseWriter
	responseBody bytes.Buffer
	status       int
}

func (r *responseLogger) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseLogger) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	if err != nil {
		return n, err
	}
	return r.responseBody.Write(b)
}
